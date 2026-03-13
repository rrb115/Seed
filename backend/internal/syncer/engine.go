package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

// PrepareTokenValidator validates workflow prepare tokens during sync.
type PrepareTokenValidator interface {
	ValidatePrepareToken(ctx context.Context, workflowID string, token string) error
}

// Engine applies operations with idempotence and conflict handling.
type Engine struct {
	store              store.Store
	inventory          map[string]int64
	conflicts          *Registry
	objectClassHandler map[string]string
	requirePrepare     map[string]bool
	prepareValidator   PrepareTokenValidator
}

type stateEntry struct {
	stateRaw json.RawMessage
	stateMap map[string]any
	lastSeq  int64
}

func NewEngine(st store.Store) *Engine {
	return &Engine{
		store: st,
		inventory: map[string]int64{
			"sku-1": 5,
			"sku-2": 0,
			"sku-3": 100,
		},
		conflicts: NewRegistry(),
		objectClassHandler: map[string]string{
			"note":   "lww",
			"ticket": "reject",
			"cart":   "transform",
		},
		requirePrepare: map[string]bool{},
	}
}

func (e *Engine) SetPrepareValidator(v PrepareTokenValidator) {
	e.prepareValidator = v
}

func (e *Engine) SetPrepareRequirements(workflows map[string]bool) {
	cp := make(map[string]bool, len(workflows))
	for k, v := range workflows {
		cp[k] = v
	}
	e.requirePrepare = cp
}

func (e *Engine) SetObjectClassHandlers(handlers map[string]string) {
	cp := make(map[string]string, len(handlers))
	for k, v := range handlers {
		cp[k] = v
	}
	e.objectClassHandler = cp
}

