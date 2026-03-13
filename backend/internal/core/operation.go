package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// CanonicalOperationPayload returns a normalized payload for persistence.
func CanonicalOperationPayload(op Operation) (json.RawMessage, error) {
	if len(op.Payload) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, op.Payload); err != nil {
			return nil, fmt.Errorf("invalid payload: %w", err)
		}
		return json.RawMessage(compact.Bytes()), nil
	}
	payload := map[string]any{
		"type":  op.Type,
		"path":  op.Path,
		"value": op.Value,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// DecodeOperationPayload returns operation type/path/value from payload or compatibility fields.
func DecodeOperationPayload(op Operation) (string, []string, any, error) {
	if len(op.Payload) == 0 {
		if op.Type == "" {
			return "", nil, nil, errors.New("operation type is required")
		}
		return op.Type, op.Path, op.Value, nil
	}

	var payload struct {
		Type  string `json:"type"`
		Path  []any  `json:"path"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return "", nil, nil, err
	}
	if payload.Type == "" {
		return "", nil, nil, errors.New("payload.type is required")
	}

	path := make([]string, 0, len(payload.Path))
	for _, p := range payload.Path {
		s, ok := p.(string)
		if !ok {
			return "", nil, nil, errors.New("payload.path must contain string values")
		}
		path = append(path, s)
	}

	return payload.Type, path, payload.Value, nil
}

// ApplyOperationState mutates object projection state for supported operation types.
func ApplyOperationState(state map[string]any, op Operation) error {
	typ, path, value, err := DecodeOperationPayload(op)
	if err != nil {
		return err
	}

	switch typ {
	case "set_field":
		if len(path) == 0 {
			return errors.New("set_field requires path")
		}
		setNestedField(state, path, value)
		return nil
	case "add_item":
		sku, qty, ok := decodeItemValue(value)
		if !ok {
			return errors.New("invalid add_item payload")
		}
		items := ensureItemsMap(state)
		items[sku] += qty
		return nil
	case "remove_item":
		sku, ok := value.(string)
		if !ok || sku == "" {
			return errors.New("invalid remove_item payload")
		}
		items := ensureItemsMap(state)
		delete(items, sku)
		return nil
	case "set_quantity":
		sku, qty, ok := decodeItemValue(value)
		if !ok {
			return errors.New("invalid set_quantity payload")
		}
		items := ensureItemsMap(state)
		items[sku] = qty
		return nil
	default:
		return fmt.Errorf("unsupported operation type: %s", typ)
	}
}

func setNestedField(root map[string]any, path []string, value any) {
	cursor := root
	for i := 0; i < len(path)-1; i++ {
		key := path[i]
		next, ok := cursor[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[key] = next
		}
		cursor = next
	}
	cursor[path[len(path)-1]] = value
}

func ensureItemsMap(root map[string]any) map[string]int64 {
	itemsAny, ok := root["items"]
	if !ok {
		items := map[string]int64{}
		root["items"] = items
		return items
	}

	switch tv := itemsAny.(type) {
	case map[string]int64:
		return tv
	case map[string]any:
		items := map[string]int64{}
		for k, v := range tv {
			if qty, ok := int64FromAny(v); ok {
				items[k] = qty
			}
		}
		root["items"] = items
		return items
	default:
		items := map[string]int64{}
		root["items"] = items
		return items
	}
}

func decodeItemValue(value any) (string, int64, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return "", 0, false
	}
	sku, ok := m["sku"].(string)
	if !ok || sku == "" {
		return "", 0, false
	}
	qty, ok := int64FromAny(m["qty"])
	if !ok {
		return "", 0, false
	}
	return sku, qty, true
}

func int64FromAny(v any) (int64, bool) {
	switch tv := v.(type) {
	case int:
		return int64(tv), true
	case int64:
		return tv, true
	case uint64:
		return int64(tv), true
	case float64:
		return int64(tv), true
	case float32:
		return int64(tv), true
	default:
		return 0, false
	}
}
