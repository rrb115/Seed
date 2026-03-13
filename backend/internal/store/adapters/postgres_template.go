package adapters

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"seed/backend/internal/core"
)

// PostgresTemplate documents one Store adapter shape.
// TODO: replace placeholders with concrete DB calls in downstream projects.
type PostgresTemplate struct {
	// db *sql.DB
}

func NewPostgresTemplate() *PostgresTemplate {
	return &PostgresTemplate{}
}

func (p *PostgresTemplate) AppendEventsTx(ctx context.Context, txID uuid.UUID, ops []core.Operation) ([]uuid.UUID, error) {
	_ = ctx
	_ = txID
	_ = ops
	/*
		BEGIN;
		INSERT INTO op_seen(op_id, seen_at) VALUES ($1, now()); -- unique(op_id)
		INSERT INTO events(event_id, tx_id, op_id, object_id, sequence_number, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, now());
		UPSERT projection state per object_id from events.
		COMMIT;
	*/
	return nil, nil
}

func (p *PostgresTemplate) GetObjectState(ctx context.Context, objectID string) (json.RawMessage, int64, error) {
	_ = ctx
	_ = objectID
	/*
		SELECT state, last_sequence FROM object_projection WHERE object_id = $1;
	*/
	return nil, 0, nil
}

func (p *PostgresTemplate) MarkOpSeen(ctx context.Context, opID string) error {
	_ = ctx
	_ = opID
	/*
		INSERT INTO op_seen(op_id, seen_at) VALUES ($1, now());
		-- rely on unique constraint and map duplicate key to store.ErrOpSeen
	*/
	return nil
}

func (p *PostgresTemplate) EnqueueJob(ctx context.Context, job core.Job) (uuid.UUID, error) {
	_ = ctx
	_ = job
	/*
		INSERT INTO sync_jobs(job_id, status, payload, created_at) VALUES (...);
	*/
	return uuid.Nil, nil
}

func (p *PostgresTemplate) GetJobStatus(ctx context.Context, jobID uuid.UUID) (core.JobStatus, error) {
	_ = ctx
	_ = jobID
	/*
		SELECT job_id, status, response, error, updated_at FROM sync_jobs WHERE job_id = $1;
	*/
	return core.JobStatus{}, nil
}

func (p *PostgresTemplate) SetJobStatus(ctx context.Context, jobID uuid.UUID, status core.JobStatus) error {
	_ = ctx
	_ = jobID
	_ = status
	/*
		UPDATE sync_jobs SET status=$2, response=$3::jsonb, error=$4, updated_at=now() WHERE job_id=$1;
	*/
	return nil
}

func (p *PostgresTemplate) ListEvents(ctx context.Context, objectID string, since time.Time) ([]core.Event, error) {
	_ = ctx
	_ = objectID
	_ = since
	/*
		SELECT event_id, tx_id, op_id, object_id, sequence_number, payload, created_at
		FROM events WHERE object_id=$1 AND created_at >= $2 ORDER BY created_at, sequence_number;
	*/
	return nil, nil
}

func (p *PostgresTemplate) ReplayEvents(ctx context.Context, objectID string, from time.Time) error {
	_ = ctx
	_ = objectID
	_ = from
	/*
		SELECT payload FROM events WHERE object_id=$1 AND created_at >= $2 ORDER BY created_at;
		Rebuild projection row in object_projection.
	*/
	return nil
}

/*
Migration notes (template):
1. events table (append-only)
   - event_id uuid primary key
   - tx_id uuid not null
   - op_id text not null unique
   - object_id text not null
   - sequence_number bigint not null
   - payload jsonb not null
   - created_at timestamptz not null
2. object_projection table
   - object_id text primary key
   - state jsonb not null
   - last_sequence bigint not null
3. sync_jobs table
   - job_id uuid primary key
   - status text not null
   - response jsonb null
   - error text null
   - updated_at timestamptz not null
*/
