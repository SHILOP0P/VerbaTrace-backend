package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ResolveTranscriptionMode(ctx context.Context, userID uuid.UUID, companyID uuid.NullUUID) (models.TranscriptionMode, error) {
	if companyID.Valid {
		subscription, err := s.activeBusinessSubscription(ctx, companyID.UUID)
		if err != nil {
			return "", err
		}
		return transcriptionModeForPlan(subscription.Plan), nil
	}
	subscription, err := s.activePersonalSubscription(ctx, userID)
	if err != nil {
		return "", err
	}
	return transcriptionModeForPlan(subscription.Plan), nil
}

func transcriptionModeForPlan(plan models.Plan) models.TranscriptionMode {
	switch plan.Code {
	case models.PlanCodePersonalPro, models.PlanCodeBusinessPlus, models.PlanCodeBusinessPro:
		return models.TranscriptionModeIdentified
	case models.PlanCodePersonalPlus, models.PlanCodeBusinessStart:
		return models.TranscriptionModeDiarized
	default:
		return models.TranscriptionModeStandard
	}
}
