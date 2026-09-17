package processing_job

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) MarkDone(ctx context.Context, id uuid.UUID) (model.ProcessingJob, error) {
	query := `
	UPDATE processing_jobs
	SET status = $2,
	    locked_at = NULL,
	    locked_by = NULL,
	    last_error = NULL,
	    updated_at = now()
	WHERE job_uuid = $1
	RETURNING ` + processingJobReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.ProcessingJobStatusDone))

	return scanUpdatedJob(row, "mark processing job done")
}

func (r *Repository) MarkRetry(ctx context.Context, id uuid.UUID, lastError string, delay time.Duration) (model.ProcessingJob, error) {
	query := `
	UPDATE processing_jobs
	SET status = CASE
	        WHEN attempts >= max_attempts THEN $2
	        ELSE $3
	    END,
	    available_at = CASE
	        WHEN attempts >= max_attempts THEN available_at
	        ELSE now() + make_interval(secs => $5)
	    END,
	    locked_at = NULL,
	    locked_by = NULL,
	    last_error = $4,
	    updated_at = now()
	WHERE job_uuid = $1
	RETURNING ` + processingJobReturningColumns

	row := r.db.QueryRowContext(
		ctx,
		query,
		id,
		string(model.ProcessingJobStatusFailed),
		string(model.ProcessingJobStatusPending),
		lastError,
		int(delay.Seconds()),
	)

	return scanUpdatedJob(row, "mark processing job retry")
}

// MarkWaitingForCredits puts the job back in the queue without spending an
// attempt. Running out of credits says nothing about the job, so counting it as
// a failed try would eventually declare a perfectly good call broken. TakeNext
// raised the attempt when it claimed the job, so it is given back here.
func (r *Repository) MarkWaitingForCredits(ctx context.Context, id uuid.UUID, reason string, delay time.Duration) (model.ProcessingJob, error) {
	query := `
	UPDATE processing_jobs
	SET status = $2,
	    attempts = GREATEST(attempts - 1, 0),
	    available_at = now() + make_interval(secs => $4),
	    locked_at = NULL,
	    locked_by = NULL,
	    last_error = $3,
	    updated_at = now()
	WHERE job_uuid = $1
	RETURNING ` + processingJobReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.ProcessingJobStatusPending), reason, int(delay.Seconds()))

	return scanUpdatedJob(row, "mark processing job waiting for credits")
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, lastError string) (model.ProcessingJob, error) {
	query := `
	UPDATE processing_jobs
	SET status = $2,
	    locked_at = NULL,
	    locked_by = NULL,
	    last_error = $3,
	    updated_at = now()
	WHERE job_uuid = $1
	RETURNING ` + processingJobReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.ProcessingJobStatusFailed), lastError)

	return scanUpdatedJob(row, "mark processing job failed")
}

func scanUpdatedJob(row interface {
	Scan(dest ...any) error
}, operation string) (model.ProcessingJob, error) {
	repoJob, err := scaner.ScanProcessingJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ProcessingJob{}, model.ErrProcessingJobNotFound
		}
		return model.ProcessingJob{}, fmt.Errorf("%s: %w", operation, err)
	}

	return converter.RepoProcessingJobToModel(repoJob)
}
