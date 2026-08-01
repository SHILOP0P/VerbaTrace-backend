package converter

import (
	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoProcessingJobToModel(repoJob repoModel.ProcessingJob) (model.ProcessingJob, error) {
	return model.ProcessingJob{
		ID:                repoJob.ID,
		Type:              model.ProcessingJobType(repoJob.Type),
		TranscriptionMode: model.TranscriptionMode(repoJob.TranscriptionMode),
		EntityUUID:        repoJob.EntityUUID,
		Status:            model.ProcessingJobStatus(repoJob.Status),
		Attempts:          repoJob.Attempts,
		MaxAttempts:       repoJob.MaxAttempts,
		AvailableAt:       repoJob.AvailableAt,
		LockedAt:          nullTimeToTimePtr(repoJob.LockedAt),
		LockedBy:          nullStringToStringPtr(repoJob.LockedBy),
		LastError:         nullStringToStringPtr(repoJob.LastError),
		CreatedAt:         repoJob.CreatedAt,
		UpdatedAt:         repoJob.UpdatedAt,
	}, nil
}

func ModelProcessingJobToRepoModel(job model.ProcessingJob) (repoModel.ProcessingJob, error) {
	mode := job.TranscriptionMode
	if mode == "" {
		mode = model.TranscriptionModeStandard
	}
	return repoModel.ProcessingJob{
		ID:                job.ID,
		Type:              string(job.Type),
		TranscriptionMode: string(mode),
		EntityUUID:        job.EntityUUID,
		Status:            string(job.Status),
		Attempts:          job.Attempts,
		MaxAttempts:       job.MaxAttempts,
		AvailableAt:       job.AvailableAt,
		LockedAt:          timePtrToNullTime(job.LockedAt),
		LockedBy:          stringPtrToNullString(job.LockedBy),
		LastError:         stringPtrToNullString(job.LastError),
		CreatedAt:         job.CreatedAt,
		UpdatedAt:         job.UpdatedAt,
	}, nil
}