func (e *Engine) Apply(ctx context.Context, req core.SyncRequest, txID uuid.UUID) core.SyncResponse {
	if txID == uuid.Nil {
		txID = uuid.New()
	}

	resp := core.SyncResponse{
		TxID:       txID.String(),
		ServerTime: time.Now().UTC(),
		Status:     "completed",
	}
	if len(req.Ops) == 0 {
		resp.Conflicts = []core.Conflict{{Reason: "ops_empty", Handler: "reject"}}
		return resp
	}

	states := map[string]*stateEntry{}
	touched := map[string]struct{}{}
	acked := make([]string, 0, len(req.Ops))
	toApply := make([]core.Operation, 0, len(req.Ops))
	conflicts := make([]core.Conflict, 0)

	for _, rawOp := range req.Ops {
		op := normalizeOperation(rawOp)
		if err := validateBaseOperation(op); err != nil {
			conflicts = append(conflicts, core.Conflict{
				ObjectID: op.ObjectID,
				OpID:     op.OpID,
				Reason:   "invalid_op",
				Handler:  "reject",
				SuggestedFix: map[string]any{
					"error": err.Error(),
				},
			})
			continue
		}

		dup, err := e.isDuplicate(ctx, op)
		if err != nil {
			conflicts = append(conflicts, core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "store_error", Handler: "reject", SuggestedFix: map[string]any{"error": err.Error()}})
			continue
		}
		if dup {
			acked = append(acked, op.OpID)
			touched[op.ObjectID] = struct{}{}
			continue
		}

		entry, err := e.loadState(ctx, states, op.ObjectID)
		if err != nil {
			conflicts = append(conflicts, core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "store_error", Handler: "reject", SuggestedFix: map[string]any{"error": err.Error()}})
			continue
		}

		handler := e.resolveHandler(op.ObjectID)

		if op.Sequence <= entry.lastSeq {
			decision, details := handler.HandleConflict(ctx, op, entry.stateRaw)
			if !decision.Apply {
				conflicts = append(conflicts, core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "out_of_order_sequence", Handler: decision.Name, SuggestedFix: details})
				continue
			}
			op.Sequence = entry.lastSeq + 1
		}

		if e.requirePrepare[op.Workflow] {
			if err := e.validatePrepare(ctx, op); err != nil {
				decision, details := handler.HandleConflict(ctx, op, entry.stateRaw)
				details["error"] = err.Error()
				conflicts = append(conflicts, core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "prepare_token_invalid", Handler: decision.Name, SuggestedFix: details})
				continue
			}
		}

		if conflict := validateWorkflowOperation(op); conflict != nil {
			decision, details := handler.HandleConflict(ctx, op, entry.stateRaw)
			if conflict.SuggestedFix == nil {
				conflict.SuggestedFix = map[string]any{}
			}
			for k, v := range details {
				conflict.SuggestedFix[k] = v
			}
			conflict.Handler = decision.Name
			conflicts = append(conflicts, *conflict)
			continue
		}

		if conflict := e.ensureInventory(op); conflict != nil {
			decision, details := handler.HandleConflict(ctx, op, entry.stateRaw)
			if conflict.SuggestedFix == nil {
				conflict.SuggestedFix = map[string]any{}
			}
			for k, v := range details {
				conflict.SuggestedFix[k] = v
			}
			conflict.Handler = decision.Name
			conflicts = append(conflicts, *conflict)
			continue
		}

		if err := core.ApplyOperationState(entry.stateMap, op); err != nil {
			conflicts = append(conflicts, core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "projection_failed", Handler: "reject", SuggestedFix: map[string]any{"error": err.Error()}})
			continue
		}
		entry.lastSeq = op.Sequence
		entry.stateMap["_meta"] = map[string]any{"updated_at": time.Now().UTC().Format(time.RFC3339Nano)}
		entry.stateRaw, _ = json.Marshal(entry.stateMap)

		toApply = append(toApply, op)
		touched[op.ObjectID] = struct{}{}
	}

	if len(conflicts) > 0 {
		resp.Conflicts = conflicts
		resp.AckedOpIDs = sortedUnique(acked)
		resp.Results = e.resultsForObjects(ctx, touched)
		return resp
	}

	if len(toApply) == 0 {
		resp.AckedOpIDs = sortedUnique(acked)
		resp.Results = e.resultsForObjects(ctx, touched)
		return resp
	}

	eventIDs, err := e.store.AppendEventsTx(ctx, txID, toApply)
	if err != nil {
		if errors.Is(err, store.ErrOpSeen) {
			for _, op := range toApply {
				acked = append(acked, op.OpID)
			}
			resp.AckedOpIDs = sortedUnique(acked)
			resp.Results = e.resultsForObjects(ctx, touched)
			return resp
		}
		resp.Conflicts = []core.Conflict{{Reason: "append_failed", Handler: "reject", SuggestedFix: map[string]any{"error": err.Error()}}}
		return resp
	}

	applied := make([]string, 0, len(eventIDs))
	for _, id := range eventIDs {
		applied = append(applied, id.String())
	}
	for _, op := range toApply {
		acked = append(acked, op.OpID)
	}

	resp.AppliedEvents = applied
	resp.AckedOpIDs = sortedUnique(acked)
	resp.Results = e.resultsForObjects(ctx, touched)
	return resp
}

func normalizeOperation(op core.Operation) core.Operation {
	if op.Sequence == 0 && op.Sequence <= 0 {
		op.Sequence = int64(op.Clock)
	}
	if op.Timestamp.IsZero() {
		op.Timestamp = time.Now().UTC()
	}
	if op.Payload == nil || len(op.Payload) == 0 {
		payload, err := core.CanonicalOperationPayload(op)
		if err == nil {
			op.Payload = payload
		}
	}
	return op
}

func validateBaseOperation(op core.Operation) error {
	if strings.TrimSpace(op.OpID) == "" {
		return errors.New("op_id is required")
	}
	if strings.TrimSpace(op.ObjectID) == "" {
		return errors.New("object_id is required")
	}
	if op.Sequence <= 0 {
		return errors.New("sequence_number must be > 0")
	}
	_, _, _, err := core.DecodeOperationPayload(op)
	return err
}

func (e *Engine) loadState(ctx context.Context, cache map[string]*stateEntry, objectID string) (*stateEntry, error) {
	if got, ok := cache[objectID]; ok {
		return got, nil
	}
	stateRaw, lastSeq, err := e.store.GetObjectState(ctx, objectID)
	if err != nil {
		return nil, err
	}
	if len(stateRaw) == 0 {
		stateRaw = json.RawMessage("{}")
	}
	stateMap := map[string]any{}
	if err := json.Unmarshal(stateRaw, &stateMap); err != nil {
		stateMap = map[string]any{}
	}
	entry := &stateEntry{stateRaw: stateRaw, stateMap: stateMap, lastSeq: lastSeq}
	cache[objectID] = entry
	return entry, nil
}

