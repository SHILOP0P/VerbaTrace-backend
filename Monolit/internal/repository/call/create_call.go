package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	repoModel "calllens/monolit/internal/repository/models"
	"calllens/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

type hintExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertSpeakerHints(ctx context.Context, executor hintExecutor, callID uuid.UUID, hints []model.SpeakerHint) error {
	for position, hint := range hints {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO call_speaker_hints(call_uuid,user_uuid,participant_name,username,role,note,position)
			VALUES($1,$2,$3,$4,$5,$6,$7)
		`, callID, hint.UserID, hint.Name, hint.Username, hint.Role, hint.Note, position); err != nil {
			return fmt.Errorf("insert speaker hint: %w", err)
		}
	}
	return nil
}

func insertDiarizationRoles(ctx context.Context, executor hintExecutor, callID uuid.UUID, roles []model.DiarizationRole) error {
	for position, role := range roles {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO call_diarization_roles(call_uuid,name,description,position)
			VALUES($1,$2,$3,$4)
		`, callID, role.Name, role.Description, position); err != nil {
			return fmt.Errorf("insert diarization role: %w", err)
		}
	}
	return nil
}

func (r *Repository) CreateCall(ctx context.Context, call model.Call) (model.Call, error) {
	repoCall, err := converter.ModelCallToRepoCall(call)
	if err != nil {
		return model.Call{}, model.ErrCallConvert
	}
	var repoCallNew repoModel.Call

	create := `
	INSERT INTO calls (
		call_uuid,
		title,
		status,
		audio_path,
		asr_cache_path,
		original_filename,
		mime_type,
		size_bytes,
		duration_seconds,
		uploaded_by_user_uuid,
		company_uuid,
		department_uuid,
		visibility_scope,
		skip_custom_instructions,
		created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	RETURNING call_uuid,
	          title,
	          status,
	          audio_path,
	          asr_cache_path,
	          original_filename,
	          mime_type,
	          size_bytes,
	          duration_seconds,
	          uploaded_by_user_uuid,
	          company_uuid,
	          department_uuid,
	          visibility_scope,
	          skip_custom_instructions,
	          created_at
	`

	row := r.db.QueryRowContext(ctx, create,
		repoCall.ID,
		repoCall.Title,
		repoCall.Status,
		repoCall.AudioPath,
		repoCall.ASRCachePath,
		repoCall.OriginalFilename,
		repoCall.MimeType,
		repoCall.SizeBytes,
		repoCall.DurationSeconds,
		repoCall.UploadedByUserUUID,
		repoCall.CompanyUUID,
		repoCall.DepartmentUUID,
		repoCall.VisibilityScope,
		repoCall.SkipCustomInstructions,
		repoCall.CreatedAt,
	)

	repoCallNew, err = scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Call{}, fmt.Errorf("creating call failed: %w", model.ErrCallNotFound)
		}
		return model.Call{}, fmt.Errorf("creating call failed: %w", err)
	}
	if err := insertSpeakerHints(ctx, r.db, call.ID, call.SpeakerHints); err != nil {
		return model.Call{}, err
	}
	if err := insertDiarizationRoles(ctx, r.db, call.ID, call.DiarizationRoles); err != nil {
		return model.Call{}, err
	}

	return converter.RepoCallToModel(repoCallNew)
}

func (r *Repository) CreateCallWithProcessingJob(ctx context.Context, call model.Call, job model.ProcessingJob) (model.Call, error) {
	repoCall, err := converter.ModelCallToRepoCall(call)
	if err != nil {
		return model.Call{}, model.ErrCallConvert
	}

	repoJob, err := converter.ModelProcessingJobToRepoModel(job)
	if err != nil {
		return model.Call{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Call{}, fmt.Errorf("begin create call with processing job transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	createCall := `
	INSERT INTO calls (
		call_uuid,
		title,
		status,
		audio_path,
		asr_cache_path,
		original_filename,
		mime_type,
		size_bytes,
		duration_seconds,
		uploaded_by_user_uuid,
		company_uuid,
		department_uuid,
		visibility_scope,
		skip_custom_instructions,
		created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	RETURNING call_uuid,
	          title,
	          status,
	          audio_path,
	          asr_cache_path,
	          original_filename,
	          mime_type,
	          size_bytes,
	          duration_seconds,
	          uploaded_by_user_uuid,
	          company_uuid,
	          department_uuid,
	          visibility_scope,
	          skip_custom_instructions,
	          created_at
	`

	row := tx.QueryRowContext(ctx, createCall,
		repoCall.ID,
		repoCall.Title,
		repoCall.Status,
		repoCall.AudioPath,
		repoCall.ASRCachePath,
		repoCall.OriginalFilename,
		repoCall.MimeType,
		repoCall.SizeBytes,
		repoCall.DurationSeconds,
		repoCall.UploadedByUserUUID,
		repoCall.CompanyUUID,
		repoCall.DepartmentUUID,
		repoCall.VisibilityScope,
		repoCall.SkipCustomInstructions,
		repoCall.CreatedAt,
	)

	createdRepoCall, err := scaner.ScanCall(row)
	if err != nil {
		return model.Call{}, fmt.Errorf("create call with processing job: create call: %w", err)
	}

	createJob := `
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
	`

	_, err = tx.ExecContext(ctx, createJob,
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
	if err != nil {
		return model.Call{}, fmt.Errorf("create call with processing job: create job: %w", err)
	}
	if call.FolderUUID.Valid {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO call_folder_assignments(folder_uuid,call_uuid,assigned_by_user_uuid)
			VALUES($1,$2,$3)
		`, call.FolderUUID.UUID, call.ID, call.UploadedByUserUUID.UUID); err != nil {
			return model.Call{}, fmt.Errorf("create call with processing job: assign folder: %w", err)
		}
	}
	if err = insertSpeakerHints(ctx, tx, call.ID, call.SpeakerHints); err != nil {
		return model.Call{}, fmt.Errorf("create call with processing job: %w", err)
	}
	if err = insertDiarizationRoles(ctx, tx, call.ID, call.DiarizationRoles); err != nil {
		return model.Call{}, fmt.Errorf("create call with processing job: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return model.Call{}, fmt.Errorf("commit create call with processing job transaction: %w", err)
	}

	return converter.RepoCallToModel(createdRepoCall)
}
