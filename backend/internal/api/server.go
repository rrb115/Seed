package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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

type metrics struct {
	opsReceivedTotal   atomic.Uint64
	opsAppliedTotal    atomic.Uint64
	syncConflictsTotal atomic.Uint64
	syncErrorsTotal    atomic.Uint64
}

type preparedRecord struct {
	WorkflowID string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	Claims     core.PrepareTokenClaims
}

// Server wires API handlers.
type Server struct {
	signer    *security.Signer
	store     store.Store
	engine    *syncer.Engine
	staticDir string
	apiToken  string

	metrics metrics

	prepareMu sync.RWMutex
	prepared  map[string]preparedRecord
}

func NewServer(signer *security.Signer, st store.Store, engine *syncer.Engine, staticDir string, apiToken string) *Server {
	s := &Server{
		signer:    signer,
		store:     st,
		engine:    engine,
		staticDir: staticDir,
		apiToken:  apiToken,
		prepared:  map[string]preparedRecord{},
	}

	prepareReq := map[string]bool{}
	for workflowID, tpl := range manifestTemplates() {
		prepareReq[workflowID] = tpl.RequiresGlobalState || tpl.RequiresValidation
	}
	s.engine.SetPrepareValidator(s)
	s.engine.SetPrepareRequirements(prepareReq)
	return s
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/metrics", s.withCORS(s.handleMetrics))
	mux.HandleFunc("/.well-known/dwce-keys", s.withCORS(s.handleDWCEKeys))
	mux.HandleFunc("/v1/manifest", s.withCORS(s.withAuth(s.handleManifest)))
	mux.HandleFunc("/v1/prepare", s.withCORS(s.withAuth(s.handlePrepare)))
	mux.HandleFunc("/v1/sync", s.withCORS(s.withAuth(s.handleSync)))
	mux.HandleFunc("/v1/sync/status", s.withCORS(s.withAuth(s.handleSyncStatus)))
	mux.HandleFunc("/v1/verify-resource", s.withCORS(s.withAuth(s.handleVerifyResource)))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "ops_received_total %d\n", s.metrics.opsReceivedTotal.Load())
	_, _ = fmt.Fprintf(w, "ops_applied_total %d\n", s.metrics.opsAppliedTotal.Load())
	_, _ = fmt.Fprintf(w, "sync_conflicts_total %d\n", s.metrics.syncConflictsTotal.Load())
	_, _ = fmt.Fprintf(w, "sync_errors_total %d\n", s.metrics.syncErrorsTotal.Load())
}

func (s *Server) handleDWCEKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"keys": []security.PublicJWK{s.signer.PublicJWK()},
	})
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
		PrepareRequired:    tpl.RequiresGlobalState || tpl.RequiresValidation,
		Version:            1,
		KeyID:              s.signer.KeyID(),
		Audience:           "seed-pwa",
		CreatedAt:          now,
		ExpiresAt:          now.Add(15 * time.Minute),
	}

	if r.URL.Query().Get("include_prepare") == "1" && payload.PrepareRequired {
		token, claims, err := s.issuePrepareToken(goal, nil)
		if err != nil {
			http.Error(w, "failed to issue prepare token", http.StatusInternalServerError)
			return
		}
		payload.PrepareToken = token
		payload.ExpiresAt = claims.ExpiresAt
	}

	canonical, err := core.CanonicalManifestBytes(payload)
	if err != nil {
		http.Error(w, "manifest canonicalization failed", http.StatusInternalServerError)
		return
	}
	jws, err := s.signer.SignCompactJWS(canonical)
	if err != nil {
		http.Error(w, "manifest signing failed", http.StatusInternalServerError)
		return
	}

	manifest := core.GoalManifest{
		ManifestPayload: payload,
		ManifestJWS:     jws,
		Signature:       s.signer.Sign(canonical),
	}
	writeJSON(w, http.StatusOK, manifest)
}

type prepareReq struct {
	Preconditions json.RawMessage `json:"preconditions"`
}

func (s *Server) handlePrepare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
	if workflowID == "" {
		http.Error(w, "workflow_id is required", http.StatusBadRequest)
		return
	}
	if _, ok := manifestTemplates()[workflowID]; !ok {
		http.Error(w, "unknown workflow_id", http.StatusNotFound)
		return
	}

	var req prepareReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)

	token, claims, err := s.issuePrepareToken(workflowID, req.Preconditions)
	if err != nil {
		http.Error(w, "failed to sign prepare token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"prepare_token": token,
		"valid_from":    claims.ValidFrom,
		"expires_at":    claims.ExpiresAt,
		"nonce":         claims.Nonce,
	})
}

func (s *Server) issuePrepareToken(workflowID string, preconditions json.RawMessage) (string, core.PrepareTokenClaims, error) {
	now := time.Now().UTC()
	claims := core.PrepareTokenClaims{
		WorkflowID:   workflowID,
		IssuedAt:     now,
		ValidFrom:    now,
		ExpiresAt:    now.Add(15 * time.Minute),
		Nonce:        randomID(),
		Precondition: preconditions,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", core.PrepareTokenClaims{}, err
	}
	token, err := s.signer.SignCompactJWS(payload)
	if err != nil {
		return "", core.PrepareTokenClaims{}, err
	}

	s.prepareMu.Lock()
	s.prepared[claims.Nonce] = preparedRecord{WorkflowID: workflowID, IssuedAt: now, ExpiresAt: claims.ExpiresAt, Claims: claims}
	s.prepareMu.Unlock()

	return token, claims, nil
}

