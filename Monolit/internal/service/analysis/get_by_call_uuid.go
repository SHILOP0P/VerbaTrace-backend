package analysis

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetByCallUUID(ctx context.Context, callUUID uuid.UUID, userID uuid.UUID) (models.CallAnalysis, error) {
	if callUUID == uuid.Nil || userID == uuid.Nil {
		return models.CallAnalysis{}, models.ErrInvalidAnalysisInput
	}

	if _, err := s.callRepository.GetByUUID(ctx, callUUID, userID); err != nil {
		return models.CallAnalysis{}, fmt.Errorf("get call: %w", err)
	}

	analysis, err := s.analysisRepository.GetByCallUUID(ctx, callUUID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("get analysis: %w", err)
	}

	return analysis, nil
}
