package scaner

import (
	"encoding/json"

	repoModel "verbatrace/monolit/internal/repository/models"
)

func ScanCallAnalysis(row rowScanner) (repoModel.CallAnalysis, error) {
	var analysis repoModel.CallAnalysis
	var resultJSON []byte

	err := row.Scan(
		&analysis.ID,
		&analysis.CallUUID,
		&analysis.Status,
		&analysis.Provider,
		&analysis.Model,
		&resultJSON,
		&analysis.ResultText,
		&analysis.ErrorMessage,
		&analysis.CreatedAt,
		&analysis.UpdatedAt,
	)
	if err != nil {
		return repoModel.CallAnalysis{}, err
	}

	if resultJSON != nil {
		analysis.ResultJSON = json.RawMessage(resultJSON)
	}

	return analysis, nil
}
