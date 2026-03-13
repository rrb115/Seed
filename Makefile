.PHONY: run test fmt

run:
	cd backend && go run ./cmd/server -listen :8080 -static-dir ../frontend -api-token dev-token

test:
	cd backend && go test ./...

fmt:
	cd backend && gofmt -w ./cmd ./internal
