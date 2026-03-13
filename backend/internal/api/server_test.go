package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"seed/backend/internal/core"
	"seed/backend/internal/security"
	"seed/backend/internal/store"
	"seed/backend/internal/syncer"
)

func TestManifestIsSigned(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)

	mux := http.NewServeMux()
	s.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/manifest?goal=note_draft", nil)
	req.Header.Set("Authorization", "Bearer dev-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var manifest core.GoalManifest
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	payloadBytes, err := core.CanonicalManifestBytes(manifest.ManifestPayload)
	if err != nil {
		t.Fatalf("canonical bytes: %v", err)
	}
	pub := w.Header().Get("X-Manifest-Public-Key")
	if !security.Verify(pub, payloadBytes, manifest.Signature) {
		t.Fatal("manifest signature verification failed")
	}
	if manifest.SafetyClass != SafetySafe {
		t.Fatalf("expected SAFE manifest, got %s", manifest.SafetyClass)
	}
	if !manifest.OfflineEligible {
		t.Fatal("expected note_draft to be offline eligible")
	}
}

func TestUnsafeManifestRejected(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)

	mux := http.NewServeMux()
	s.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/manifest?goal=checkout", nil)
	req.Header.Set("Authorization", "Bearer dev-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body["error"] != "workflow_offline_unsafe" {
		t.Fatalf("unexpected error payload: %#v", body)
	}
}

func TestAsyncSyncFlow(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)
	mux := http.NewServeMux()
	s.Register(mux)

	payload := core.SyncRequest{
		ClientID: "device-1",
		Ops: []core.Op{
			{
				OpID:     "device-1:1",
				ObjectID: "cart:1",
				ClientID: "device-1",
				Clock:    1,
				Type:     "set_field",
				Path:     []string{"shipping", "city"},
				Value:    "Goa",
			},
		},
	}
	b, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/sync?async=1", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer dev-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}

	var accepted core.SyncResponse
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("unmarshal accepted payload: %v", err)
	}
	if accepted.QueueID == "" {
		t.Fatal("queue id is empty")
	}

	var statusResp *httptest.ResponseRecorder
	for i := 0; i < 20; i++ {
		reqStatus := httptest.NewRequest(http.MethodGet, "/v1/sync/status?queue_id="+accepted.QueueID, nil)
		reqStatus.Header.Set("Authorization", "Bearer dev-token")
		statusResp = httptest.NewRecorder()
		mux.ServeHTTP(statusResp, reqStatus)
		if statusResp.Code == http.StatusOK {
			var status core.SyncStatus
			_ = json.Unmarshal(statusResp.Body.Bytes(), &status)
			if status.Status == "completed" && status.Response != nil {
				if len(status.Response.AckedOpIDs) != 1 {
					t.Fatalf("expected 1 acked op, got %d", len(status.Response.AckedOpIDs))
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("sync status did not complete: code=%d body=%s", statusResp.Code, statusResp.Body.String())
}

func buildServer(t *testing.T, staticDir string) *Server {
	t.Helper()
	signer, err := security.NewSigner("test-key", "")
	if err != nil {
		t.Fatalf("NewSigner error: %v", err)
	}
	st := store.NewMemoryStore()
	engine := syncer.NewEngine(st)
	return NewServer(signer, st, engine, staticDir, "dev-token")
}

func makeStaticDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	files := map[string]string{
		"index.html":                    "<html></html>",
		"styles.css":                    "body{}",
		"app.js":                        "console.log('x')",
		"sw.js":                         "self.addEventListener('install',()=>{})",
		"offline.html":                  "offline",
		"dwce/index.js":                 "export const x = 1;",
		"dwce/workflow-engine.js":       "export class WorkflowEngine {}",
		"dwce/dependency-graph.js":      "export class DependencyGraphEngine {}",
		"dwce/manifest-manager.js":      "export class ManifestManager {}",
		"dwce/offline-state.js":         "export class OfflineStateEngine {}",
		"dwce/op-queue.js":              "export class OperationQueue {}",
		"dwce/sync-agent.js":            "export class SyncAgent {}",
		"dwce/service-worker-bridge.js": "export class ServiceWorkerBridge {}",
		"dwce/storage.js":               "export const storage = {};",
	}
	for n, content := range files {
		path := filepath.Join(d, n)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	return d
}
