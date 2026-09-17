package call

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type callRestarter interface {
	RestartCallProcessing(ctx context.Context, id, userID uuid.UUID, transcriptionOnly bool, job models.ProcessingJob) (models.Call, error)
	SwitchCallToTranscriptionOnly(ctx context.Context, id, userID uuid.UUID) (models.Call, error)
}

// RestartProcessing starts a cancelled call over. Cancelling is meant to be a
// pause, not a verdict: the recording stayed, so the work can begin again, and
// the mode may change on the way — dropping the analysis is the usual reason
// somebody stopped the call in the first place.
//
// Switching an already transcribed call to transcription only is free: the
// transcript exists, nothing goes to a provider, and the call is simply done.
func (s *Service) RestartProcessing(ctx context.Context, id, userID uuid.UUID, transcriptionOnly bool) (models.Call, error) {
	restarter, ok := s.repository.(callRestarter)
	if !ok {
		return models.Call{}, models.ErrInvalidCallStatusTransition
	}

	current, err := s.repository.GetByUUID(ctx, id, userID)
	if err != nil {
		return models.Call{}, err
	}
	if current.IsTest {
		return models.Call{}, models.ErrTestCallReadOnly
	}

	if transcriptionOnly && s.hasUsableTranscription(ctx, id) {
		call, switchErr := restarter.SwitchCallToTranscriptionOnly(ctx, id, userID)
		if switchErr != nil {
			s.log.Warn(ctx, "switch call to transcription only failed", zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(switchErr))
			return models.Call{}, switchErr
		}

		s.log.Info(ctx, "call switched to transcription only", zap.String("user_id", userID.String()), zap.String("call_id", call.ID.String()))

		return call, nil
	}

	if current.Status != models.CallStatusCancelled {
		return models.Call{}, models.ErrInvalidCallStatusTransition
	}
	// Restarting puts one more call in front of the credit limit, so it answers
	// to the same cap as a fresh upload: a queue nobody can drain helps nobody.
	if err = s.ensureRestartQueueHasRoom(ctx, current); err != nil {
		s.log.Warn(ctx, "restart call processing refused", zap.String("reason", "pending_credit_queue_full"), zap.String("call_id", id.String()), zap.Error(err))
		return models.Call{}, err
	}

	job, err := s.buildRestartJob(ctx, current)
	if err != nil {
		return models.Call{}, err
	}

	call, err := restarter.RestartCallProcessing(ctx, id, userID, transcriptionOnly, job)
	if err != nil {
		s.log.Warn(ctx, "restart call processing failed", zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return models.Call{}, err
	}

	s.log.Info(ctx, "call processing restarted", zap.String("user_id", userID.String()), zap.String("call_id", call.ID.String()), zap.Bool("transcription_only", transcriptionOnly))

	return call, nil
}

// hasUsableTranscription answers whether the transcript survived whatever
// stopped the call. Without a transcription repository — unit tests, the
// sandbox — the answer is no, and the call goes through the queue as usual.
func (s *Service) hasUsableTranscription(ctx context.Context, id uuid.UUID) bool {
	if s.transcriptionRepository == nil {
		return false
	}

	transcription, err := s.transcriptionRepository.GetByCallUUID(ctx, id)
	if err != nil {
		if !errors.Is(err, models.ErrTranscriptionNotFound) {
			s.log.Warn(ctx, "failed to read transcription of a restarted call", zap.String("call_id", id.String()), zap.Error(err))
		}
		return false
	}

	return transcription.Status == models.TranscriptionStatusTranscribed && transcription.Text != nil
}

func (s *Service) ensureRestartQueueHasRoom(ctx context.Context, call models.Call) error {
	queue, ok := s.billingLimiter.(interface {
		CanQueueCallForCredits(ctx context.Context, userID uuid.UUID, companyID, departmentID uuid.NullUUID) error
	})
	if !ok || !call.UploadedByUserUUID.Valid {
		return nil
	}

	return queue.CanQueueCallForCredits(ctx, call.UploadedByUserUUID.UUID, call.CompanyUUID, call.DepartmentUUID)
}

func (s *Service) buildRestartJob(ctx context.Context, call models.Call) (models.ProcessingJob, error) {
	jobID, err := uuid.NewV7()
	if err != nil {
		return models.ProcessingJob{}, err
	}

	mode := models.TranscriptionModeStandard
	if s.transcriptionModeResolver != nil && call.UploadedByUserUUID.Valid {
		mode, err = s.transcriptionModeResolver.ResolveTranscriptionMode(ctx, call.UploadedByUserUUID.UUID, call.CompanyUUID)
		if err != nil {
			return models.ProcessingJob{}, err
		}
	}

	now := time.Now().UTC()

	return models.ProcessingJob{
		ID:                jobID,
		Type:              models.ProcessingJobTypeTranscribeCall,
		TranscriptionMode: mode,
		EntityUUID:        call.ID,
		Status:            models.ProcessingJobStatusPending,
		MaxAttempts:       s.processingJobMaxAttempts,
		AvailableAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}
