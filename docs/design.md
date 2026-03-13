# Seed Design

Docs index: [`README.md`](README.md)

Seed is a backend-first reference runtime for offline workflow execution and sync.

## 1. Components

- API server
  - Manifest issuance
  - JWKS endpoint
  - Prepare token issuance
  - Sync endpoint
  - Async status endpoint
  - Metrics endpoint
- Sync engine
  - Validation
  - Conflict handling
  - Batch apply
  - Idempotence
- Store adapter
  - Append-only event writes
  - Projection read/replay
  - Job queue status
- Signer
  - JWS signing for manifests and prepare tokens
  - JWKS publish for verification
- Demo client and service worker
  - Manifest fetch and verification
  - Outbox queue and sync

## 2. Store abstraction

File: `backend/internal/store/store.go`

```go
type Store interface {
  AppendEventsTx(ctx context.Context, txID uuid.UUID, ops []core.Operation) ([]uuid.UUID, error)
  GetObjectState(ctx context.Context, objectID string) (state json.RawMessage, lastSeq int64, err error)
  MarkOpSeen(ctx context.Context, opID string) error
  EnqueueJob(ctx context.Context, job core.Job) (jobID uuid.UUID, err error)
  GetJobStatus(ctx context.Context, jobID uuid.UUID) (core.JobStatus, error)
  SetJobStatus(ctx context.Context, jobID uuid.UUID, status core.JobStatus) error
  ListEvents(ctx context.Context, objectID string, since time.Time) ([]core.Event, error)
  ReplayEvents(ctx context.Context, objectID string, from time.Time) error
}
```

Contract details: see [`store_contract.md`](store_contract.md).

## 3. Manifest trust model (JWKS)

Flow:

1. Client fetches manifest from `GET /v1/manifest`.
2. Client fetches keys from `GET /.well-known/dwce-keys`.
3. Client verifies compact JWS in `manifest_jws`.
4. Client verifies:
   - `kid` exists in JWKS
   - signature valid
   - `expires_at` in future
   - monotonic `version`
5. Client rejects manifest if checks fail.

The verification key must not be accepted from manifest response headers.

Reference model: TUF key-anchoring and key-rotation pattern.

## 4. Sync semantics

### Request

`POST /v1/sync`

```json
{
  "client_tx_id": "optional",
  "client_id": "device-1",
  "manifest_version": 3,
  "ops": [
    {
      "op_id": "op-1",
      "object_id": "note:1",
      "sequence_number": 1,
      "payload": {"type":"set_field","path":["content"],"value":"x"},
      "prepare_token": "optional-jws"
    }
  ]
}
```

### Response

```json
{
  "tx_id": "uuid",
  "applied_events": ["uuid"],
  "conflicts": [
    {
      "op_id": "op-1",
      "reason": "validation_failed",
      "handler": "reject",
      "suggested_fix": {}
    }
  ],
  "results": [
    {"object_id":"note:1","state":{},"last_applied_sequence":1}
  ]
}
```

### Batch behavior

- Server assigns a `tx_id` when missing.
- Engine validates full batch.
- If conflicts are returned, write is skipped.
- If batch is valid, `AppendEventsTx` stores all ops in one transaction.
- Duplicate `op_id` is no-op.

### Idempotence

- `op_id` dedupe is enforced through `MarkOpSeen`.
- Existing `op_id` does not mutate state and is acknowledged.

## 5. Prepare token flow

Endpoint: `POST /v1/prepare?workflow_id=<id>`

Request:

```json
{ "preconditions": {"region":"apac"} }
```

Response:

```json
{
  "prepare_token": "JWS",
  "valid_from": "2026-03-13T10:00:00Z",
  "expires_at": "2026-03-13T10:15:00Z",
  "nonce": "n-123"
}
```

Validation on sync:

- token signature valid against JWKS keyset
- token not expired
- token nonce exists in prepared-token registry
- workflow in token matches operation workflow

If any check fails: conflict with reason `prepare_token_invalid`.

## 6. Conflict handling model

Registry interface:

```go
type ConflictHandler interface {
  Name() string
  HandleConflict(ctx context.Context, op core.Operation, state json.RawMessage) (Action, map[string]any)
}
```

Built-ins:

- `reject`
- `lww`
- `transform`

Workflow config maps object class to handler.

## 7. Projection model

- Store is event-first: ops are appended as events.
- Object state is projection derived from events.
- `ReplayEvents` rebuilds projection from selected start time.

Reference model: event sourcing pattern.

## 8. Admin operations

CLI (`cmd/admin`):

- `admin events --object <id> --from <ts> --to <ts>`
- `admin replay --object <id> --from <ts> --to <ts>`

Admin package depends on `Store` interface only.

## 9. Testing strategy

- Unit tests
  - signer JWS/JWKS
  - conflict handlers
  - engine atomic batch and dedupe behavior
  - prepare token validation
- Integration tests
  - local harness simulating offline queue and sync
  - async sync status polling
  - key rotation path

CI runs only in-memory adapter tests and integration harness.

## 10. References

- TUF: [https://theupdateframework.io/](https://theupdateframework.io/)
- Event sourcing pattern: [https://learn.microsoft.com/azure/architecture/patterns/event-sourcing](https://learn.microsoft.com/azure/architecture/patterns/event-sourcing)
- Dexie docs: [https://dexie.org/docs/](https://dexie.org/docs/)
- MDN Background Sync: [https://developer.mozilla.org/docs/Web/API/Background_Synchronization_API](https://developer.mozilla.org/docs/Web/API/Background_Synchronization_API)
