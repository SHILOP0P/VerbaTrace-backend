package analysis

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) Create(ctx context.Context, analysis model.CallAnalysis) (model.CallAnalysis, error) {
	repoAnalysis, err := converter.ModelCallAnalysisToRepoModel(analysis)
	if err != nil {
		return model.CallAnalysis{}, model.ErrInvalidAnalysisInput
	}

	query := `
	INSERT INTO call_analyses (
		analysis_uuid,
		call_uuid,
		status,
		provider,
		model,
		result_json,
		result_text,
		error_message,
		created_at,
		updated_at
	)
	VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)
	ON CONFLICT (call_uuid) DO UPDATE
	SET status = EXCLUDED.status,
	    provider = EXCLUDED.provider,
	    model = EXCLUDED.model,
	    error_message = NULL,
	    updated_at = now()
	RETURNING ` + analysisReturningColumns

	row := r.db.QueryRowContext(ctx, query,
		repoAnalysis.ID,
		repoAnalysis.CallUUID,
		repoAnalysis.Status,
		repoAnalysis.Provider,
		repoAnalysis.Model,
		[]byte(repoAnalysis.ResultJSON),
		repoAnalysis.ResultText,
		repoAnalysis.ErrorMessage,
		repoAnalysis.CreatedAt,
		repoAnalysis.UpdatedAt,
	)

	createdAnalysis, err := scaner.ScanCallAnalysis(row)
	if err != nil {
		return model.CallAnalysis{}, fmt.Errorf("create analysis: %w", err)
	}

	return converter.RepoCallAnalysisToModel(createdAnalysis)
}
