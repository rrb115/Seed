# Seed DWCE Setup Guide

This repo has two parts:

- `backend/` Go API and sync engine
- `frontend/` client for API checks

## Current backend state

- Storage uses in-memory maps in `backend/internal/store/memory.go`.
- Signing uses Ed25519 in `backend/internal/security/signer.go`.
- Key discovery endpoint: `GET /.well-known/dwce-keys`.
- Manifest endpoint: `GET /v1/manifest`.
- Sync endpoints: `POST /v1/sync`, `GET /v1/sync/status`.
- Resource check endpoint: `POST /v1/verify-resource`.

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

## What teams must rewire for their app

1. Workflow list and step graph
- File: `backend/internal/api/server.go`
- Update `manifestTemplates()` and `workflowGraphs()`.
- Add your workflow IDs, steps, and resource paths.
- Set safety flags for each workflow.

2. Auth check
- File: `backend/internal/api/server.go`
- Replace `withAuth()` token compare with your auth flow.
- Validate user and scopes before manifest and sync routes.

3. Signing key source
- File: `backend/internal/security/signer.go`
- Keep Ed25519 format.
- Replace local seed source with your key source if needed.
- Keep `kid` stable for active key set.

4. Sync validation rules
- File: `backend/internal/syncer/engine.go`
- Update `validateWorkflowOperation()` with app rules.
- Keep sequence checks and op-id dedupe checks.

5. Store layer
- File: `backend/internal/store/memory.go`
- Keep map store for local use.
- If needed, replace with your store and keep method behavior:
  - op dedupe lookup
  - object read/write
  - sync status read/write
  - last applied sequence tracking

6. API contract fields
- File: `backend/internal/core/types.go`
- Keep these op fields in client payloads:
  - `op_id`
  - `object_id`
  - `client_id`
  - `workflow`
  - `sequence_number` (or `clock` fallback)
  - `type`, `path`, `value`
- Keep these sync request fields:
  - `client_id`
  - `manifest_version`
  - `ops[]`

## Integration steps for another web app

1. Add a client queue
- Store ops in browser storage.
- Increment `sequence_number` per `object_id`.

2. Fetch manifest
- Call `GET /v1/manifest?goal=<workflow>`.
- Verify signature using keys from `GET /.well-known/dwce-keys`.
- Cache listed resources.

3. Record actions
- Convert each user action to an op.
- Save op before showing saved state in UI.

4. Sync ops
- Send batches to `POST /v1/sync`.
- If `202`, poll `GET /v1/sync/status?queue_id=<id>`.

5. Apply result
- For each op, handle status by server response:
  - acked
  - conflict/rejected
- Update local state and UI state.

6. Handle unsafe workflows
- If manifest call returns `409 workflow_offline_unsafe`, block run without network.
- Keep user input as draft if needed.

## Where to plug this into your app

- Web app backend service layer:
  - call manifest and sync APIs
- Workflow module:
  - map UI actions to op payloads
- Cache module:
  - cache resources from signed manifest
- Sync module:
  - batch submit and status polling

## Frontend folder use

- `frontend/` is for API checks and flow checks.
- You can replace it with your app client.
- Backend APIs run without this folder if you provide another client.
