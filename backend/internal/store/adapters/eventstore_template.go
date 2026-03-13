package adapters

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
)

// EventStoreTemplate documents an append-only log adapter shape.
// TODO: replace pseudocode sections with concrete broker/client APIs.
type EventStoreTemplate struct{}

func NewEventStoreTemplate() *EventStoreTemplate {
	return &EventStoreTemplate{}
}

func (e *EventStoreTemplate) AppendEventsTx(ctx context.Context, txID uuid.UUID, ops []core.Operation) ([]uuid.UUID, error) {
	_ = ctx
	_ = txID
	_ = ops
	/*
		for op in ops:
		  emit to stream "events.<object_id>" with key=object_id and value={tx_id, op_id, payload}
		  ensure idempotence key = op_id in stream metadata
	*/
	return nil, nil
}

func (e *EventStoreTemplate) GetObjectState(ctx context.Context, objectID string) (json.RawMessage, int64, error) {
	_ = ctx
	_ = objectID
	/*
		Read projection snapshot from projection store (KV/document DB) keyed by object_id.
	*/
	return nil, 0, nil
}

func (e *EventStoreTemplate) MarkOpSeen(ctx context.Context, opID string) error {
	_ = ctx
	_ = opID
	/*
		Set key op_seen/<op_id> with CAS operation; conflict => ErrOpSeen.
	*/
	return nil
}

func (e *EventStoreTemplate) EnqueueJob(ctx context.Context, job core.Job) (uuid.UUID, error) {
	_ = ctx
	_ = job
	/*
		Write job request to jobs topic; consumer persists status snapshots.
	*/
	return uuid.Nil, nil
}

func (e *EventStoreTemplate) GetJobStatus(ctx context.Context, jobID uuid.UUID) (core.JobStatus, error) {
	_ = ctx
	_ = jobID
	/*
		Read status from job-status projection store.
	*/
	return core.JobStatus{}, nil
}

func (e *EventStoreTemplate) SetJobStatus(ctx context.Context, jobID uuid.UUID, status core.JobStatus) error {
	_ = ctx
	_ = jobID
	_ = status
	/*
		Emit status event to jobs-status stream and update projection.
	*/
	return nil
}

func (e *EventStoreTemplate) ListEvents(ctx context.Context, objectID string, since time.Time) ([]core.Event, error) {
	_ = ctx
	_ = objectID
	_ = since
	/*
		Read stream segment for object_id and filter by created_at.
	*/
	return nil, nil
}

func (e *EventStoreTemplate) ReplayEvents(ctx context.Context, objectID string, from time.Time) error {
	_ = ctx
	_ = objectID
	_ = from
	/*
		Create projection rebuild job:
		  - read object event stream from offset/time
		  - apply projector
		  - replace projection snapshot atomically
	*/
	return nil
}
