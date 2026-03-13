package admin

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

func TestListEventsAndReplay(t *testing.T) {
	st := store.NewMemoryStore()
	ctx := context.Background()

	ops := []core.Operation{{
		OpID:     "op-1",
		ObjectID: "note:1",
		Sequence: 1,
		Type:     "set_field",
		Path:     []string{"content"},
		Value:    "hello",
	}}
	if _, err := st.AppendEventsTx(ctx, uuid.New(), ops); err != nil {
		t.Fatalf("append: %v", err)
	}

	events, err := ListEvents(ctx, st, "note:1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}

	if err := ReplayObject(ctx, st, "note:1", time.Time{}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	_, seq, err := st.GetObjectState(ctx, "note:1")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if seq != 1 {
		t.Fatalf("expected seq=1, got %d", seq)
	}
}
