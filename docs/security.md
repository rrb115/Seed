# Seed Security Model

Docs index: [`README.md`](README.md)

Seed uses signed manifests, JWKS key distribution, and token checks for prepared workflows.

## 1. Manifest root-of-trust

1. Server signs each manifest payload as compact JWS (`manifest_jws`) with Ed25519.
2. Server publishes public keys at `GET /.well-known/dwce-keys`.
3. Client verifies JWS using `kid` from JWS header and JWKS key set.
4. Client rejects manifest if:
- signature check fails
- `kid` is unknown
- `expires_at` is in the past
- version is lower than cached version

This follows TUF trust anchoring and key rotation guidance.
Reference: [The Update Framework](https://theupdateframework.io/)

## 2. Prepare token checks

Endpoint: `POST /v1/prepare?workflow_id=<id>`

- Returns signed prepare token JWS with claims:
  - `workflow_id`
  - `issued_at`
  - `valid_from`
  - `expires_at`
  - `nonce`
  - `preconditions`

Sync engine validation on `POST /v1/sync`:

- JWS signature valid
- token time window valid
- nonce exists in backend prepared-token registry
- workflow in token equals operation workflow

Failure returns conflict reason `prepare_token_invalid`.

## 3. Replay and dedupe controls

- Every operation has `op_id`.
- Backend dedupes by `op_id` and treats duplicates as no-op.
- Batch apply is atomic to avoid partial writes.

## 4. Transport and request security

- Use TLS in deployments.
- API endpoints require bearer auth.
- Client sends `X-Trace-Id`; server logs this for correlation.

## 5. Key rotation flow

1. Add new signing key in signer service.
2. Publish new key in JWKS endpoint.
3. Start signing manifests with new `kid`.
4. Keep prior key during overlap window.
5. Remove old key after all clients refresh key set.
