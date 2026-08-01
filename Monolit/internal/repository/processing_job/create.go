package processing_job

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) Create(ctx context.Context, job model.ProcessingJob) (model.ProcessingJob, error) {
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
	)

	createdJob, err := scaner.ScanProcessingJob(row)
	if err != nil {
		return model.ProcessingJob{}, fmt.Errorf("create processing job: %w", err)
	}

	return converter.RepoProcessingJobToModel(createdJob)
}
