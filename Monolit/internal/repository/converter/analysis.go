package converter

import (
	"encoding/json"

	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoCallAnalysisToModel(repoAnalysis repoModel.CallAnalysis) (model.CallAnalysis, error) {
	return model.CallAnalysis{
		ID:           repoAnalysis.ID,
		CallUUID:     repoAnalysis.CallUUID,
		Status:       model.CallAnalysisStatus(repoAnalysis.Status),
		Provider:     repoAnalysis.Provider,
		Model:        nullStringToStringPtr(repoAnalysis.Model),
		ResultJSON:   cloneRawMessage(repoAnalysis.ResultJSON),
		ResultText:   nullStringToStringPtr(repoAnalysis.ResultText),
		ErrorMessage: nullStringToStringPtr(repoAnalysis.ErrorMessage),
		CreatedAt:    repoAnalysis.CreatedAt,
		UpdatedAt:    repoAnalysis.UpdatedAt,
	}, nil
}

func ModelCallAnalysisToRepoModel(analysis model.CallAnalysis) (repoModel.CallAnalysis, error) {
	return repoModel.CallAnalysis{
		ID:           analysis.ID,
		CallUUID:     analysis.CallUUID,
		Status:       repoModel.CallAnalysisStatus(analysis.Status),
		Provider:     analysis.Provider,
		Model:        stringPtrToNullString(analysis.Model),
		ResultJSON:   cloneRawMessage(analysis.ResultJSON),
		ResultText:   stringPtrToNullString(analysis.ResultText),
		ErrorMessage: stringPtrToNullString(analysis.ErrorMessage),
		CreatedAt:    analysis.CreatedAt,
		UpdatedAt:    analysis.UpdatedAt,
	}, nil
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}

	cloned := make(json.RawMessage, len(value))
	copy(cloned, value)
	return cloned
}
