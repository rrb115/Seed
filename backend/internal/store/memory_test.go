package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
)

func TestAppendEventsTxAtomicFailure(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()

	ops := []core.Operation{
		{OpID: "op-1", ObjectID: "note:1", Sequence: 1, Type: "set_field", Path: []string{"content"}, Value: "hello"},
		{OpID: "op-2", ObjectID: "note:1", Sequence: 2, Type: "set_field", Path: []string{}, Value: "bad"},
	}

	_, err := st.AppendEventsTx(ctx, uuid.New(), ops)
	if err == nil {
		t.Fatal("expected append error")
	}

	events, err := st.ListEvents(ctx, "note:1", time.Time{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events after atomic failure, got %d", len(events))
	}

	state, seq, err := st.GetObjectState(ctx, "note:1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if string(state) != "{}" || seq != 0 {
		t.Fatalf("expected empty projection, got state=%s seq=%d", string(state), seq)
	}
}

func TestMarkOpSeenDuplicate(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()

	if err := st.MarkOpSeen(ctx, "op-1"); err != nil {
		t.Fatalf("first mark failed: %v", err)
	}
	if err := st.MarkOpSeen(ctx, "op-1"); err == nil {
		t.Fatal("expected duplicate mark error")
	}
}

func TestReplayEvents(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()

	ops := []core.Operation{{OpID: "op-1", ObjectID: "note:1", Sequence: 1, Type: "set_field", Path: []string{"content"}, Value: "hello"}}
	if _, err := st.AppendEventsTx(ctx, uuid.New(), ops); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := st.ReplayEvents(ctx, "note:1", time.Time{}); err != nil {
		t.Fatalf("replay: %v", err)
	}

	state, seq, err := st.GetObjectState(ctx, "note:1")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if seq != 1 {
		t.Fatalf("expected seq 1 got %d", seq)
	}
	if string(state) == "{}" {
		t.Fatalf("expected state content")
	}
}
