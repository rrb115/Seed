package core

import "time"

// ManifestResource describes one cacheable resource required to finish a goal.
type ManifestResource struct {
	URL        string `json:"url"`
	CID        string `json:"cid"`
	Size       int64  `json:"size"`
	Integrity  string `json:"integrity"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

// ManifestPayload is the signed portion of GoalManifest.
type ManifestPayload struct {
	ManifestID         string             `json:"manifest_id"`
	Goal               string             `json:"goal"`
	Objectives         []string           `json:"objectives"`
	Resources          []ManifestResource `json:"resources"`
	SafetyClass        string             `json:"safety_class"`
	OfflineEligible    bool               `json:"offline_eligible"`
	ValidationRequired bool               `json:"validation_required"`
	Version            int                `json:"version"`
	KeyID              string             `json:"key_id"`
	Audience           string             `json:"audience"`
	CreatedAt          time.Time          `json:"created_at"`
	ExpiresAt          time.Time          `json:"expires_at"`
}

// GoalManifest is the wire shape returned to clients.
type GoalManifest struct {
	ManifestPayload
	Signature string `json:"edge_signature"`
}

// Op is a single user intent mutation queued while offline.
type Op struct {
	OpID     string   `json:"op_id"`
	ObjectID string   `json:"object_id"`
	ClientID string   `json:"client_id"`
	Workflow string   `json:"workflow,omitempty"`
	Clock    uint64   `json:"clock"`
	Type     string   `json:"type"`
	Path     []string `json:"path,omitempty"`
	Value    any      `json:"value,omitempty"`
}

// SyncRequest is posted by the client sync agent.
type SyncRequest struct {
	ClientID          string            `json:"client_id"`
	ClientVectorClock map[string]uint64 `json:"client_vector_clock,omitempty"`
	Ops               []Op              `json:"ops"`
}

// Conflict describes server-side invariant failures.
type Conflict struct {
	ObjectID  string         `json:"object_id"`
	OpID      string         `json:"op_id"`
	Reason    string         `json:"reason"`
	Suggested string         `json:"suggested_action"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ObjectResult is per-object sync response output.
type ObjectResult struct {
	ObjectID      string            `json:"object_id"`
	State         map[string]any    `json:"state"`
	VersionVector map[string]uint64 `json:"version_vector"`
}

// SyncResponse is returned from /v1/sync.
type SyncResponse struct {
	ServerTime time.Time      `json:"server_time"`
	AckedOpIDs []string       `json:"acked_op_ids"`
	Results    []ObjectResult `json:"results"`
	Conflicts  []Conflict     `json:"conflicts,omitempty"`
	QueueID    string         `json:"queue_id,omitempty"`
	Status     string         `json:"status,omitempty"`
}

// SyncStatus reports asynchronous queue processing state.
type SyncStatus struct {
	QueueID   string        `json:"queue_id"`
	Status    string        `json:"status"`
	UpdatedAt time.Time     `json:"updated_at"`
	Response  *SyncResponse `json:"response,omitempty"`
	Error     string        `json:"error,omitempty"`
}
