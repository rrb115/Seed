# Seed: Deterministic Workflow Completion Engine (DWCE)

This repository contains an end-to-end DWCE reference implementation that can be attached to web apps as a runtime module.

## What is implemented

- Frontend DWCE SDK with pluggable modules:
  - workflow engine (DAG/reachability)
  - workflow safety classifier (`SAFE`, `EVENTUAL`, `UNSAFE`)
  - dependency graph engine (step/resource maps + transitive closure)
  - manifest manager (signed manifest fetch/import)
  - offline state engine (deterministic local apply)
  - operation queue (durable IndexedDB queue)
  - sync agent (`POST /v1/sync`, async status polling)
  - service worker bridge
- Service Worker with:
  - app shell caching
  - manifest signature verification (Ed25519)
  - resource integrity verification (SHA-256)
  - offline fetch strategies
- Go backend services:
  - `GET /v1/manifest` with deterministic workflow closure support (`goal`, `steps`, `resources`)
  - server-side workflow safety metadata in signed manifests
  - unsafe workflow rejection for offline manifest requests
  - `POST /v1/sync`
  - `GET /v1/sync/status`
  - `POST /v1/verify-resource`

## Safety model

- `SAFE`:
  - Fully offline executable (user-owned state).
- `EVENTUAL`:
  - Offline executable but server validation on sync.
  - UX should treat results as provisional until validated.
- `UNSAFE`:
  - Requires strong global consistency (inventory, seats, balances).
  - Offline execution is blocked; DWCE stores draft intent and executes when online.

## SDK usage

```javascript
import { DWCE } from "./dwce/index.js";

await DWCE.init({ token: "dev-token" });

DWCE.registerWorkflow("note_draft", {
  steps: ["open_editor", "edit_content", "save_draft"],
  safety_class: "SAFE",
});

DWCE.registerWorkflow("support_ticket", {
  steps: ["fill_form", "attach_context", "submit_ticket"],
  requires_validation: true,
});

DWCE.registerStepDependencies("note_draft", {
  open_editor: ["/index.html", "/styles.css", "/app.js"],
  edit_content: ["/index.html", "/styles.css", "/app.js"],
  save_draft: ["/index.html", "/styles.css", "/app.js", "/offline.html"],
});

await DWCE.prepareWorkflow("note_draft");
await DWCE.queueOperation({
  workflow: "note_draft",
  object_id: "note:123",
  type: "set_field",
  path: ["content"],
  value: "Offline draft text",
});
await DWCE.sync();
```

## Project layout

- `backend/`: Go APIs and merge engine
- `frontend/`: demo web app + `dwce/` SDK modules + Service Worker
- `docs/architecture.md`: detailed architecture mapping

## Quick start

```bash
cd backend
go run ./cmd/server -listen :8080 -static-dir ../frontend -api-token dev-token
```

Open [http://localhost:8080](http://localhost:8080).

## Test

```bash
cd backend
go test ./...
```

## Notes

- This is a production-oriented reference, but backend persistence is in-memory for simplicity.
- For production, replace in-memory storage with durable op/state stores and KMS-backed key management.
