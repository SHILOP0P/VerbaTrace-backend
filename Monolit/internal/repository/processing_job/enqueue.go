package processing_job

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) Enqueue(ctx context.Context, job model.ProcessingJob) (model.ProcessingJob, error) {
	repoJob, err := converter.ModelProcessingJobToRepoModel(job)
	if err != nil {
		return model.ProcessingJob{}, err
	}

	query := `
	INSERT INTO processing_jobs (
		job_uuid,
		job_type,
		transcription_mode,
		entity_uuid,
		status,
		attempts,
		max_attempts,
		available_at,
		locked_at,
		locked_by,
		last_error,
		created_at,
		updated_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	ON CONFLICT (job_type, entity_uuid) DO UPDATE
	SET status = CASE
	        WHEN processing_jobs.status = $14 THEN processing_jobs.status
	        ELSE EXCLUDED.status
	    END,
	    attempts = CASE
	        WHEN processing_jobs.status = $14 THEN processing_jobs.attempts
	        ELSE 0
	    END,
	    max_attempts = EXCLUDED.max_attempts,
	    transcription_mode = EXCLUDED.transcription_mode,
	    available_at = CASE
	        WHEN processing_jobs.status = $14 THEN processing_jobs.available_at
	        ELSE EXCLUDED.available_at
	    END,
	    locked_at = CASE
	        WHEN processing_jobs.status = $14 THEN processing_jobs.locked_at
	        ELSE NULL
	    END,
	    locked_by = CASE
	        WHEN processing_jobs.status = $14 THEN processing_jobs.locked_by
	        ELSE NULL
	    END,
	    last_error = CASE
	        WHEN processing_jobs.status = $14 THEN processing_jobs.last_error
	        ELSE NULL
	    END,
	    updated_at = now()
	RETURNING ` + processingJobReturningColumns

	row := r.db.QueryRowContext(ctx, query,
		repoJob.ID,
		repoJob.Type,
		repoJob.TranscriptionMode,
		repoJob.EntityUUID,
		repoJob.Status,
		repoJob.Attempts,
		repoJob.MaxAttempts,
		repoJob.AvailableAt,
		repoJob.LockedAt,
		repoJob.LockedBy,
		repoJob.LastError,
		repoJob.CreatedAt,
		repoJob.UpdatedAt,
		string(model.ProcessingJobStatusRunning),
	)

	enqueuedJob, err := scaner.ScanProcessingJob(row)
	if err != nil {
		return model.ProcessingJob{}, fmt.Errorf("enqueue processing job: %w", err)
	}

	return converter.RepoProcessingJobToModel(enqueuedJob)
}