func (e *Engine) validatePrepare(ctx context.Context, op core.Operation) error {
	if strings.TrimSpace(op.PrepareToken) == "" {
		return errors.New("prepare_token missing")
	}
	if e.prepareValidator == nil {
		return errors.New("prepare validator not configured")
	}
	return e.prepareValidator.ValidatePrepareToken(ctx, op.Workflow, op.PrepareToken)
}

func (e *Engine) isDuplicate(ctx context.Context, op core.Operation) (bool, error) {
	events, err := e.store.ListEvents(ctx, op.ObjectID, time.Time{})
	if err != nil {
		return false, err
	}
	for _, evt := range events {
		if evt.OpID == op.OpID {
			return true, nil
		}
	}
	return false, nil
}

func (e *Engine) resolveHandler(objectID string) ConflictHandler {
	class := objectID
	if i := strings.Index(class, ":"); i > 0 {
		class = class[:i]
	}
	name := e.objectClassHandler[class]
	if name == "" {
		name = "reject"
	}
	return e.conflicts.Resolve(name)
}

func validateWorkflowOperation(op core.Operation) *core.Conflict {
	typeName, path, value, err := core.DecodeOperationPayload(op)
	if err != nil {
		return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "invalid_payload", SuggestedFix: map[string]any{"error": err.Error()}}
	}

	switch op.Workflow {
	case "support_ticket", "signup":
		if typeName != "set_field" {
			return nil
		}
		if len(path) == 0 {
			return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "validation_failed", SuggestedFix: map[string]any{"detail": "path is required"}}
		}
		field := strings.ToLower(path[len(path)-1])
		if field == "email" {
			email, ok := value.(string)
			if !ok || !strings.Contains(email, "@") {
				return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "validation_failed", SuggestedFix: map[string]any{"field": "email", "detail": "invalid email format"}}
			}
		}
		if field == "message" || field == "bio" {
			msg, ok := value.(string)
			if !ok || len(strings.TrimSpace(msg)) < 5 {
				return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "validation_failed", SuggestedFix: map[string]any{"field": field, "detail": "value is too short"}}
			}
		}
	}
	return nil
}

func (e *Engine) ensureInventory(op core.Operation) *core.Conflict {
	typ, _, value, err := core.DecodeOperationPayload(op)
	if err != nil {
		return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "invalid_payload", SuggestedFix: map[string]any{"error": err.Error()}}
	}
	if typ != "add_item" && typ != "set_quantity" {
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "invalid_value", SuggestedFix: map[string]any{"detail": "sku/qty expected"}}
	}
	sku, ok := m["sku"].(string)
	if !ok || sku == "" {
		return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "invalid_value", SuggestedFix: map[string]any{"detail": "sku missing"}}
	}
	qty, ok := asInt64(m["qty"])
	if !ok {
		return &core.Conflict{ObjectID: op.ObjectID, OpID: op.OpID, Reason: "invalid_value", SuggestedFix: map[string]any{"detail": "qty missing"}}
	}
	available := e.inventory[sku]
	if qty <= available {
		return nil
	}
	return &core.Conflict{
		ObjectID: op.ObjectID,
		OpID:     op.OpID,
		Reason:   "inventory_zero",
		SuggestedFix: map[string]any{
			"sku":           sku,
			"requested_qty": qty,
			"available_qty": available,
			"alternate_sku": "sku-3",
		},
	}
}

func (e *Engine) resultsForObjects(ctx context.Context, touched map[string]struct{}) []core.ObjectResult {
	results := make([]core.ObjectResult, 0, len(touched))
	for objectID := range touched {
		stateRaw, lastSeq, err := e.store.GetObjectState(ctx, objectID)
		if err != nil {
			continue
		}
		state := map[string]any{}
		_ = json.Unmarshal(stateRaw, &state)
		results = append(results, core.ObjectResult{
			ObjectID:            objectID,
			State:               state,
			LastAppliedSequence: lastSeq,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ObjectID < results[j].ObjectID })
	return results
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func asInt64(v any) (int64, bool) {
	switch tv := v.(type) {
	case int:
		return int64(tv), true
	case int64:
		return tv, true
	case float64:
		return int64(tv), true
	case float32:
		return int64(tv), true
	case uint64:
		return int64(tv), true
	default:
		return 0, false
	}
}
