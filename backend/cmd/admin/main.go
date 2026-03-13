package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/admin"
	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	ctx := context.Background()
	st := store.NewMemoryStore()

	cmd := os.Args[1]
	switch cmd {
	case "events":
		eventsCmd(ctx, st, os.Args[2:])
	case "replay":
		replayCmd(ctx, st, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func eventsCmd(ctx context.Context, st store.Store, args []string) {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	objectID := fs.String("object", "", "object id")
	fromRaw := fs.String("from", "", "RFC3339 timestamp")
	toRaw := fs.String("to", "", "RFC3339 timestamp")
	seedPath := fs.String("seed", "", "optional seed operations JSON file")
	_ = fs.Parse(args)

	if *objectID == "" {
		fmt.Fprintln(os.Stderr, "--object is required")
		os.Exit(2)
	}
	if err := seedStore(ctx, st, *seedPath); err != nil {
		fmt.Fprintln(os.Stderr, "seed error:", err)
		os.Exit(2)
	}

	from, err := parseTime(*fromRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid --from:", err)
		os.Exit(2)
	}
	to, err := parseTime(*toRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid --to:", err)
		os.Exit(2)
	}

	events, err := admin.ListEvents(ctx, st, *objectID, from, to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list events error:", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(events)
}

func replayCmd(ctx context.Context, st store.Store, args []string) {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	objectID := fs.String("object", "", "object id")
	fromRaw := fs.String("from", "", "RFC3339 timestamp")
	toRaw := fs.String("to", "", "RFC3339 timestamp")
	seedPath := fs.String("seed", "", "optional seed operations JSON file")
	_ = fs.Parse(args)

	if *objectID == "" {
		fmt.Fprintln(os.Stderr, "--object is required")
		os.Exit(2)
	}
	if err := seedStore(ctx, st, *seedPath); err != nil {
		fmt.Fprintln(os.Stderr, "seed error:", err)
		os.Exit(2)
	}

	from, err := parseTime(*fromRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid --from:", err)
		os.Exit(2)
	}
	_, _ = parseTime(*toRaw)

	if err := admin.ReplayObject(ctx, st, *objectID, from); err != nil {
		fmt.Fprintln(os.Stderr, "replay error:", err)
		os.Exit(1)
	}
	state, lastSeq, err := st.GetObjectState(ctx, *objectID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "state read error:", err)
		os.Exit(1)
	}
	fmt.Printf("{\"object_id\":%q,\"last_sequence\":%d,\"state\":%s}\n", *objectID, lastSeq, state)
}

func seedStore(ctx context.Context, st store.Store, seedPath string) error {
	if seedPath == "" {
		return nil
	}
	b, err := os.ReadFile(seedPath)
	if err != nil {
		return err
	}
	var req core.SyncRequest
	if err := json.Unmarshal(b, &req); err != nil {
		return err
	}
	_, err = st.AppendEventsTx(ctx, uuid.New(), req.Ops)
	return err
}

func parseTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  admin events --object <id> --from <ts> --to <ts> [--seed file]")
	fmt.Println("  admin replay --object <id> --from <ts> --to <ts> [--seed file]")
}
