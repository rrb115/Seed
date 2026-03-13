package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"seed/backend/internal/api"
	"seed/backend/internal/core"
	"seed/backend/internal/security"
	"seed/backend/internal/store"
	"seed/backend/internal/syncer"
)

func TestOfflineQueueToSyncFlow(t *testing.T) {
	server := newHarness(t)
	mux := http.NewServeMux()
	server.Register(mux)

	manifestReq := httptest.NewRequest(http.MethodGet, "/v1/manifest?goal=note_draft", nil)
	manifestReq.Header.Set("Authorization", "Bearer dev-token")
	manifestResp := httptest.NewRecorder()
	mux.ServeHTTP(manifestResp, manifestReq)
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest request failed: %d", manifestResp.Code)
	}

	var manifest core.GoalManifest
	_ = json.Unmarshal(manifestResp.Body.Bytes(), &manifest)

	keysReq := httptest.NewRequest(http.MethodGet, "/.well-known/dwce-keys", nil)
	keysResp := httptest.NewRecorder()
	mux.ServeHTTP(keysResp, keysReq)
	var keyBody struct {
		Keys []security.PublicJWK `json:"keys"`
	}
	_ = json.Unmarshal(keysResp.Body.Bytes(), &keyBody)

	if _, _, err := security.VerifyCompactJWS(manifest.ManifestJWS, keyBody.Keys); err != nil {
		t.Fatalf("manifest verification failed: %v", err)
	}

	// Simulated offline outbox persisted locally.
	outbox := []core.Operation{
		{OpID: "off:1", ObjectID: "note:77", ClientID: "device-off", Workflow: "note_draft", Sequence: 1, Type: "set_field", Path: []string{"content"}, Value: "draft"},
		{OpID: "off:2", ObjectID: "note:77", ClientID: "device-off", Workflow: "note_draft", Sequence: 2, Type: "set_field", Path: []string{"title"}, Value: "offline title"},
	}

	syncReqBody, _ := json.Marshal(core.SyncRequest{ClientID: "device-off", ClientTxID: "", Ops: outbox})
	syncReq := httptest.NewRequest(http.MethodPost, "/v1/sync", bytes.NewReader(syncReqBody))
	syncReq.Header.Set("Authorization", "Bearer dev-token")
	syncReq.Header.Set("X-Trace-Id", "trace-offline-flow")
	syncResp := httptest.NewRecorder()
	mux.ServeHTTP(syncResp, syncReq)
	if syncResp.Code != http.StatusOK {
		t.Fatalf("sync failed: %d body=%s", syncResp.Code, syncResp.Body.String())
	}

	var syncBody core.SyncResponse
	_ = json.Unmarshal(syncResp.Body.Bytes(), &syncBody)
	if len(syncBody.Conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %#v", syncBody.Conflicts)
	}
	if len(syncBody.AckedOpIDs) != 2 {
		t.Fatalf("expected 2 acked ops, got %#v", syncBody.AckedOpIDs)
	}

	// Re-submit same outbox to assert idempotence.
	syncReq2 := httptest.NewRequest(http.MethodPost, "/v1/sync", bytes.NewReader(syncReqBody))
	syncReq2.Header.Set("Authorization", "Bearer dev-token")
	syncResp2 := httptest.NewRecorder()
	mux.ServeHTTP(syncResp2, syncReq2)
	if syncResp2.Code != http.StatusOK {
		t.Fatalf("second sync failed: %d", syncResp2.Code)
	}
	var syncBody2 core.SyncResponse
	_ = json.Unmarshal(syncResp2.Body.Bytes(), &syncBody2)
	if len(syncBody2.AckedOpIDs) != 2 {
		t.Fatalf("expected duplicate replay to ack same ops")
	}
}

func newHarness(t *testing.T) *api.Server {
	t.Helper()
	signer, err := security.NewSigner("integration-key", "")
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	st := store.NewMemoryStore()
	engine := syncer.NewEngine(st)
	return api.NewServer(signer, st, engine, makeStaticDir(t), "dev-token")
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
