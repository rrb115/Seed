package store

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
)

type objectProjection struct {
	State   map[string]any
	LastSeq int64
}

// MemoryStore is an in-memory reference adapter.
type MemoryStore struct {
	mu          sync.RWMutex
	opSeen      map[string]struct{}
	events      []core.Event
	byObject    map[string][]int
	projections map[string]objectProjection
	jobs        map[uuid.UUID]core.JobStatus
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		opSeen:      map[string]struct{}{},
		events:      make([]core.Event, 0),
		byObject:    map[string][]int{},
		projections: map[string]objectProjection{},
		jobs:        map[uuid.UUID]core.JobStatus{},
	}
}

func (s *MemoryStore) AppendEventsTx(_ context.Context, txID uuid.UUID, ops []core.Operation) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, op := range ops {
		if _, exists := s.opSeen[op.OpID]; exists {
			return nil, ErrOpSeen
		}
	}

	now := time.Now().UTC()
	eventIDs := make([]uuid.UUID, 0, len(ops))
	pending := make([]core.Event, 0, len(ops))
	projectionCopy := cloneProjectionMap(s.projections)
	byObjectCopy := cloneByObject(s.byObject)
	eventsCopy := append([]core.Event{}, s.events...)

	for _, op := range ops {
		payload, err := core.CanonicalOperationPayload(op)
		if err != nil {
			return nil, err
		}

		eventID := uuid.New()
		event := core.Event{
			EventID:     eventID.String(),
			TxID:        txID.String(),
			OpID:        op.OpID,
			ObjectID:    op.ObjectID,
			Sequence:    op.Sequence,
			Payload:     payload,
			CreatedAt:   now,
			AppliedType: op.Type,
		}
		pending = append(pending, event)
		eventIDs = append(eventIDs, eventID)

		proj := projectionCopy[op.ObjectID]
		if proj.State == nil {
			proj = objectProjection{State: map[string]any{}}
		}
		if err := core.ApplyOperationState(proj.State, op); err != nil {
			return nil, err
		}
		proj.LastSeq = op.Sequence
		projectionCopy[op.ObjectID] = proj

		idx := len(eventsCopy)
		eventsCopy = append(eventsCopy, event)
		byObjectCopy[op.ObjectID] = append(byObjectCopy[op.ObjectID], idx)
	}

	for _, op := range ops {
		s.opSeen[op.OpID] = struct{}{}
	}
	s.events = eventsCopy
	s.byObject = byObjectCopy
	s.projections = projectionCopy

	return eventIDs, nil
}

func (s *MemoryStore) GetObjectState(_ context.Context, objectID string) (json.RawMessage, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	proj, ok := s.projections[objectID]
	if !ok {
		return json.RawMessage("{}"), 0, nil
	}
	b, err := json.Marshal(proj.State)
	if err != nil {
		return nil, 0, err
	}
	return b, proj.LastSeq, nil
}

func (s *MemoryStore) MarkOpSeen(_ context.Context, opID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.opSeen[opID]; exists {
		return ErrOpSeen
	}
	s.opSeen[opID] = struct{}{}
	return nil
}

func (s *MemoryStore) EnqueueJob(_ context.Context, job core.Job) (uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New()
	s.jobs[id] = core.JobStatus{
		JobID:     id.String(),
		Status:    "queued",
		UpdatedAt: time.Now().UTC(),
	}
	_ = job
	return id, nil
}

func (s *MemoryStore) SetJobStatus(_ context.Context, jobID uuid.UUID, status core.JobStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[jobID]; !ok {
		return ErrJobNotFound
	}
	status.JobID = jobID.String()
	status.UpdatedAt = time.Now().UTC()
	s.jobs[jobID] = status
	return nil
}

func (s *MemoryStore) GetJobStatus(_ context.Context, jobID uuid.UUID) (core.JobStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status, ok := s.jobs[jobID]
	if !ok {
		return core.JobStatus{}, ErrJobNotFound
	}
	if status.Response != nil {
		copyResp := *status.Response
		status.Response = &copyResp
	}
	return status, nil
}

func (s *MemoryStore) ListEvents(_ context.Context, objectID string, since time.Time) ([]core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	indexes := s.byObject[objectID]
	out := make([]core.Event, 0, len(indexes))
	for _, idx := range indexes {
		evt := s.events[idx]
		if !since.IsZero() && evt.CreatedAt.Before(since) {
			continue
		}
		out = append(out, evt)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Sequence < out[j].Sequence
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MemoryStore) ReplayEvents(_ context.Context, objectID string, from time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	indexes := s.byObject[objectID]
	state := map[string]any{}
	lastSeq := int64(0)

	for _, idx := range indexes {
		evt := s.events[idx]
		if !from.IsZero() && evt.CreatedAt.Before(from) {
			continue
		}

		op := core.Operation{
			OpID:     evt.OpID,
			ObjectID: evt.ObjectID,
			Sequence: evt.Sequence,
			Payload:  evt.Payload,
		}
		if err := core.ApplyOperationState(state, op); err != nil {
			return err
		}
		lastSeq = evt.Sequence
	}

	s.projections[objectID] = objectProjection{State: state, LastSeq: lastSeq}
	return nil
}

func cloneProjectionMap(src map[string]objectProjection) map[string]objectProjection {
	out := make(map[string]objectProjection, len(src))
	for k, v := range src {
		out[k] = objectProjection{State: deepCopyMap(v.State), LastSeq: v.LastSeq}
	}
	return out
}

func cloneByObject(src map[string][]int) map[string][]int {
	out := make(map[string][]int, len(src))
	for k, v := range src {
		cp := make([]int, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func deepCopyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = deepCopyAny(v)
	}
	return out
}

func deepCopyAny(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		return deepCopyMap(tv)
	case []any:
		cp := make([]any, len(tv))
		for i := range tv {
			cp[i] = deepCopyAny(tv[i])
		}
		return cp
	default:
		return tv
	}
}
