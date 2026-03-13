package store

import (
	"sync"
	"time"

	"seed/backend/internal/core"
)

// ObjectState is the canonical per-object state stored server-side.
type ObjectState struct {
	Data          map[string]any
	VersionVector map[string]uint64
}

// MemoryStore is an in-memory storage backend for demo/reference use.
type MemoryStore struct {
	mu      sync.RWMutex
	acked   map[string]struct{}
	objects map[string]ObjectState
	status  map[string]core.SyncStatus
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		acked:   map[string]struct{}{},
		objects: map[string]ObjectState{},
		status:  map[string]core.SyncStatus{},
	}
}

func (s *MemoryStore) IsAcked(opID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.acked[opID]
	return ok
}

func (s *MemoryStore) MarkAcked(opID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked[opID] = struct{}{}
}

func (s *MemoryStore) GetObject(objectID string) ObjectState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.objects[objectID]
	if !ok {
		return ObjectState{Data: map[string]any{}, VersionVector: map[string]uint64{}}
	}
	return cloneObject(obj)
}

func (s *MemoryStore) PutObject(objectID string, object ObjectState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[objectID] = cloneObject(object)
}

func (s *MemoryStore) SetSyncStatus(status core.SyncStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status.UpdatedAt = time.Now().UTC()
	s.status[status.QueueID] = status
}

func (s *MemoryStore) GetSyncStatus(queueID string) (core.SyncStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status, ok := s.status[queueID]
	if !ok {
		return core.SyncStatus{}, false
	}
	if status.Response != nil {
		respCopy := *status.Response
		status.Response = &respCopy
	}
	return status, true
}

func cloneObject(obj ObjectState) ObjectState {
	dataCopy := make(map[string]any, len(obj.Data))
	for k, v := range obj.Data {
		dataCopy[k] = deepCopy(v)
	}
	vvCopy := make(map[string]uint64, len(obj.VersionVector))
	for k, v := range obj.VersionVector {
		vvCopy[k] = v
	}
	return ObjectState{Data: dataCopy, VersionVector: vvCopy}
}

func deepCopy(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		copyMap := make(map[string]any, len(tv))
		for k, val := range tv {
			copyMap[k] = deepCopy(val)
		}
		return copyMap
	case []any:
		copySlice := make([]any, len(tv))
		for i := range tv {
			copySlice[i] = deepCopy(tv[i])
		}
		return copySlice
	default:
		return tv
	}
}
