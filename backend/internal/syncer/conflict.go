package syncer

import (
	"context"
	"encoding/json"
	"time"

	"seed/backend/internal/core"
)

// Action is the conflict handler action output.
type Action struct {
	Name   string
	Reason string
	Apply  bool
}

// ConflictHandler decides conflict outcomes.
type ConflictHandler interface {
	Name() string
	HandleConflict(ctx context.Context, op core.Operation, state json.RawMessage) (Action, map[string]any)
}

// Registry stores handlers by name.
type Registry struct {
	handlers map[string]ConflictHandler
}

func NewRegistry() *Registry {
	r := &Registry{handlers: map[string]ConflictHandler{}}
	r.Register(&RejectHandler{})
	r.Register(&LWWHandler{})
	r.Register(&TransformHandler{})
	return r
}

func (r *Registry) Register(handler ConflictHandler) {
	r.handlers[handler.Name()] = handler
}

func (r *Registry) Resolve(name string) ConflictHandler {
	if h, ok := r.handlers[name]; ok {
		return h
	}
	return r.handlers["reject"]
}

// RejectHandler always rejects.
type RejectHandler struct{}

func (h *RejectHandler) Name() string { return "reject" }

func (h *RejectHandler) HandleConflict(_ context.Context, _ core.Operation, _ json.RawMessage) (Action, map[string]any) {
	return Action{Name: h.Name(), Reason: "rejected", Apply: false}, map[string]any{}
}

// LWWHandler applies if operation timestamp is newer than state metadata.
type LWWHandler struct{}

func (h *LWWHandler) Name() string { return "lww" }

func (h *LWWHandler) HandleConflict(_ context.Context, op core.Operation, state json.RawMessage) (Action, map[string]any) {
	if op.Timestamp.IsZero() {
		return Action{Name: h.Name(), Reason: "timestamp_missing", Apply: false}, map[string]any{"requires": "timestamp"}
	}

	var parsed map[string]any
	if err := json.Unmarshal(state, &parsed); err != nil {
		return Action{Name: h.Name(), Reason: "state_decode_failed", Apply: false}, map[string]any{"error": err.Error()}
	}

	meta, _ := parsed["_meta"].(map[string]any)
	tsAny, _ := meta["updated_at"].(string)
	if tsAny == "" {
		return Action{Name: h.Name(), Reason: "winner", Apply: true}, map[string]any{"resolution": "state_missing_timestamp"}
	}
	prevTS, err := time.Parse(time.RFC3339Nano, tsAny)
	if err != nil {
		return Action{Name: h.Name(), Reason: "state_timestamp_invalid", Apply: true}, map[string]any{"resolution": "state_timestamp_invalid"}
	}
	if op.Timestamp.After(prevTS) {
		return Action{Name: h.Name(), Reason: "winner", Apply: true}, map[string]any{"previous": prevTS.Format(time.RFC3339Nano)}
	}
	return Action{Name: h.Name(), Reason: "older_than_current", Apply: false}, map[string]any{"previous": prevTS.Format(time.RFC3339Nano)}
}

// TransformHandler applies transform hints for correction flows.
type TransformHandler struct{}

func (h *TransformHandler) Name() string { return "transform" }

func (h *TransformHandler) HandleConflict(_ context.Context, _ core.Operation, _ json.RawMessage) (Action, map[string]any) {
	return Action{Name: h.Name(), Reason: "transform_required", Apply: false}, map[string]any{"suggested_fix": "edit_and_resubmit"}
}
