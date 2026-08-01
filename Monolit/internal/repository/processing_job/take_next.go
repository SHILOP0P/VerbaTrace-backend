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
)

func (r *Repository) TakeNext(ctx context.Context, workerID string, staleAfter time.Duration) (model.ProcessingJob, error) {
	query := `
	UPDATE processing_jobs
	SET status = $1,
	    attempts = attempts + 1,
	    locked_at = now(),
	    locked_by = $2,
	    updated_at = now()
	WHERE job_uuid = (
		SELECT job_uuid
		FROM processing_jobs
		WHERE (
			status = $3
			AND available_at <= now()
		)
		OR (
			status = $1
			AND locked_at < now() - make_interval(secs => $4)
			AND attempts < max_attempts
		)
		ORDER BY available_at, created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	)
	RETURNING ` + processingJobReturningColumns

	row := r.db.QueryRowContext(
		ctx,
		query,
		string(model.ProcessingJobStatusRunning),
		workerID,
		string(model.ProcessingJobStatusPending),
		int(staleAfter.Seconds()),
	)

	repoJob, err := scaner.ScanProcessingJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ProcessingJob{}, model.ErrNoProcessingJobs
		}
		return model.ProcessingJob{}, fmt.Errorf("take next processing job: %w", err)
	}

	return converter.RepoProcessingJobToModel(repoJob)
}
