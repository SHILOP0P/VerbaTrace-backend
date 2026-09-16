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

func (s *Service) ListDeletedCalls(ctx context.Context, input models.ListDeletedCallsInput) (models.ListDeletedCallsResult, error) {
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 20
	}
	if input.Offset < 0 {
		input.Offset = 0
	}

	return s.repository.ListDeletedCalls(ctx, input)
}
