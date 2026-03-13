# Seed

Seed is an offline workflow runtime reference.

## What Seed includes

- backend APIs for manifests, prepare tokens, sync, status, metrics, and key distribution
- sync engine with validation, conflict handling, and batch apply semantics
- in-memory store adapter and store interface for pluggable persistence
- adapter templates for Postgres and event-store style backends
- demo frontend runtime and service worker for backend integration tests

## Repository structure

- `backend/` Go services, engine, store interface, admin CLI, tests
- `frontend/` demo client and service worker
- `docs/` architecture, design, security, migration, and integration docs

Detailed map: [docs/code_map.md](docs/code_map.md)

## Core entry points

- backend server: [`backend/cmd/server/main.go`](backend/cmd/server/main.go)
- admin CLI: [`backend/cmd/admin/main.go`](backend/cmd/admin/main.go)
- API handlers: [`backend/internal/api/server.go`](backend/internal/api/server.go)
- sync engine: [`backend/internal/syncer/engine.go`](backend/internal/syncer/engine.go)
- store interface: [`backend/internal/store/store.go`](backend/internal/store/store.go)
- memory adapter: [`backend/internal/store/memory.go`](backend/internal/store/memory.go)
- signer and JWKS/JWS helpers: [`backend/internal/security/signer.go`](backend/internal/security/signer.go)

## API endpoints

- `GET /.well-known/dwce-keys`
- `GET /v1/manifest`
- `POST /v1/prepare?workflow_id=<id>`
- `POST /v1/sync`
- `GET /v1/sync/status?queue_id=<id>`
- `POST /v1/verify-resource`
- `GET /metrics`

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

## Admin CLI

```bash
cd backend
go run ./cmd/admin events --object note:1 --from 2026-03-13T00:00:00Z --to 2026-03-13T23:59:59Z
go run ./cmd/admin replay --object note:1 --from 2026-03-13T00:00:00Z --to 2026-03-13T23:59:59Z
```

## Store adapters

- memory reference adapter: [`backend/internal/store/memory.go`](backend/internal/store/memory.go)
- Postgres template: [`backend/internal/store/adapters/postgres_template.go`](backend/internal/store/adapters/postgres_template.go)
- event-store template: [`backend/internal/store/adapters/eventstore_template.go`](backend/internal/store/adapters/eventstore_template.go)

Adapter contract: [docs/store_contract.md](docs/store_contract.md)

## Integration steps for external apps

1. Define workflow templates and resource closure maps in backend: [`backend/internal/api/server.go`](backend/internal/api/server.go).
2. Map user actions to operations with `op_id`, `object_id`, `sequence_number`, and `payload`.
3. Keep workflow validation and conflict mapping in backend engine: [`backend/internal/syncer/engine.go`](backend/internal/syncer/engine.go).
4. Verify manifest JWS with JWKS before caching resources.
5. Keep local outbox semantics in client: write, commit, then sync.
6. Replace memory adapter with your own adapter using templates and the store contract.

## Docs index

- docs index: [docs/README.md](docs/README.md)
- architecture: [docs/architecture.md](docs/architecture.md)
- design: [docs/design.md](docs/design.md)
- security: [docs/security.md](docs/security.md)
- client integration: [docs/client.md](docs/client.md)
- migration: [docs/migration.md](docs/migration.md)
- store contract: [docs/store_contract.md](docs/store_contract.md)
- code map: [docs/code_map.md](docs/code_map.md)

## References

- TUF: [The Update Framework](https://theupdateframework.io/)
- Event sourcing pattern: [Microsoft Learn](https://learn.microsoft.com/azure/architecture/patterns/event-sourcing)
- IndexedDB outbox reference: [Dexie docs](https://dexie.org/docs/)
- Background Sync reference: [MDN](https://developer.mozilla.org/docs/Web/API/Background_Synchronization_API)
