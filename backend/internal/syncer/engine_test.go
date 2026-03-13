package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

func TestApplyIdempotent(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)

	req := core.SyncRequest{
		ClientID: "device-1",
		Ops: []core.Operation{{
			OpID:     "device-1:1",
			ObjectID: "cart:1",
			ClientID: "device-1",
			Clock:    1,
			Type:     "set_field",
			Path:     []string{"shipping", "city"},
			Value:    "Pune",
		}},
	}

	first := engine.Apply(context.Background(), req, uuid.New())
	if len(first.AckedOpIDs) != 1 {
		t.Fatalf("expected 1 ack on first apply, got %d", len(first.AckedOpIDs))
	}
	second := engine.Apply(context.Background(), req, uuid.New())
	if len(second.AckedOpIDs) != 1 {
		t.Fatalf("expected duplicate op to be acked once, got %d", len(second.AckedOpIDs))
	}

	stateRaw, _, err := st.GetObjectState(context.Background(), "cart:1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	var obj map[string]any
	_ = json.Unmarshal(stateRaw, &obj)
	shipping, ok := obj["shipping"].(map[string]any)
	if !ok {
		t.Fatalf("shipping not present: %#v", obj)
	}
	if shipping["city"] != "Pune" {
		t.Fatalf("unexpected city value: %#v", shipping["city"])
	}
}

func TestBatchConflictIsAtomic(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)

	req := core.SyncRequest{ClientID: "device-2", Ops: []core.Operation{
		{OpID: "op-1", ObjectID: "ticket:1", ClientID: "device-2", Sequence: 1, Workflow: "support_ticket", Type: "set_field", Path: []string{"contact", "email"}, Value: "invalid"},
		{OpID: "op-2", ObjectID: "ticket:1", ClientID: "device-2", Sequence: 2, Workflow: "support_ticket", Type: "set_field", Path: []string{"message"}, Value: "hello world"},
	}}

	resp := engine.Apply(context.Background(), req, uuid.New())
	if len(resp.Conflicts) == 0 {
		t.Fatalf("expected conflicts")
	}
	if len(resp.AppliedEvents) != 0 {
		t.Fatalf("expected no applied events on atomic conflict")
	}

	events, err := st.ListEvents(context.Background(), "ticket:1", time.Time{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected zero events, got %d", len(events))
	}
}

func TestRejectOutOfOrderSequence(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)

	first := core.SyncRequest{ClientID: "device-seq", Ops: []core.Operation{{
		OpID:     "seq:2",
		ObjectID: "ticket:1",
		ClientID: "device-seq",
		Sequence: 2,
		Type:     "set_field",
		Path:     []string{"content"},
		Value:    "newer",
	}}}
	firstResp := engine.Apply(context.Background(), first, uuid.New())
	if len(firstResp.AckedOpIDs) != 1 {
		t.Fatalf("expected first op acked, got %#v", firstResp)
	}

	second := core.SyncRequest{ClientID: "device-seq", Ops: []core.Operation{{
		OpID:     "seq:1",
		ObjectID: "ticket:1",
		ClientID: "device-seq",
		Sequence: 1,
		Type:     "set_field",
		Path:     []string{"content"},
		Value:    "older",
	}}}
	secondResp := engine.Apply(context.Background(), second, uuid.New())
	if len(secondResp.Conflicts) != 1 {
		t.Fatalf("expected one conflict, got %#v", secondResp)
	}
	if secondResp.Conflicts[0].Reason != "out_of_order_sequence" {
		t.Fatalf("unexpected reason: %s", secondResp.Conflicts[0].Reason)
	}
}

func TestPrepareTokenValidation(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)
	engine.SetPrepareRequirements(map[string]bool{"checkout": true})
	engine.SetPrepareValidator(staticPrepareValidator{err: errors.New("invalid")})

	req := core.SyncRequest{ClientID: "device-3", Ops: []core.Operation{{
		OpID:     "prep:1",
		ObjectID: "cart:1",
		ClientID: "device-3",
		Workflow: "checkout",
		Sequence: 1,
		Type:     "set_field",
		Path:     []string{"shipping", "city"},
		Value:    "Goa",
	}}}

	resp := engine.Apply(context.Background(), req, uuid.New())
	if len(resp.Conflicts) != 1 {
		t.Fatalf("expected one conflict, got %#v", resp)
	}
	if resp.Conflicts[0].Reason != "prepare_token_invalid" {
		t.Fatalf("unexpected reason: %s", resp.Conflicts[0].Reason)
	}
}

func TestConflictHandlers(t *testing.T) {
	registry := NewRegistry()
	reject := registry.Resolve("reject")
	lww := registry.Resolve("lww")

	action, _ := reject.HandleConflict(context.Background(), core.Operation{}, json.RawMessage(`{}`))
	if action.Apply {
		t.Fatal("reject handler should not apply")
	}

	op := core.Operation{Timestamp: time.Now().UTC()}
	state := json.RawMessage(`{"_meta":{"updated_at":"2000-01-01T00:00:00Z"}}`)
	action, _ = lww.HandleConflict(context.Background(), op, state)
	if !action.Apply {
		t.Fatal("lww handler should apply newer operation")
	}
}

type staticPrepareValidator struct {
	err error
}

func (v staticPrepareValidator) ValidatePrepareToken(_ context.Context, _ string, _ string) error {
	return v.err
}
