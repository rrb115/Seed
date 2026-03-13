package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"seed/backend/internal/core"
	"seed/backend/internal/security"
	"seed/backend/internal/store"
	"seed/backend/internal/syncer"
)

const (
	SafetySafe     = "SAFE"
	SafetyEventual = "EVENTUAL"
	SafetyUnsafe   = "UNSAFE"
)

type goalTemplate struct {
	Objectives          []string
	CoreResources       []string
	RequiresGlobalState bool
	RequiresValidation  bool
}

type workflowGraph struct {
	StepResources map[string][]string
	ResourceDeps  map[string][]string
}

// Server wires API handlers for manifests and sync.
type Server struct {
	signer    *security.Signer
	store     *store.MemoryStore
	engine    *syncer.Engine
	staticDir string
	apiToken  string
}

func NewServer(signer *security.Signer, st *store.MemoryStore, engine *syncer.Engine, staticDir string, apiToken string) *Server {
	return &Server{
		signer:    signer,
		store:     st,
		engine:    engine,
		staticDir: staticDir,
		apiToken:  apiToken,
	}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/manifest", s.withCORS(s.withAuth(s.handleManifest)))
	mux.HandleFunc("/v1/sync", s.withCORS(s.withAuth(s.handleSync)))
	mux.HandleFunc("/v1/sync/status", s.withCORS(s.withAuth(s.handleSyncStatus)))
	mux.HandleFunc("/v1/verify-resource", s.withCORS(s.withAuth(s.handleVerifyResource)))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	goal := strings.TrimSpace(r.URL.Query().Get("goal"))
	if goal == "" {
		http.Error(w, "goal is required", http.StatusBadRequest)
		return
	}

	templates := manifestTemplates()
	tpl, ok := templates[goal]
	if !ok {
		http.Error(w, "unknown goal", http.StatusNotFound)
		return
	}

	safetyClass, offlineEligible, validationRequired := evaluateWorkflowSafety(tpl)
	if !offlineEligible {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":               "workflow_offline_unsafe",
			"goal":                goal,
			"safety_class":        safetyClass,
			"offline_eligible":    false,
			"validation_required": validationRequired,
		})
		return
	}

	steps := parseCSV(r.URL.Query().Get("steps"))
	if len(steps) == 0 {
		steps = tpl.Objectives
	}

	graphs := workflowGraphs()
	graph, ok := graphs[goal]
	if !ok {
		http.Error(w, "missing workflow graph", http.StatusInternalServerError)
		return
	}
	for _, step := range steps {
		if _, exists := graph.StepResources[step]; !exists {
			http.Error(w, "invalid step for goal", http.StatusBadRequest)
			return
		}
	}

	closure := computeResourceClosure(steps, graph.StepResources, graph.ResourceDeps)
	clientHints := parseCSV(r.URL.Query().Get("resources"))
	resolvedURLs := orderedUnique(append(append(tpl.CoreResources, closure...), clientHints...))

	resources := make([]core.ManifestResource, 0, len(resolvedURLs))
	for _, url := range resolvedURLs {
		meta, err := s.resourceMetadata(url)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resources = append(resources, meta)
	}

	now := time.Now().UTC()
	payload := core.ManifestPayload{
		ManifestID:         randomID(),
		Goal:               goal,
		Objectives:         steps,
		Resources:          resources,
		SafetyClass:        safetyClass,
		OfflineEligible:    offlineEligible,
		ValidationRequired: validationRequired,
		Version:            1,
		KeyID:              s.signer.KeyID(),
		Audience:           "seed-pwa",
		CreatedAt:          now,
		ExpiresAt:          now.Add(15 * time.Minute),
	}

	canonical, err := core.CanonicalManifestBytes(payload)
	if err != nil {
		http.Error(w, "manifest canonicalization failed", http.StatusInternalServerError)
		return
	}

	manifest := core.GoalManifest{
		ManifestPayload: payload,
		Signature:       s.signer.Sign(canonical),
	}
	w.Header().Set("X-Manifest-Public-Key", s.signer.PublicKeyBase64())
	writeJSON(w, http.StatusOK, manifest)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req core.SyncRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}
	if len(req.Ops) == 0 {
		http.Error(w, "ops cannot be empty", http.StatusBadRequest)
		return
	}

	if wantsAsync(r) {
		queueID := randomID()
		s.store.SetSyncStatus(core.SyncStatus{QueueID: queueID, Status: "queued"})
		go func() {
			resp := s.engine.Apply(req)
			s.store.SetSyncStatus(core.SyncStatus{QueueID: queueID, Status: "completed", Response: &resp})
		}()
		writeJSON(w, http.StatusAccepted, core.SyncResponse{
			ServerTime: time.Now().UTC(),
			QueueID:    queueID,
			Status:     "queued",
		})
		return
	}

	resp := s.engine.Apply(req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	queueID := strings.TrimSpace(r.URL.Query().Get("queue_id"))
	if queueID == "" {
		http.Error(w, "queue_id is required", http.StatusBadRequest)
		return
	}
	status, ok := s.store.GetSyncStatus(queueID)
	if !ok {
		http.Error(w, "queue id not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type verifyReq struct {
	URL         string `json:"url"`
	ExpectedCID string `json:"expected_cid"`
}

func (s *Server) handleVerifyResource(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req verifyReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	meta, err := s.resourceMetadata(req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":          req.URL,
		"computed_cid": meta.CID,
		"expected_cid": req.ExpectedCID,
		"match":        req.ExpectedCID == "" || req.ExpectedCID == meta.CID,
		"size":         meta.Size,
	})
}

func (s *Server) resourceMetadata(urlPath string) (core.ManifestResource, error) {
	cleanURL := strings.TrimSpace(urlPath)
	if cleanURL == "" {
		return core.ManifestResource{}, errors.New("resource url is empty")
	}
	relPath := cleanURL
	if relPath == "/" {
		relPath = "/index.html"
	}
	relPath = strings.TrimPrefix(relPath, "/")
	absPath := filepath.Join(s.staticDir, relPath)
	if !strings.HasPrefix(filepath.Clean(absPath), filepath.Clean(s.staticDir)) {
		return core.ManifestResource{}, errors.New("invalid resource path")
	}

	b, err := os.ReadFile(absPath)
	if err != nil {
		return core.ManifestResource{}, fmt.Errorf("resource unavailable: %s", cleanURL)
	}

	sum := sha256.Sum256(b)
	cid := "sha256:" + hex.EncodeToString(sum[:])
	integrity := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
	return core.ManifestResource{
		URL:        cleanURL,
		CID:        cid,
		Size:       int64(len(b)),
		Integrity:  integrity,
		TTLSeconds: 900,
	}, nil
}

func evaluateWorkflowSafety(tpl goalTemplate) (safetyClass string, offlineEligible bool, validationRequired bool) {
	if tpl.RequiresGlobalState {
		return SafetyUnsafe, false, true
	}
	if tpl.RequiresValidation {
		return SafetyEventual, true, true
	}
	return SafetySafe, true, false
}

func manifestTemplates() map[string]goalTemplate {
	baseResources := []string{"/", "/index.html", "/styles.css", "/app.js", "/sw.js", "/offline.html"}
	return map[string]goalTemplate{
		"note_draft": {
			Objectives:    []string{"open_editor", "edit_content", "save_draft"},
			CoreResources: baseResources,
		},
		"support_ticket": {
			Objectives:         []string{"fill_form", "attach_context", "submit_ticket"},
			CoreResources:      baseResources,
			RequiresValidation: true,
		},
		"signup": {
			Objectives:         []string{"profile", "verification", "consent"},
			CoreResources:      baseResources,
			RequiresValidation: true,
		},
		"checkout": {
			Objectives:          []string{"review_cart", "enter_address", "payment", "confirmation"},
			CoreResources:       baseResources,
			RequiresGlobalState: true,
		},
		"booking": {
			Objectives:          []string{"select_slot", "details", "confirm"},
			CoreResources:       baseResources,
			RequiresGlobalState: true,
		},
	}
}

func workflowGraphs() map[string]workflowGraph {
	baseResourceDeps := map[string][]string{
		"/app.js": {"/dwce/index.js"},
		"/dwce/index.js": {
			"/dwce/workflow-engine.js",
			"/dwce/dependency-graph.js",
			"/dwce/manifest-manager.js",
			"/dwce/offline-state.js",
			"/dwce/op-queue.js",
			"/dwce/sync-agent.js",
			"/dwce/service-worker-bridge.js",
			"/dwce/storage.js",
		},
	}

	return map[string]workflowGraph{
		"note_draft": {
			StepResources: map[string][]string{
				"open_editor":  {"/index.html", "/styles.css", "/app.js", "/sw.js"},
				"edit_content": {"/index.html", "/styles.css", "/app.js"},
				"save_draft":   {"/index.html", "/styles.css", "/app.js", "/offline.html"},
			},
			ResourceDeps: baseResourceDeps,
		},
		"support_ticket": {
			StepResources: map[string][]string{
				"fill_form":      {"/index.html", "/styles.css", "/app.js", "/sw.js"},
				"attach_context": {"/index.html", "/styles.css", "/app.js"},
				"submit_ticket":  {"/index.html", "/styles.css", "/app.js", "/offline.html"},
			},
			ResourceDeps: baseResourceDeps,
		},
		"signup": {
			StepResources: map[string][]string{
				"profile":      {"/index.html", "/styles.css", "/app.js", "/sw.js"},
				"verification": {"/index.html", "/styles.css", "/app.js"},
				"consent":      {"/index.html", "/styles.css", "/app.js", "/offline.html"},
			},
			ResourceDeps: baseResourceDeps,
		},
		"checkout": {
			StepResources: map[string][]string{
				"review_cart":   {"/index.html", "/styles.css", "/app.js", "/sw.js"},
				"enter_address": {"/index.html", "/styles.css", "/app.js"},
				"payment":       {"/index.html", "/styles.css", "/app.js"},
				"confirmation":  {"/index.html", "/styles.css", "/app.js", "/offline.html"},
			},
			ResourceDeps: baseResourceDeps,
		},
		"booking": {
			StepResources: map[string][]string{
				"select_slot": {"/index.html", "/styles.css", "/app.js", "/sw.js"},
				"details":     {"/index.html", "/styles.css", "/app.js"},
				"confirm":     {"/index.html", "/styles.css", "/app.js", "/offline.html"},
			},
			ResourceDeps: baseResourceDeps,
		},
	}
}

func computeResourceClosure(steps []string, stepResources map[string][]string, resourceDeps map[string][]string) []string {
	seen := map[string]struct{}{}
	stack := make([]string, 0)

	for _, step := range steps {
		for _, resource := range stepResources[step] {
			if _, ok := seen[resource]; ok {
				continue
			}
			seen[resource] = struct{}{}
			stack = append(stack, resource)
		}
	}

	for len(stack) > 0 {
		resource := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, dep := range resourceDeps[resource] {
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			stack = append(stack, dep)
		}
	}

	result := make([]string, 0, len(seen))
	for resource := range seen {
		result = append(result, resource)
	}
	sort.Strings(result)
	return result
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return orderedUnique(out)
}

func orderedUnique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func wantsAsync(r *http.Request) bool {
	if r.URL.Query().Get("async") == "1" {
		return true
	}
	prefer := strings.ToLower(r.Header.Get("Prefer"))
	return strings.Contains(prefer, "respond-async")
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		head := strings.TrimSpace(r.Header.Get("Authorization"))
		if head == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(head, "Bearer ")
		if token != s.apiToken {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Prefer")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}
