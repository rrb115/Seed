# Seed Migration Guide: Memory Store to Production Adapter

Docs index: [`README.md`](README.md)

This repo ships with memory store only.

Use this guide to replace it in downstream deployments.

## 1. Keep interface stable

Implement `backend/internal/store/store.go` in your adapter package.

Do not change API handlers or sync engine call contracts.

## 2. Build append-only schema

Postgres example schema:

```sql
create table op_seen (
  op_id text primary key,
  seen_at timestamptz not null default now()
);

create table events (
  event_id uuid primary key,
  tx_id uuid not null,
  op_id text not null unique,
  object_id text not null,
  sequence_number bigint not null,
  payload jsonb not null,
  created_at timestamptz not null default now()
);

create table object_projection (
  object_id text primary key,
  state jsonb not null,
  last_sequence bigint not null
);

create table sync_jobs (
  job_id uuid primary key,
  status text not null,
  response jsonb,
  error text,
  updated_at timestamptz not null default now()
);
```

## 3. Implement adapter methods

Use templates:

- `backend/internal/store/adapters/postgres_template.go`
- `backend/internal/store/adapters/eventstore_template.go`

Fill TODO sections with concrete persistence logic.

## 4. Wire adapter in server bootstrap

In `backend/cmd/server/main.go` replace:

```go
st := store.NewMemoryStore()
```

with your adapter constructor.

## 5. Validation checks before rollout

1. duplicate `op_id` returns dedupe behavior.
2. mixed valid/invalid batch does not persist partial events.
3. replay rebuild produces expected state.
4. async status survives process restart.

## 6. Rollout plan

1. run adapter in shadow mode in staging.
2. compare sync outputs with memory adapter for fixture requests.
3. enable canary traffic.
4. enable full traffic after conflict/error rates match baseline.
