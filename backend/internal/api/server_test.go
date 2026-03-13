package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seed/backend/internal/core"
	"seed/backend/internal/security"
	"seed/backend/internal/store"
	"seed/backend/internal/syncer"
)

func TestManifestJWKSVerification(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)

	mux := http.NewServeMux()
	s.Register(mux)

	manifestReq := httptest.NewRequest(http.MethodGet, "/v1/manifest?goal=note_draft", nil)
	manifestReq.Header.Set("Authorization", "Bearer dev-token")
	manifestResp := httptest.NewRecorder()
	mux.ServeHTTP(manifestResp, manifestReq)
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", manifestResp.Code, manifestResp.Body.String())
	}

	var manifest core.GoalManifest
	if err := json.Unmarshal(manifestResp.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if manifest.ManifestJWS == "" {
		t.Fatal("manifest_jws was empty")
	}

	keysReq := httptest.NewRequest(http.MethodGet, "/.well-known/dwce-keys", nil)
	keysResp := httptest.NewRecorder()
	mux.ServeHTTP(keysResp, keysReq)
	if keysResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", keysResp.Code)
	}
	var keyBody struct {
		Keys []security.PublicJWK `json:"keys"`
	}
	if err := json.Unmarshal(keysResp.Body.Bytes(), &keyBody); err != nil {
		t.Fatalf("keys unmarshal: %v", err)
	}

	payload, kid, err := security.VerifyCompactJWS(manifest.ManifestJWS, keyBody.Keys)
	if err != nil {
		t.Fatalf("jws verify failed: %v", err)
	}
	if kid == "" {
		t.Fatal("kid was empty")
	}
	canonical, err := core.CanonicalManifestBytes(manifest.ManifestPayload)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}
	if string(payload) != string(canonical) {
		t.Fatalf("verified payload mismatch")
	}

	if _, _, err := security.VerifyCompactJWS(manifest.ManifestJWS, []security.PublicJWK{}); err == nil {
		t.Fatal("expected unknown kid verification failure")
	}
}

func TestManifestKeyRotationSimulation(t *testing.T) {
	staticDir := makeStaticDir(t)
	s1 := buildServer(t, staticDir)
	s2 := buildServerWithKeyID(t, staticDir, "rotated-key")

	mux1 := http.NewServeMux()
	s1.Register(mux1)
	mux2 := http.NewServeMux()
	s2.Register(mux2)

	manifestReq := httptest.NewRequest(http.MethodGet, "/v1/manifest?goal=note_draft", nil)
	manifestReq.Header.Set("Authorization", "Bearer dev-token")
	manifestResp := httptest.NewRecorder()
	mux1.ServeHTTP(manifestResp, manifestReq)

	var manifest core.GoalManifest
	_ = json.Unmarshal(manifestResp.Body.Bytes(), &manifest)

	keysReq := httptest.NewRequest(http.MethodGet, "/.well-known/dwce-keys", nil)
	keysResp := httptest.NewRecorder()
	mux2.ServeHTTP(keysResp, keysReq)
	var keyBody struct {
		Keys []security.PublicJWK `json:"keys"`
	}
	_ = json.Unmarshal(keysResp.Body.Bytes(), &keyBody)

	if _, _, err := security.VerifyCompactJWS(manifest.ManifestJWS, keyBody.Keys); err == nil {
		t.Fatal("expected key rotation mismatch verification failure")
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
}

func TestPrepareThenSync(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)
	mux := http.NewServeMux()
	s.Register(mux)

	prepareReq := httptest.NewRequest(http.MethodPost, "/v1/prepare?workflow_id=support_ticket", bytes.NewReader([]byte(`{"preconditions":{"scope":"demo"}}`)))
	prepareReq.Header.Set("Authorization", "Bearer dev-token")
	prepareReq.Header.Set("Content-Type", "application/json")
	prepareResp := httptest.NewRecorder()
	mux.ServeHTTP(prepareResp, prepareReq)
	if prepareResp.Code != http.StatusOK {
		t.Fatalf("prepare failed: %d %s", prepareResp.Code, prepareResp.Body.String())
	}
	var prepareBody struct {
		PrepareToken string `json:"prepare_token"`
	}
	if err := json.Unmarshal(prepareResp.Body.Bytes(), &prepareBody); err != nil {
		t.Fatalf("prepare body: %v", err)
	}
	if prepareBody.PrepareToken == "" {
		t.Fatal("prepare token missing")
	}

	syncPayload := core.SyncRequest{ClientID: "device-1", Ops: []core.Operation{{
		OpID:         "ticket:1:1",
		ObjectID:     "ticket:1",
		ClientID:     "device-1",
		Workflow:     "support_ticket",
		Sequence:     1,
		Type:         "set_field",
		Path:         []string{"message"},
		Value:        "hello world",
		PrepareToken: prepareBody.PrepareToken,
	}}}
	b, _ := json.Marshal(syncPayload)
	syncReq := httptest.NewRequest(http.MethodPost, "/v1/sync", bytes.NewReader(b))
	syncReq.Header.Set("Authorization", "Bearer dev-token")
	syncReq.Header.Set("Content-Type", "application/json")
	syncResp := httptest.NewRecorder()
	mux.ServeHTTP(syncResp, syncReq)
	if syncResp.Code != http.StatusOK {
		t.Fatalf("sync failed: %d %s", syncResp.Code, syncResp.Body.String())
	}
	var syncBody core.SyncResponse
	if err := json.Unmarshal(syncResp.Body.Bytes(), &syncBody); err != nil {
		t.Fatalf("sync body: %v", err)
	}
	if len(syncBody.Conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %#v", syncBody.Conflicts)
	}
	if len(syncBody.AckedOpIDs) != 1 {
		t.Fatalf("expected one ack, got %#v", syncBody)
	}
}

