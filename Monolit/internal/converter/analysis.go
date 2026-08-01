package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func AnalysisModelToAPI(analysis models.CallAnalysis) (dto.AnalysisResponse, error) {
	return dto.AnalysisResponse{
		ID:           analysis.ID.String(),
		CallUUID:     analysis.CallUUID.String(),
		Status:       string(analysis.Status),
		Provider:     analysis.Provider,
		Model:        analysis.Model,
		ResultJSON:   analysis.ResultJSON,
		ResultText:   analysis.ResultText,
		ErrorMessage: analysis.ErrorMessage,
		CreatedAt:    analysis.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    analysis.UpdatedAt.Format(time.RFC3339),
	}, nil
}
