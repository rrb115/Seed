package admin

import (
	"context"
	"time"

	"seed/backend/internal/core"
	"seed/backend/internal/store"
)

// ListEvents returns object events between from and to timestamps.
func ListEvents(ctx context.Context, st store.Store, objectID string, from, to time.Time) ([]core.Event, error) {
	events, err := st.ListEvents(ctx, objectID, from)
	if err != nil {
		return nil, err
	}
	if to.IsZero() {
		return events, nil
	}
	filtered := make([]core.Event, 0, len(events))
	for _, evt := range events {
		if evt.CreatedAt.After(to) {
			continue
		}
		filtered = append(filtered, evt)
	}
	return filtered, nil
}

// ReplayObject rebuilds projection for object from the provided timestamp.
func ReplayObject(ctx context.Context, st store.Store, objectID string, from time.Time) error {
	return st.ReplayEvents(ctx, objectID, from)
}
