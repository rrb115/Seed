package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
)

var (
	// ErrOpSeen is returned when operation id was already recorded.
	ErrOpSeen = errors.New("operation already seen")
	// ErrJobNotFound is returned when a queue job does not exist.
	ErrJobNotFound = errors.New("job not found")
)

// Store defines the backend persistence contract used by API, engine, and admin tooling.
type Store interface {
	AppendEventsTx(ctx context.Context, txID uuid.UUID, ops []core.Operation) ([]uuid.UUID, error)
	GetObjectState(ctx context.Context, objectID string) (state json.RawMessage, lastSeq int64, err error)
	MarkOpSeen(ctx context.Context, opID string) error
	EnqueueJob(ctx context.Context, job core.Job) (jobID uuid.UUID, err error)
	GetJobStatus(ctx context.Context, jobID uuid.UUID) (core.JobStatus, error)
	ListEvents(ctx context.Context, objectID string, since time.Time) ([]core.Event, error)
	ReplayEvents(ctx context.Context, objectID string, from time.Time) error
	SetJobStatus(ctx context.Context, jobID uuid.UUID, status core.JobStatus) error
}
