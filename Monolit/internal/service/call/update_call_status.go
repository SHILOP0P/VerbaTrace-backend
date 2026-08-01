package call

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) UpdateCallStatus(ctx context.Context, input models.UpdateCallStatusInput) (models.Call, error) {
	if input.CallUUID == uuid.Nil {
		return models.Call{}, models.ErrCallNotFound
	}

	if !validCallStatus(input.Status) {
		return models.Call{}, models.ErrInvalidCallStatus
	}

	currentCall, err := s.repository.GetByUUIDForProcessing(ctx, input.CallUUID)
	if err != nil {
		return models.Call{}, err
	}

	if !canMoveCallStatus(currentCall.Status, input.Status) {
		s.log.Warn(
			ctx,
			"invalid call status transition",
			zap.String("call_id", input.CallUUID.String()),
			zap.String("from_status", string(currentCall.Status)),
			zap.String("to_status", string(input.Status)),
		)
		return models.Call{}, models.ErrInvalidCallStatusTransition
	}

	updatedCall, err := s.repository.UpdateCallStatus(ctx, input.CallUUID, input.Status)
	if err != nil {
		s.log.Error(ctx, "failed to update call status", zap.String("call_id", input.CallUUID.String()), zap.String("status", string(input.Status)), zap.Error(err))
		return models.Call{}, err
	}

	s.log.Info(
		ctx,
		"call status updated",
		zap.String("call_id", updatedCall.ID.String()),
		zap.String("from_status", string(currentCall.Status)),
		zap.String("to_status", string(updatedCall.Status)),
	)

	return updatedCall, nil
}

func validCallStatus(status models.CallStatus) bool {
	switch status {
	case models.CallStatusNew,
		models.CallStatusProcessing,
		models.CallStatusTranscribed,
		models.CallStatusAnalyzed,
		models.CallStatusFailed:
		return true
	default:
		return false
	}
}

func canMoveCallStatus(from models.CallStatus, to models.CallStatus) bool {
	if from == to {
		return true
	}

	switch from {
	case models.CallStatusNew:
		return to == models.CallStatusProcessing || to == models.CallStatusFailed
	case models.CallStatusProcessing:
		return to == models.CallStatusTranscribed || to == models.CallStatusFailed
	case models.CallStatusTranscribed:
		return to == models.CallStatusAnalyzed || to == models.CallStatusFailed
	default:
		return false
	}
}
