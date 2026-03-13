package syncer

import (
	"testing"

	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

func TestApplyIdempotent(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)

	req := core.SyncRequest{
		ClientID: "device-1",
		Ops: []core.Op{
			{
				OpID:     "device-1:1",
				ObjectID: "cart:1",
				ClientID: "device-1",
				Clock:    1,
				Type:     "set_field",
				Path:     []string{"shipping", "city"},
				Value:    "Pune",
			},
		},
	}

	first := engine.Apply(req)
	if len(first.AckedOpIDs) != 1 {
		t.Fatalf("expected 1 ack on first apply, got %d", len(first.AckedOpIDs))
	}
	second := engine.Apply(req)
	if len(second.AckedOpIDs) != 1 {
		t.Fatalf("expected duplicate op to still be acknowledged once in response, got %d", len(second.AckedOpIDs))
	}

	obj := st.GetObject("cart:1")
	shipping, ok := obj.Data["shipping"].(map[string]any)
	if !ok {
		t.Fatalf("shipping not present: %#v", obj.Data)
	}
	if shipping["city"] != "Pune" {
		t.Fatalf("unexpected city value: %#v", shipping["city"])
	}
}

func TestInventoryConflict(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)

	req := core.SyncRequest{
		ClientID: "device-2",
		Ops: []core.Op{
			{
				OpID:     "device-2:1",
				ObjectID: "cart:2",
				ClientID: "device-2",
				Clock:    1,
				Type:     "add_item",
				Value: map[string]any{
					"sku": "sku-2",
					"qty": float64(1),
				},
			},
		},
	}

	resp := engine.Apply(req)
	if len(resp.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(resp.Conflicts))
	}
	if resp.Conflicts[0].Reason != "inventory_zero" {
		t.Fatalf("unexpected reason: %s", resp.Conflicts[0].Reason)
	}
	if len(resp.AckedOpIDs) != 0 {
		t.Fatalf("conflict op should not be acked")
	}
}

func TestEventualValidationFailure(t *testing.T) {
	st := store.NewMemoryStore()
	engine := NewEngine(st)

	req := core.SyncRequest{
		ClientID: "device-3",
		Ops: []core.Op{
			{
				OpID:     "device-3:1",
				ObjectID: "ticket:1",
				ClientID: "device-3",
				Workflow: "support_ticket",
				Clock:    1,
				Type:     "set_field",
				Path:     []string{"contact", "email"},
				Value:    "invalid-email",
			},
		},
	}

	resp := engine.Apply(req)
	if len(resp.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(resp.Conflicts))
	}
	if resp.Conflicts[0].Reason != "validation_failed" {
		t.Fatalf("unexpected reason: %s", resp.Conflicts[0].Reason)
	}
	if len(resp.AckedOpIDs) != 0 {
		t.Fatalf("validation-failed op should not be acked")
	}
}
