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
	if input.FolderUUID.Valid {
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

	if err := s.checkUploadMinutes(ctx, input, durationSeconds); err != nil {
		_ = s.audioStorage.Delete(context.Background(), savedFile.Path)
		s.log.Warn(ctx, "create call failed", zap.String("reason", "billing_limit"), zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("call_id", callUUID.String()), zap.Error(err))
		return models.Call{}, err
	}

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

	if err := s.addUsageMinutes(ctx, input, durationSeconds); err != nil {
		// The call and its durable processing job have already committed. Returning
		// an error here made the client see a false 5xx and retry a successfully
		// accepted large upload. Admission was checked before the record was
		// created, so retain that invariant and surface a reconciliation signal
		// instead of lying about the upload outcome.
		s.log.Error(ctx, "call usage accounting requires reconciliation", zap.String("user_id", input.UploadedByUserUUID.String()), zap.String("call_id", createdCall.ID.String()), zap.Int("duration_seconds", durationSeconds), zap.Error(err))
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

func (s *Service) checkUploadMinutes(ctx context.Context, input models.CreateCallInput, durationSeconds int) error {
	if s.billingLimiter == nil {
		return nil
	}

	if input.CompanyUUID.Valid {
		return s.billingLimiter.CanUploadBusinessCall(ctx, input.CompanyUUID.UUID, durationSeconds)
	}

	return s.billingLimiter.CanUploadPersonalCall(ctx, input.UploadedByUserUUID, durationSeconds)
}

func (s *Service) addUsageMinutes(ctx context.Context, input models.CreateCallInput, durationSeconds int) error {
	if s.billingLimiter == nil {
		return nil
	}

	if input.CompanyUUID.Valid {
		return s.billingLimiter.AddBusinessUsageMinutes(ctx, input.CompanyUUID.UUID, durationSeconds)
	}

	return s.billingLimiter.AddPersonalUsageMinutes(ctx, input.UploadedByUserUUID, durationSeconds)
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
	if len(input.SpeakerHints) == 0 && len(input.DiarizationRoles) == 0 {
		return models.TranscriptionModeStandard, nil
	}
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

	return s.repository.CreateCallWithProcessingJob(ctx, call, job)
}