func (s *Server) ValidatePrepareToken(_ context.Context, workflowID string, token string) error {
	payload, _, err := security.VerifyCompactJWS(token, []security.PublicJWK{s.signer.PublicJWK()})
	if err != nil {
		return err
	}
	var claims core.PrepareTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return err
	}
	if claims.WorkflowID != workflowID {
		return errors.New("workflow mismatch")
	}
	now := time.Now().UTC()
	if now.Before(claims.ValidFrom) || now.After(claims.ExpiresAt) {
		return errors.New("token expired")
	}

	s.prepareMu.RLock()
	rec, ok := s.prepared[claims.Nonce]
	s.prepareMu.RUnlock()
	if !ok {
		return errors.New("prepare nonce not found")
	}
	if rec.WorkflowID != workflowID {
		return errors.New("prepare workflow mismatch")
	}
	if now.After(rec.ExpiresAt) {
		return errors.New("prepare record expired")
	}
	return nil
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	traceID := traceIDFromRequest(r)

	var req core.SyncRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil {
		s.metrics.syncErrorsTotal.Add(1)
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}
	if len(req.Ops) == 0 {
		http.Error(w, "ops cannot be empty", http.StatusBadRequest)
		return
	}
	s.metrics.opsReceivedTotal.Add(uint64(len(req.Ops)))

	txID := uuid.New()
	if strings.TrimSpace(req.ClientTxID) != "" {
		if parsed, err := uuid.Parse(req.ClientTxID); err == nil {
			txID = parsed
		}
	}

	if wantsAsync(r) {
		jobID, err := s.store.EnqueueJob(r.Context(), core.Job{Type: "sync", ClientID: req.ClientID, CreatedAt: time.Now().UTC(), TraceID: traceID})
		if err != nil {
			s.metrics.syncErrorsTotal.Add(1)
			http.Error(w, "failed to enqueue job", http.StatusInternalServerError)
			return
		}
		go s.runAsyncSync(jobID, txID, req, traceID)
		writeJSON(w, http.StatusAccepted, core.SyncResponse{ServerTime: time.Now().UTC(), QueueID: jobID.String(), TxID: txID.String(), Status: "queued"})
		return
	}

	started := time.Now()
	resp := s.engine.Apply(r.Context(), req, txID)
	s.metrics.opsAppliedTotal.Add(uint64(len(resp.AckedOpIDs)))
	s.metrics.syncConflictsTotal.Add(uint64(len(resp.Conflicts)))

	logSync(traceID, resp, time.Since(started))
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) runAsyncSync(jobID uuid.UUID, txID uuid.UUID, req core.SyncRequest, traceID string) {
	ctx := context.Background()
	_ = s.store.SetJobStatus(ctx, jobID, core.JobStatus{Status: "processing"})
	started := time.Now()
	resp := s.engine.Apply(ctx, req, txID)
	_ = s.store.SetJobStatus(ctx, jobID, core.JobStatus{Status: "completed", Response: &resp})

	s.metrics.opsAppliedTotal.Add(uint64(len(resp.AckedOpIDs)))
	s.metrics.syncConflictsTotal.Add(uint64(len(resp.Conflicts)))
	logSync(traceID, resp, time.Since(started))
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	queueID := strings.TrimSpace(r.URL.Query().Get("queue_id"))
	if queueID == "" {
		http.Error(w, "queue_id is required", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(queueID)
	if err != nil {
		http.Error(w, "invalid queue_id", http.StatusBadRequest)
		return
	}
	status, err := s.store.GetJobStatus(r.Context(), id)
	if err != nil {
		http.Error(w, "queue id not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"queue_id":   queueID,
		"status":     status.Status,
		"updated_at": status.UpdatedAt,
		"response":   status.Response,
		"error":      status.Error,
	})
}

type verifyReq struct {
	URL         string `json:"url"`
	ExpectedCID string `json:"expected_cid"`
}

func (s *Server) handleVerifyResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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

func traceIDFromRequest(r *http.Request) string {
	traceID := strings.TrimSpace(r.Header.Get("X-Trace-Id"))
	if traceID != "" {
		return traceID
	}
	return randomID()
}

func logSync(traceID string, resp core.SyncResponse, dur time.Duration) {
	logJSON(map[string]any{
		"trace_id":    traceID,
		"tx_id":       resp.TxID,
		"duration_ms": dur.Milliseconds(),
		"level":       "info",
		"applied":     len(resp.AckedOpIDs),
		"conflicts":   len(resp.Conflicts),
	})
	for _, c := range resp.Conflicts {
		logJSON(map[string]any{
			"trace_id":    traceID,
			"op_id":       c.OpID,
			"tx_id":       resp.TxID,
			"handler":     c.Handler,
			"duration_ms": dur.Milliseconds(),
			"level":       "warn",
			"reason":      c.Reason,
		})
	}
}

func logJSON(fields map[string]any) {
	b, err := json.Marshal(fields)
	if err != nil {
		log.Printf("{\"level\":\"error\",\"msg\":\"log_marshal_failed\",\"error\":%q}", err.Error())
		return
	}
	log.Print(string(b))
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
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Prefer, X-Trace-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}
