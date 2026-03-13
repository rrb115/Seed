# Seed Store Contract

Docs index: [`README.md`](README.md)

This contract defines what any Seed store adapter must provide.

## Interface

`backend/internal/store/store.go`

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

## Required semantics

1. `AppendEventsTx`
- Writes all events in one transaction.
- If one operation fails, no event from that batch is committed.
- Event order in a transaction follows request order.

2. `MarkOpSeen`
- Must return duplicate error for existing `op_id`.
- Duplicate behavior must be stable across process restarts.

3. `GetObjectState`
- Returns projection state and `lastSeq` for one object.
- Returned `lastSeq` is the highest applied sequence.

4. `ListEvents`
- Returns append-only event history for one object since timestamp.

5. `ReplayEvents`
- Rebuilds projection state from event stream from timestamp.

6. Async job methods
- `EnqueueJob` persists job with state `queued`.
- `GetJobStatus` returns state transitions and response payload.
- `SetJobStatus` updates status transitions and final response payload.

## Adapter expectations

- Transaction boundary must include event append and projection updates.
- Unique constraint for `op_id` must exist.
- Read-your-write consistency is required for sync response reads.
- Time fields should use UTC.

## Error mapping

- Duplicate operation: map backend duplicate-key error to `store.ErrOpSeen`.
- Missing job: map to `store.ErrJobNotFound`.

## Reference adapters

- In-memory: `backend/internal/store/memory.go`
- Postgres template: `backend/internal/store/adapters/postgres_template.go`
- Event-store template: `backend/internal/store/adapters/eventstore_template.go`
