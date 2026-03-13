package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"seed/backend/internal/api"
	"seed/backend/internal/security"
	"seed/backend/internal/store"
	"seed/backend/internal/syncer"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "HTTP listen address")
	staticDir := flag.String("static-dir", filepath.Clean(filepath.Join("..", "frontend")), "directory to serve static client files from")
	apiToken := flag.String("api-token", getenvDefault("API_TOKEN", "dev-token"), "bearer token for API calls")
	manifestKeyID := flag.String("manifest-key-id", getenvDefault("MANIFEST_KEY_ID", "dev-key-1"), "manifest signing key id")
	manifestSeed := flag.String("manifest-seed-b64", os.Getenv("MANIFEST_PRIVATE_SEED_B64"), "base64 Ed25519 seed (32 bytes decoded)")
	flag.Parse()

	signer, err := security.NewSigner(*manifestKeyID, *manifestSeed)
	if err != nil {
		log.Fatalf("failed to initialize signer: %v", err)
	}

	st := store.NewMemoryStore()
	engine := syncer.NewEngine(st)
	apiServer := api.NewServer(signer, st, engine, *staticDir, *apiToken)

	mux := http.NewServeMux()
	apiServer.Register(mux)

	fileServer := http.FileServer(http.Dir(*staticDir))
	mux.Handle("/", fileServer)

	httpServer := &http.Server{
		Addr:              *listenAddr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Seed offline cache server listening on %s", *listenAddr)
	log.Printf("Static dir: %s", *staticDir)
	log.Printf("Manifest public key (base64): %s", signer.PublicKeyBase64())
	log.Printf("API token (dev): %s", *apiToken)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

func getenvDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
