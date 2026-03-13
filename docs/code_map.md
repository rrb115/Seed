# Seed Code Map

This file lists the runtime modules and their roles.

Docs index: [`README.md`](README.md)

## Backend

- `backend/cmd/server/main.go`
  - Server bootstrap, route wiring, static file serving.
- `backend/internal/api/server.go`
  - HTTP handlers for manifest, sync, status, keyset, verify, prepare, metrics.
- `backend/internal/syncer/engine.go`
  - Sync apply flow, idempotence, conflict handling, batch processing.
- `backend/internal/syncer/conflict.go`
  - Conflict handler interfaces and built-in handlers.
- `backend/internal/store/store.go`
  - Store contract used by engine, API, and admin.
- `backend/internal/store/memory.go`
  - In-memory store adapter used for runtime and tests.
- `backend/internal/store/adapters/postgres_template.go`
  - Template for a Postgres adapter.
- `backend/internal/store/adapters/eventstore_template.go`
  - Template for an append-only event-store adapter.
- `backend/internal/security/signer.go`
  - Ed25519 signer, JWS encode/verify helpers, JWKS key output.
- `backend/internal/core/types.go`
  - API models, operation/event/job models.
- `backend/internal/core/canonical.go`
  - Canonical encoding helpers for signatures.
- `backend/internal/admin/admin.go`
  - Admin use cases (list events and replay).
- `backend/cmd/admin/main.go`
  - Admin CLI entrypoint.

## Frontend demo client and test surface

- `frontend/dwce/index.js`
  - Runtime entrypoint used by demo app.
- `frontend/dwce/manifest-manager.js`
  - Manifest fetch and import call.
- `frontend/dwce/sync-agent.js`
  - Sync batching and status polling.
- `frontend/dwce/op-queue.js`
  - Local queue write and status transitions.
- `frontend/dwce/storage.js`
  - IndexedDB helpers.
- `frontend/sw.js`
  - Service worker manifest verification, cache handling, sync trigger.
- `frontend/app.js`
  - Demo UI wiring.

## Tests

- `backend/internal/api/server_test.go`
  - API behavior tests.
- `backend/internal/syncer/engine_test.go`
  - Engine behavior tests.
- `backend/internal/security/signer_test.go`
  - Signature and JWS tests.
- `backend/integration/*.go`
  - Integration tests with in-memory store and local client harness.

## CI

- No CI workflow files are committed in this repository.
- Run tests locally with `cd backend && go test ./...`.
