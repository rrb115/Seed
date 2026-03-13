# Seed Client Integration Notes

Docs index: [`README.md`](README.md)

Seed backend is the source of workflow rules, validation, sync semantics, and signature checks.

Frontend code is a demo harness.

## 1. Outbox pattern

Recommended browser outbox model:

- local DB for pending operations
- write operation and wait for commit before UI confirmation
- send operation batches on reconnect

Reference for IndexedDB outbox APIs: [Dexie docs](https://dexie.org/docs/)

Current demo implementation uses a minimal IndexedDB wrapper in `frontend/dwce/storage.js`.

## 2. Service worker responsibilities

- import manifest
- fetch JWKS from `/.well-known/dwce-keys`
- verify `manifest_jws` with `kid`
- verify `expires_at` and version monotonicity
- verify resource `sha256` digest before cache write
- trigger sync via Background Sync tag

Reference for Background Sync behavior: [MDN Background Sync](https://developer.mozilla.org/docs/Web/API/Background_Synchronization_API)

## 3. Required sync request fields

`POST /v1/sync`:

- `client_tx_id` (optional)
- `client_id`
- `ops[]`
  - `op_id`
  - `object_id`
  - `sequence_number`
  - `payload` or compatibility fields (`type`, `path`, `value`)
  - `prepare_token` when workflow requires it

## 4. Offline close and resume flow

1. user action writes op to IndexedDB
2. UI shows saved state after commit
3. tab closes while offline
4. on reconnect:
- Background Sync triggers service worker, or
- app init scans outbox and posts to `/v1/sync`

## 5. Conflict UI states

Use these states in client UI:

- `Draft`
- `Pending Sync`
- `Validated`
- `Rejected`
