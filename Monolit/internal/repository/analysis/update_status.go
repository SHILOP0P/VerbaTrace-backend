package analysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) MarkProcessing(ctx context.Context, id uuid.UUID) (model.CallAnalysis, error) {
	query := `
	UPDATE call_analyses
	SET status = $2,
	    error_message = NULL,
	    updated_at = now()
	WHERE analysis_uuid = $1
	RETURNING ` + analysisReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.CallAnalysisStatusProcessing))

	return scanUpdatedAnalysis(row, "mark analysis processing")
}

func (r *Repository) MarkDone(ctx context.Context, id uuid.UUID, result model.AnalysisResult) (model.CallAnalysis, error) {
	query := `
	UPDATE call_analyses
	SET status = $2,
	    model = $3,
	    result_json = $4::jsonb,
	    result_text = $5,
	    error_message = NULL,
	    updated_at = now()
	WHERE analysis_uuid = $1
	AND ($6 = '' OR (pipeline_run_key=$6 AND $7=(SELECT COALESCE(s.active_revision,(SELECT max(rr.revision) FROM call_transcription_revisions rr WHERE rr.transcription_uuid=t.transcription_uuid),1)
	FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=call_analyses.call_uuid)))
	RETURNING ` + analysisReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.CallAnalysisStatusDone), result.Model, []byte(result.ResultJSON), result.ResultText, result.PipelineRunKey, result.TranscriptionRevision)

	return scanUpdatedAnalysis(row, "mark analysis done")
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, errorMessage string) (model.CallAnalysis, error) {
	query := `
	UPDATE call_analyses
	SET status = $2,
	    error_message = $3,
	    updated_at = now()
	WHERE analysis_uuid = $1
	RETURNING ` + analysisReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.CallAnalysisStatusFailed), errorMessage)

	return scanUpdatedAnalysis(row, "mark analysis failed")
}

func scanUpdatedAnalysis(row interface {
	Scan(dest ...any) error
}, operation string) (model.CallAnalysis, error) {
	repoAnalysis, err := scaner.ScanCallAnalysis(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CallAnalysis{}, model.ErrAnalysisNotFound
		}
		return model.CallAnalysis{}, fmt.Errorf("%s: %w", operation, err)
	}

	return converter.RepoCallAnalysisToModel(repoAnalysis)
}
