package call

import (
	"context"
	"time"

	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) CreateCall(ctx context.Context, input models.CreateCallInput) (models.Call, error) {
	if err := validateMediaInput(input); err != nil {
		s.log.Warn(ctx, "create call failed", zap.String("reason", "invalid_media_input"), zap.String("user_id", input.UploadedByUserUUID.String()), zap.Error(err))
		return models.Call{}, err
	}

	if err := s.authorizeUpload(ctx, input); err != nil {
		s.log.Warn(ctx, "create call failed", zap.String("reason", "upload_forbidden"), zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("visibility_scope", string(input.VisibilityScope)), zap.Error(err))
		return models.Call{}, err
	}
	// A call whose budget has run out is accepted and waits, but not without
	// end: refusing here is the only moment the person can still do something
	// about it, and it keeps unprocessable files off the disk.
	if err := s.ensureCreditQueueHasRoom(ctx, input); err != nil {
		s.log.Warn(ctx, "create call failed", zap.String("reason", "pending_credit_queue_full"), zap.String("user_id", input.UploadedByUserUUID.String()), zap.Error(err))
		return models.Call{}, err
	}

	if input.FolderUUID.Valid && !input.IntegrationPrincipalUUID.Valid {
		if s.callFolderRepository == nil {
			return models.Call{}, models.ErrCallFolderNotFound
		}
		folder, folderErr := s.callFolderRepository.GetVisibleByUUID(ctx, input.FolderUUID.UUID, input.UploadedByUserUUID)
		if folderErr != nil {
			return models.Call{}, folderErr
		}
		if !folderMatchesPlacement(folder, input) {
			return models.Call{}, models.ErrCallFolderScopeMismatch
		}
	}

	callUUID, err := uuid.NewV7()
	if err != nil {
		s.log.Error(ctx, "failed to generate call uuid", zap.String("user_id", input.UploadedByUserUUID.String()), zap.Error(err))
		return models.Call{}, err
	}

	savedFile, err := s.audioStorage.Save(ctx, models.SaveInput{
		CallID:           callUUID,
		OriginalFilename: input.OriginalFilename,
		Content:          input.Content,
		SizeBytes:        input.SizeBytes,
		MimeType:         input.MimeType,
	})

	if err != nil {
		s.log.Error(ctx, "failed to save audio file", zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("call_id", callUUID.String()), zap.Error(err))
		return models.Call{}, err
	}

	durationSeconds, err := s.detectAudioDuration(ctx, savedFile.Path)
	if err != nil {
		_ = s.audioStorage.Delete(context.Background(), savedFile.Path)
		s.log.Error(ctx, "failed to detect audio duration", zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("call_id", callUUID.String()), zap.String("audio_path", savedFile.Path), zap.Error(err))
		return models.Call{}, err
	}

	now := time.Now().UTC()
	call, err := converter.SavedFileToModel(savedFile, callUUID, input, now)
	if err != nil {
		_ = s.audioStorage.Delete(context.Background(), savedFile.Path)
		s.log.Error(ctx, "failed to build call model", zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("call_id", callUUID.String()), zap.Error(err))
		return models.Call{}, err
	}
	call.DurationSeconds = durationSeconds
	call.FolderUUID = input.FolderUUID

	transcriptionMode, err := s.resolveTranscriptionMode(ctx, input)
	if err != nil {
		_ = s.audioStorage.Delete(context.Background(), savedFile.Path)
		return models.Call{}, err
	}

	createdCall, err := s.createCallRecord(ctx, call, now, transcriptionMode)
	if err != nil {
		_ = s.audioStorage.Delete(context.Background(), savedFile.Path)
		s.log.Error(ctx, "failed to create call record", zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("call_id", callUUID.String()), zap.Error(err))
		return models.Call{}, err
	}

	s.log.Info(
		ctx,
		"call created",
		zap.String("user_id", input.UploadedByUserUUID.String()),
		zap.String("call_id", createdCall.ID.String()),
		zap.String("mime_type", createdCall.MimeType),
		zap.Int64("size_bytes", createdCall.SizeBytes),
	)

	return createdCall, nil
}

// ensureCreditQueueHasRoom asks the billing side whether one more call may sit
// and wait. It stays silent when billing is not wired in, which is the case in
// unit tests and in the sandbox.
func (s *Service) ensureCreditQueueHasRoom(ctx context.Context, input models.CreateCallInput) error {
	queue, ok := s.billingLimiter.(interface {
		CanQueueCallForCredits(ctx context.Context, userID uuid.UUID, companyID, departmentID uuid.NullUUID) error
	})
	if !ok {
		return nil
	}

	return queue.CanQueueCallForCredits(ctx, input.UploadedByUserUUID, input.CompanyUUID, input.DepartmentUUID)
}

func folderMatchesPlacement(folder models.CallFolder, input models.CreateCallInput) bool {
	switch input.VisibilityScope {
	case models.CallVisibilityScopePersonal:
		return folder.Scope == models.CallFolderScopePersonal && folder.UserUUID.Valid &&
			folder.UserUUID.UUID == input.UploadedByUserUUID
	case models.CallVisibilityScopeCompany:
		return folder.Scope == models.CallFolderScopeCompany && folder.CompanyUUID == input.CompanyUUID
	case models.CallVisibilityScopeDepartment:
		return folder.Scope == models.CallFolderScopeDepartment &&
			folder.CompanyUUID == input.CompanyUUID && folder.DepartmentUUID == input.DepartmentUUID
	default:
		return false
	}
}

func (s *Service) detectAudioDuration(ctx context.Context, path string) (int, error) {
	if s.durationDetector == nil {
		return 0, nil
	}

	durationSeconds, err := s.durationDetector.DetectDuration(ctx, path)
	if err != nil {
		return 0, err
	}

	return durationSeconds, nil
}

func (s *Service) resolveTranscriptionMode(ctx context.Context, input models.CreateCallInput) (models.TranscriptionMode, error) {
	if s.transcriptionModeResolver == nil {
		return models.TranscriptionModeStandard, nil
	}
	return s.transcriptionModeResolver.ResolveTranscriptionMode(ctx, input.UploadedByUserUUID, input.CompanyUUID)
}

func (s *Service) createCallRecord(ctx context.Context, call models.Call, now time.Time, mode models.TranscriptionMode) (models.Call, error) {
	if s.processingJobRepository == nil {
		return s.repository.CreateCall(ctx, call)
	}

	jobID, err := uuid.NewV7()
	if err != nil {
		return models.Call{}, err
	}

	job := models.ProcessingJob{
		ID:                jobID,
		Type:              models.ProcessingJobTypeTranscribeCall,
		TranscriptionMode: mode,
		EntityUUID:        call.ID,
		Status:            models.ProcessingJobStatusPending,
		Attempts:          0,
		MaxAttempts:       s.processingJobMaxAttempts,
		AvailableAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if s.privacyAdmissionResolver != nil {
		state, err := s.privacyAdmissionResolver.ResolveCallPrivacy(ctx, call)
		if err != nil {
			return models.Call{}, err
		}
		if repository, ok := s.repository.(interface {
			CreateCallWithProcessingJobAndPrivacy(context.Context, models.Call, models.ProcessingJob, models.CallPrivacyState) (models.Call, error)
		}); ok {
			return repository.CreateCallWithProcessingJobAndPrivacy(ctx, call, job, state)
		}
	}
	return s.repository.CreateCallWithProcessingJob(ctx, call, job)
}
