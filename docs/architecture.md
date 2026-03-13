# Seed Architecture

Docs index: [`README.md`](README.md)

Seed is a backend runtime for offline workflow execution and sync.

## Flow

1. Client fetches signed workflow manifest from `GET /v1/manifest`.
2. Client fetches JWKS from `GET /.well-known/dwce-keys`.
3. Client verifies `manifest_jws` by `kid` and expiry.
4. Client caches resources and stores operations in local outbox.
5. Client sends batched operations to `POST /v1/sync`.
6. Backend validates operations, applies event batch, updates projection state.
7. Backend returns `tx_id`, applied event IDs, and conflicts.

## Backend modules

- `internal/api`
- `internal/syncer`
- `internal/store`
- `internal/security`
- `cmd/server`
- `cmd/admin`

## Data model

- operation (`op_id`, `object_id`, `sequence_number`, `payload`)
- event (`event_id`, `tx_id`, `op_id`, `payload`)
- projection (`object_id`, `state`, `last_sequence`)
- async job (`job_id`, `status`, `response`)

## Safety classes

- `SAFE`: offline execution allowed.
- `EVENTUAL`: offline execution allowed with sync validation.
- `UNSAFE`: offline execution blocked; use prepare+online completion.
