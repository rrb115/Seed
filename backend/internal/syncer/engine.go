package syncer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

// Engine applies offline ops with idempotence and simple conflict handling.
type Engine struct {
	store     *store.MemoryStore
	inventory map[string]int64
}

func NewEngine(st *store.MemoryStore) *Engine {
	return &Engine{
		store: st,
		inventory: map[string]int64{
			"sku-1": 5,
			"sku-2": 0,
			"sku-3": 100,
		},
	}
}

func (e *Engine) Apply(req core.SyncRequest) core.SyncResponse {
	acked := make([]string, 0, len(req.Ops))
	conflicts := make([]core.Conflict, 0)
	touched := map[string]struct{}{}

	for _, op := range req.Ops {
		if op.OpID == "" || op.ObjectID == "" || op.ClientID == "" {
			conflicts = append(conflicts, core.Conflict{
				ObjectID:  op.ObjectID,
				OpID:      op.OpID,
				Reason:    "invalid_op",
				Suggested: "drop",
				Metadata: map[string]any{
					"detail": "op_id, object_id, and client_id are required",
				},
			})
			continue
		}

		if e.store.IsAcked(op.OpID) {
			acked = append(acked, op.OpID)
			touched[op.ObjectID] = struct{}{}
			continue
		}

		object := e.store.GetObject(op.ObjectID)
		applied, conflict := e.applyOne(&object, op)
		if conflict != nil {
			conflicts = append(conflicts, *conflict)
			continue
		}
		if applied {
			if op.Clock > object.VersionVector[op.ClientID] {
				object.VersionVector[op.ClientID] = op.Clock
			}
			e.store.PutObject(op.ObjectID, object)
			e.store.MarkAcked(op.OpID)
			acked = append(acked, op.OpID)
			touched[op.ObjectID] = struct{}{}
		}
	}

	sort.Strings(acked)
	results := make([]core.ObjectResult, 0, len(touched))
	for objectID := range touched {
		obj := e.store.GetObject(objectID)
		results = append(results, core.ObjectResult{
			ObjectID:      objectID,
			State:         obj.Data,
			VersionVector: obj.VersionVector,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ObjectID < results[j].ObjectID })

	return core.SyncResponse{
		ServerTime: time.Now().UTC(),
		AckedOpIDs: acked,
		Results:    results,
		Conflicts:  conflicts,
		Status:     "completed",
	}
}

func (e *Engine) applyOne(object *store.ObjectState, op core.Op) (bool, *core.Conflict) {
	if conflict := validateWorkflowOperation(op); conflict != nil {
		return false, conflict
	}

	switch op.Type {
	case "set_field":
		if len(op.Path) == 0 {
			return false, &core.Conflict{
				ObjectID:  op.ObjectID,
				OpID:      op.OpID,
				Reason:    "invalid_path",
				Suggested: "drop",
			}
		}
		setNestedField(object.Data, op.Path, op.Value)
		return true, nil
	case "add_item":
		sku, qty, ok := decodeItem(op.Value)
		if !ok {
			return false, invalidValueConflict(op)
		}
		if conflict := e.ensureInventory(op, sku, qty); conflict != nil {
			return false, conflict
		}
		items := ensureItemsMap(object.Data)
		items[sku] += qty
		return true, nil
	case "remove_item":
		sku := ""
		if v, ok := op.Value.(string); ok {
			sku = strings.TrimSpace(v)
		}
		if sku == "" {
			return false, invalidValueConflict(op)
		}
		items := ensureItemsMap(object.Data)
		delete(items, sku)
		return true, nil
	case "set_quantity":
		sku, qty, ok := decodeItem(op.Value)
		if !ok {
			return false, invalidValueConflict(op)
		}
		if conflict := e.ensureInventory(op, sku, qty); conflict != nil {
			return false, conflict
		}
		items := ensureItemsMap(object.Data)
		items[sku] = qty
		return true, nil
	default:
		return false, &core.Conflict{
			ObjectID:  op.ObjectID,
			OpID:      op.OpID,
			Reason:    "unsupported_op",
			Suggested: "client_upgrade",
			Metadata:  map[string]any{"op_type": op.Type},
		}
	}
}

func validateWorkflowOperation(op core.Op) *core.Conflict {
	switch op.Workflow {
	case "support_ticket", "signup":
		if op.Type != "set_field" {
			return nil
		}
		if len(op.Path) == 0 {
			return &core.Conflict{
				ObjectID:  op.ObjectID,
				OpID:      op.OpID,
				Reason:    "validation_failed",
				Suggested: "edit_and_resubmit",
				Metadata:  map[string]any{"detail": "path is required for validated field update"},
			}
		}

		field := strings.ToLower(op.Path[len(op.Path)-1])
		if field == "email" {
			email, ok := op.Value.(string)
			if !ok || !strings.Contains(email, "@") {
				return &core.Conflict{
					ObjectID:  op.ObjectID,
					OpID:      op.OpID,
					Reason:    "validation_failed",
					Suggested: "edit_and_resubmit",
					Metadata:  map[string]any{"field": "email", "detail": "invalid email format"},
				}
			}
		}

		if field == "message" || field == "bio" {
			msg, ok := op.Value.(string)
			if !ok || len(strings.TrimSpace(msg)) < 5 {
				return &core.Conflict{
					ObjectID:  op.ObjectID,
					OpID:      op.OpID,
					Reason:    "validation_failed",
					Suggested: "edit_and_resubmit",
					Metadata:  map[string]any{"field": field, "detail": "value is too short"},
				}
			}
		}
	}

	return nil
}

func invalidValueConflict(op core.Op) *core.Conflict {
	return &core.Conflict{
		ObjectID:  op.ObjectID,
		OpID:      op.OpID,
		Reason:    "invalid_value",
		Suggested: "drop",
	}
}

func (e *Engine) ensureInventory(op core.Op, sku string, qty int64) *core.Conflict {
	available := e.inventory[sku]
	if qty <= available {
		return nil
	}
	return &core.Conflict{
		ObjectID:  op.ObjectID,
		OpID:      op.OpID,
		Reason:    "inventory_zero",
		Suggested: "partial_fulfill",
		Metadata: map[string]any{
			"sku":              sku,
			"requested_qty":    qty,
			"available_qty":    available,
			"alternate_sku":    "sku-3",
			"alternate_reason": "fallback_item_available",
		},
	}
}

func setNestedField(root map[string]any, path []string, value any) {
	cur := root
	for i := 0; i < len(path)-1; i++ {
		key := path[i]
		next, ok := cur[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[key] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = value
}

func decodeItem(v any) (string, int64, bool) {
	mv, ok := v.(map[string]any)
	if !ok {
		return "", 0, false
	}
	skuVal, ok := mv["sku"].(string)
	if !ok || strings.TrimSpace(skuVal) == "" {
		return "", 0, false
	}
	qtyRaw, ok := mv["qty"]
	if !ok {
		return "", 0, false
	}
	qty, ok := asInt64(qtyRaw)
	if !ok || qty < 0 {
		return "", 0, false
	}
	return skuVal, qty, true
}

func asInt64(v any) (int64, bool) {
	switch tv := v.(type) {
	case int:
		return int64(tv), true
	case int64:
		return tv, true
	case float64:
		return int64(tv), float64(int64(tv)) == tv
	default:
		return 0, false
	}
}

func ensureItemsMap(data map[string]any) map[string]int64 {
	raw, ok := data["items"]
	if !ok {
		m := map[string]int64{}
		data["items"] = m
		return m
	}
	if typed, ok := raw.(map[string]int64); ok {
		return typed
	}
	if anyMap, ok := raw.(map[string]any); ok {
		converted := map[string]int64{}
		for k, v := range anyMap {
			qty, ok := asInt64(v)
			if ok {
				converted[k] = qty
			}
		}
		data["items"] = converted
		return converted
	}
	panic(fmt.Sprintf("items field has unsupported type %T", raw))
}
