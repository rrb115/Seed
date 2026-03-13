# DWCE Architecture Mapping

This document maps the Deterministic Workflow Completion Engine design to the implementation in this repository.

## 0) Safety classes

- `SAFE`
  - Offline deterministic completion is allowed.
- `EVENTUAL`
  - Offline execution allowed, but final validity is server-validated during sync.
- `UNSAFE`
  - Offline completion is disallowed because global mutable state is required.
  - Client stores intent as `Draft` and promotes to sync only when online.

## 1) Runtime model

DWCE is a pluggable runtime layer:

Web App UI -> DWCE SDK -> Service Worker + IndexedDB -> Go Sync Infrastructure

DWCE does not replace app architecture; it augments it with deterministic offline workflow execution.

## 2) Frontend module graph (`frontend/dwce/`)

- `workflow-engine.js`
  - Registers workflows as DAGs.
  - Computes reachable steps.
  - Evaluates safety class from workflow metadata.
- `dependency-graph.js`
  - Stores step->resource and resource->resource edges.
  - Computes minimal transitive closure for a workflow.
- `manifest-manager.js`
  - Calls `GET /v1/manifest` with workflow context.
  - Imports signed manifests via Service Worker.
- `offline-state.js`
  - Applies deterministic local operations into IndexedDB state.
- `op-queue.js`
  - Persists queued operations and retries.
- `sync-agent.js`
  - Flushes batches to `POST /v1/sync`.
  - Handles `202` async queue via `/v1/sync/status`.
- `service-worker-bridge.js`
  - Messaging bridge to SW manifest import.
- `storage.js`
  - Shared IndexedDB layer (`goal_cache_v1`).

The SDK entrypoint is `frontend/dwce/index.js` and the demo consumer is `frontend/app.js`.

## 3) Deterministic closure implementation

### Client side

1. Workflow engine computes reachable steps.
2. Dependency graph computes minimal closure.
3. Manifest manager requests signed manifest for `goal + steps (+ resources)`.
4. Service Worker validates signature/integrity and caches closure resources.
5. Unsafe workflows are blocked from offline manifest import.

### Server side

- `backend/internal/api/server.go` recomputes workflow closure from server-defined graph to enforce deterministic manifests.
- Server evaluates safety (`SAFE`/`EVENTUAL`/`UNSAFE`) and includes it in signed manifest payload.
- Server rejects offline manifest requests for `UNSAFE` workflows with `409 workflow_offline_unsafe`.
- Manifest payload is signed (`backend/internal/security/signer.go`) with Ed25519.

## 4) Offline execution model

During offline mode:

- Mutations are transformed into deterministic ops (`op_id`, `object_id`, `type`, `clock`, `value`).
- Ops are enqueued in IndexedDB (`ops` store) with lifecycle states:
  - `Draft`
  - `Pending Sync`
  - `Validated`
  - `Rejected`
- Local state is updated in IndexedDB (`objects` store).

On reconnect:

- Sync agent posts ops to `/v1/sync`.
- Go sync engine applies idempotent merge logic, runs validation rules, and returns canonical object state plus conflicts.

## 5) Service Worker responsibilities (`frontend/sw.js`)

- Precache app shell.
- Import and verify signed manifests.
- Verify resource SHA-256 integrity before caching.
- Serve cache-first for static resources.
- Signal background sync intent to client runtime.

## 6) Go services

- `GET /.well-known/dwce-keys`
  - Publishes active Ed25519 verification keys (`kid`) as JWK.
- `GET /v1/manifest`
  - Returns signed deterministic workflow closure manifest.
- `POST /v1/sync`
  - Idempotent operation apply with sequence monotonicity checks.
- `GET /v1/sync/status`
  - Poll asynchronous queue completion.
- `POST /v1/verify-resource`
  - Resource CID verification endpoint.

## 7) Security controls in this reference

- Manifest authenticity: Ed25519 signatures
- Resource integrity: SHA-256 CIDs + integrity checks
- API guard: Bearer token
- Replay safety: operation ID dedupe in sync engine

## 8) Production hardening gaps

- Durable op log and canonical state store
- Key rotation with trust chain endpoint
- Strong authn/authz (OIDC/JWT)
- Partitioned workers by object ID
- Full CRDT data-type coverage and migration compatibility
