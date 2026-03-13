package core

import (
	"encoding/json"
	"time"
)

// ManifestResource describes one cacheable resource required to finish a workflow.
type ManifestResource struct {
	URL        string `json:"url"`
	CID        string `json:"cid"`
	Size       int64  `json:"size"`
	Integrity  string `json:"integrity"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

// ManifestPayload is the signed payload for a manifest.
type ManifestPayload struct {
	ManifestID         string             `json:"manifest_id"`
	Goal               string             `json:"goal"`
	Objectives         []string           `json:"objectives"`
	Resources          []ManifestResource `json:"resources"`
	SafetyClass        string             `json:"safety_class"`
	OfflineEligible    bool               `json:"offline_eligible"`
	ValidationRequired bool               `json:"validation_required"`
	PrepareRequired    bool               `json:"prepare_required,omitempty"`
	PrepareToken       string             `json:"prepare_token,omitempty"`
	Version            int                `json:"version"`
	KeyID              string             `json:"key_id"`
	Audience           string             `json:"audience"`
	CreatedAt          time.Time          `json:"created_at"`
	ExpiresAt          time.Time          `json:"expires_at"`
}

// GoalManifest is returned by /v1/manifest.
type GoalManifest struct {
	ManifestPayload
	ManifestJWS string `json:"manifest_jws"`
	Signature   string `json:"edge_signature,omitempty"`
}

// Operation is a single mutation intent.
type Operation struct {
	OpID          string          `json:"op_id"`
	ObjectID      string          `json:"object_id"`
	ClientID      string          `json:"client_id,omitempty"`
	Workflow      string          `json:"workflow,omitempty"`
	Sequence      int64           `json:"sequence_number"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	PrepareToken  string          `json:"prepare_token,omitempty"`
	Clock         uint64          `json:"clock,omitempty"`
	Timestamp     time.Time       `json:"timestamp,omitempty"`
	Type          string          `json:"type,omitempty"`
	Path          []string        `json:"path,omitempty"`
	Value         any             `json:"value,omitempty"`
	ClientTraceID string          `json:"client_trace_id,omitempty"`
}

// Op is kept as an alias for compatibility with existing code paths.
type Op = Operation

// SyncRequest is the payload for /v1/sync.
type SyncRequest struct {
	ClientTxID        string            `json:"client_tx_id,omitempty"`
	ClientID          string            `json:"client_id"`
	ManifestVersion   int               `json:"manifest_version,omitempty"`
	ClientVectorClock map[string]uint64 `json:"client_vector_clock,omitempty"`
	Ops               []Operation       `json:"ops"`
}

// Conflict describes server-side conflict results.
type Conflict struct {
	ObjectID     string         `json:"object_id"`
	OpID         string         `json:"op_id"`
	Reason       string         `json:"reason"`
	Handler      string         `json:"handler,omitempty"`
	SuggestedFix map[string]any `json:"suggested_fix,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// ObjectResult is per-object output after sync.
type ObjectResult struct {
	ObjectID            string            `json:"object_id"`
	State               map[string]any    `json:"state"`
	VersionVector       map[string]uint64 `json:"version_vector,omitempty"`
	LastAppliedSequence int64             `json:"last_applied_sequence"`
}

// SyncResponse is returned by /v1/sync and /v1/sync/status.
type SyncResponse struct {
	TxID          string         `json:"tx_id,omitempty"`
	ServerTime    time.Time      `json:"server_time"`
	AppliedEvents []string       `json:"applied_events,omitempty"`
	AckedOpIDs    []string       `json:"acked_op_ids,omitempty"`
	Results       []ObjectResult `json:"results,omitempty"`
	Conflicts     []Conflict     `json:"conflicts,omitempty"`
	QueueID       string         `json:"queue_id,omitempty"`
	Status        string         `json:"status,omitempty"`
}

// SyncStatus reports status for async sync queue jobs.
type SyncStatus struct {
	QueueID   string        `json:"queue_id"`
	Status    string        `json:"status"`
	UpdatedAt time.Time     `json:"updated_at"`
	Response  *SyncResponse `json:"response,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// Event is the persisted append-only record produced from one operation.
type Event struct {
	EventID     string          `json:"event_id"`
	TxID        string          `json:"tx_id"`
	OpID        string          `json:"op_id"`
	ObjectID    string          `json:"object_id"`
	Sequence    int64           `json:"sequence_number"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
	AppliedType string          `json:"applied_type,omitempty"`
}

// Job is an async queue item.
type Job struct {
	JobID      string      `json:"job_id,omitempty"`
	Type       string      `json:"type"`
	ClientID   string      `json:"client_id,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	Payload    any         `json:"payload,omitempty"`
	TraceID    string      `json:"trace_id,omitempty"`
	WorkflowID string      `json:"workflow_id,omitempty"`
	Metadata   interface{} `json:"metadata,omitempty"`
}

// JobStatus reports state of an async job.
type JobStatus struct {
	JobID      string        `json:"job_id"`
	Status     string        `json:"status"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Response   *SyncResponse `json:"response,omitempty"`
	Error      string        `json:"error,omitempty"`
	RetryCount int           `json:"retry_count,omitempty"`
}

// PrepareTokenClaims are signed in /v1/prepare responses.
type PrepareTokenClaims struct {
	WorkflowID   string          `json:"workflow_id"`
	IssuedAt     time.Time       `json:"issued_at"`
	ValidFrom    time.Time       `json:"valid_from"`
	ExpiresAt    time.Time       `json:"expires_at"`
	Nonce        string          `json:"nonce"`
	Precondition json.RawMessage `json:"preconditions,omitempty"`
}