func TestSyncBatchAtomicity(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)
	mux := http.NewServeMux()
	s.Register(mux)

	prepareReq := httptest.NewRequest(http.MethodPost, "/v1/prepare?workflow_id=support_ticket", bytes.NewReader([]byte(`{"preconditions":{"scope":"demo"}}`)))
	prepareReq.Header.Set("Authorization", "Bearer dev-token")
	prepareResp := httptest.NewRecorder()
	mux.ServeHTTP(prepareResp, prepareReq)
	var prepBody struct {
		PrepareToken string `json:"prepare_token"`
	}
	_ = json.Unmarshal(prepareResp.Body.Bytes(), &prepBody)

	payload := core.SyncRequest{ClientID: "device-2", Ops: []core.Operation{
		{OpID: "op1", ObjectID: "ticket:2", ClientID: "device-2", Workflow: "support_ticket", Sequence: 1, Type: "set_field", Path: []string{"contact", "email"}, Value: "bad-email", PrepareToken: prepBody.PrepareToken},
		{OpID: "op2", ObjectID: "ticket:2", ClientID: "device-2", Workflow: "support_ticket", Sequence: 2, Type: "set_field", Path: []string{"message"}, Value: "hello world", PrepareToken: prepBody.PrepareToken},
	}}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer dev-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sync failed: %d", w.Code)
	}
	var body core.SyncResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Conflicts) == 0 {
		t.Fatal("expected conflict")
	}
	if len(body.AppliedEvents) != 0 {
		t.Fatalf("expected atomic rejection with no events")
	}

	payload2 := core.SyncRequest{ClientID: "device-2", Ops: []core.Operation{{
		OpID: "op3", ObjectID: "ticket:2", ClientID: "device-2", Workflow: "support_ticket", Sequence: 1, Type: "set_field", Path: []string{"message"}, Value: "hello world", PrepareToken: prepBody.PrepareToken,
	}}}
	b2, _ := json.Marshal(payload2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/sync", bytes.NewReader(b2))
	req2.Header.Set("Authorization", "Bearer dev-token")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	var body2 core.SyncResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &body2)
	if len(body2.Conflicts) > 0 {
		t.Fatalf("expected follow-up batch to succeed, got conflicts %#v", body2.Conflicts)
	}
	if len(body2.AckedOpIDs) != 1 {
		t.Fatalf("expected ack on second batch")
	}
}

func TestAsyncSyncFlow(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)
	mux := http.NewServeMux()
	s.Register(mux)

	payload := core.SyncRequest{ClientID: "device-1", Ops: []core.Operation{{
		OpID:     "device-1:1",
		ObjectID: "cart:1",
		ClientID: "device-1",
		Sequence: 1,
		Type:     "set_field",
		Path:     []string{"shipping", "city"},
		Value:    "Goa",
	}}}
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
	if accepted.TxID == "" {
		t.Fatal("tx id is empty")
	}

	var statusResp *httptest.ResponseRecorder
	for i := 0; i < 20; i++ {
		reqStatus := httptest.NewRequest(http.MethodGet, "/v1/sync/status?queue_id="+accepted.QueueID, nil)
		reqStatus.Header.Set("Authorization", "Bearer dev-token")
		statusResp = httptest.NewRecorder()
		mux.ServeHTTP(statusResp, reqStatus)
		if statusResp.Code == http.StatusOK {
			var status map[string]any
			_ = json.Unmarshal(statusResp.Body.Bytes(), &status)
			if status["status"] == "completed" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("sync status did not complete: code=%d body=%s", statusResp.Code, statusResp.Body.String())
}

func TestMetricsEndpoint(t *testing.T) {
	staticDir := makeStaticDir(t)
	s := buildServer(t, staticDir)
	mux := http.NewServeMux()
	s.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, metric := range []string{"ops_received_total", "ops_applied_total", "sync_conflicts_total", "sync_errors_total"} {
		if !strings.Contains(body, metric) {
			t.Fatalf("missing metric %s in body %s", metric, body)
		}
	}
}

func buildServer(t *testing.T, staticDir string) *Server {
	t.Helper()
	return buildServerWithKeyID(t, staticDir, "test-key")
}

func buildServerWithKeyID(t *testing.T, staticDir, keyID string) *Server {
	t.Helper()
	signer, err := security.NewSigner(keyID, "")
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
