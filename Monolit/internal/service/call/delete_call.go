package call

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DeleteCall moves the call to the bin instead of erasing it. Audio, transcript
// and analysis survive the grace period so a wrong click can be undone; the
// purge worker removes them once it is over.
func (s *Service) DeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	now := time.Now().UTC()

	// A call that is still being worked on cannot simply vanish: the provider is
	// already holding it and its credits are reserved. Stopping that is a
	// decision of its own, so the person is asked to cancel first.
	current, err := s.repository.GetByUUID(ctx, id, userID)
	if err != nil {
		return err
	}
	if isBeingProcessed(current.Status) {
		s.log.Warn(ctx, "delete call refused", zap.String("reason", "processing_in_progress"), zap.String("user_id", userID.String()), zap.String("call_id", id.String()))
		return models.ErrCallProcessingInProgress
	}

	call, err := s.repository.SoftDeleteCall(ctx, id, userID, now, now.Add(models.CallBinRetention))
	if err != nil {
		s.log.Warn(ctx, "delete call failed", zap.String("reason", "soft_delete_failed"), zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return err
	}

	s.log.Info(ctx, "call moved to bin", zap.String("user_id", userID.String()), zap.String("call_id", call.ID.String()))

	return nil
}

func (s *Service) RestoreCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error) {
	call, err := s.repository.RestoreCall(ctx, id, userID)
	if err != nil {
		s.log.Warn(ctx, "restore call failed", zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return models.Call{}, err
	}

	s.log.Info(ctx, "call restored", zap.String("user_id", userID.String()), zap.String("call_id", call.ID.String()))

	return call, nil
}

// isBeingProcessed is what "the queue still owns this call" means. A call
// waiting for credits counts: it has a job sitting in the queue with its turn
// still to come.
func isBeingProcessed(status models.CallStatus) bool {
	switch status {
	case models.CallStatusNew, models.CallStatusProcessing, models.CallStatusAwaitingCredits:
		return true
	default:
		return false
	}
}

// CancelProcessing stops the work on a call without throwing the call away. The
// queue lets it go, the credit reservation is released, and the call waits in a
// cancelled state from which it can be started again, switched to transcription
// only, downloaded or binned. Its retention is the ordinary one.
func (s *Service) CancelProcessing(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error) {
	canceller, ok := s.repository.(interface {
		CancelCallProcessing(ctx context.Context, id, userID uuid.UUID) (models.Call, error)
	})
	if !ok {
		return models.Call{}, models.ErrInvalidCallStatusTransition
	}

	call, err := canceller.CancelCallProcessing(ctx, id, userID)
	if err != nil {
		s.log.Warn(ctx, "cancel call processing failed", zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return models.Call{}, err
	}

	if s.creditReleaser != nil {
		if err := s.creditReleaser.ReleaseCallReservations(ctx, id); err != nil {
			// The ledger reconciler picks the reservation up on its own; losing the
			// call because of it would be the worse outcome.
			s.log.Warn(ctx, "failed to release credits of a cancelled call", zap.String("call_id", id.String()), zap.Error(err))
		}
	}

	s.log.Info(ctx, "call processing cancelled", zap.String("user_id", userID.String()), zap.String("call_id", call.ID.String()))

	return call, nil
}

func (s *Service) ListDeletedCalls(ctx context.Context, input models.ListDeletedCallsInput) (models.ListDeletedCallsResult, error) {
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 20
	}
	if input.Offset < 0 {
		input.Offset = 0
	}

	return s.repository.ListDeletedCalls(ctx, input)
}
