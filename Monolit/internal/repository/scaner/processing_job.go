package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanProcessingJob(row rowScanner) (repoModel.ProcessingJob, error) {
	var job repoModel.ProcessingJob

	err := row.Scan(
		&job.ID,
		&job.Type,
		&job.TranscriptionMode,
		&job.EntityUUID,
		&job.Status,
		&job.Attempts,
		&job.MaxAttempts,
		&job.AvailableAt,
		&job.LockedAt,
		&job.LockedBy,
		&job.LastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return repoModel.ProcessingJob{}, err
	}

	return job, nil
}
