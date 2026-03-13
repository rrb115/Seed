# Seed Implementation Guide

## What Seed is

Seed is an implementation for workflow cache, local operation queue, and sync APIs.

Seed has two parts:

- `backend/`: Go server for manifest, sync, and key endpoints.
- `frontend/`: client used to call APIs and test workflow flows.

Seed is the implementation name in this repo.

## How Seed works

1. Client calls `GET /v1/manifest?goal=<workflow>`.
2. Server returns a signed manifest with workflow steps and resources.
3. Client stores resources and records user operations in local storage.
4. Client sends batched operations to `POST /v1/sync`.
5. Server validates and applies operations.
6. Client reads result from sync response or `GET /v1/sync/status` when async mode is used.

## Code map

- Server entry: `backend/cmd/server/main.go`
- API routes and workflow maps: `backend/internal/api/server.go`
- Data models: `backend/internal/core/types.go`
- Manifest signing: `backend/internal/security/signer.go`
- Sync rules and apply logic: `backend/internal/syncer/engine.go`
- In-memory store: `backend/internal/store/memory.go`
- Client runtime modules: `frontend/dwce/*.js`
- Service worker: `frontend/sw.js`

## API list

- `GET /.well-known/dwce-keys`
- `GET /v1/manifest`
- `POST /v1/sync`
- `GET /v1/sync/status`
- `POST /v1/verify-resource`

## Run

```bash
cd backend
go run ./cmd/server -listen :8080 -static-dir ../frontend -api-token dev-token
```

Open `http://localhost:8080`.

## Test

```bash
cd backend
go test ./...
```

## What to change for your web app

1. Workflow IDs, steps, and resource paths
- File: `backend/internal/api/server.go`
- Update `manifestTemplates()` and `workflowGraphs()`.

2. Auth check
- File: `backend/internal/api/server.go`
- Replace token compare in `withAuth()` with your auth check.

3. Signing key source
- File: `backend/internal/security/signer.go`
- Keep Ed25519 output and `kid` values.
- Replace seed source if your key source is different.

4. Sync rule set
- File: `backend/internal/syncer/engine.go`
- Update `validateWorkflowOperation()` with your rule set.
- Keep op-id dedupe and sequence checks.

5. Storage layer
- File: `backend/internal/store/memory.go`
- Current implementation uses in-memory maps.
- Keep map store or replace with your store.
- Keep behavior for:
  - op dedupe lookup
  - object read/write
  - sync status read/write
  - last applied sequence tracking

## Client integration steps for another app

1. Build local queue
- Store operations in browser storage.
- Increment `sequence_number` per `object_id`.

2. Fetch and verify manifest
- Call `GET /v1/manifest?goal=<workflow>`.
- Fetch keys from `GET /.well-known/dwce-keys`.
- Verify signature before caching resources.

3. Record operations
- Map each user action to one operation payload.
- Save operation before showing saved state.

4. Sync
- Send batches to `POST /v1/sync`.
- If response is `202`, poll `GET /v1/sync/status?queue_id=<id>`.

5. Apply server response
- Update local state with acked results.
- Handle conflicts from response.

## Notes

- Current storage in backend is in-memory map based.
- Teams can replace store and auth parts based on app needs.
