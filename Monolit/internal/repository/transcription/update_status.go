package transcription

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

func (r *Repository) MarkTranscribed(ctx context.Context, id uuid.UUID, text string, segments []model.TranscriptionSegment, language *string) (model.Transcription, error) {
	repoSegments, err := converter.TranscriptionSegmentsToNullString(segments)
	if err != nil {
		return model.Transcription{}, err
	}

	query := `
	UPDATE call_transcriptions
	SET status = $2,
	    text = $3,
	    segments = $4::jsonb,
	    language = $5,
	    error_message = NULL,
	    updated_at = now()
	WHERE transcription_uuid = $1
	RETURNING ` + transcriptionReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.TranscriptionStatusTranscribed), text, repoSegments, language)

	return scanUpdatedTranscription(row, "mark transcription transcribed")
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, errorMessage string) (model.Transcription, error) {
	query := `
	UPDATE call_transcriptions
	SET status = $2,
	    text = NULL,
	    segments = NULL,
	    language = NULL,
	    error_message = $3,
	    updated_at = now()
	WHERE transcription_uuid = $1
	RETURNING ` + transcriptionReturningColumns

	row := r.db.QueryRowContext(ctx, query, id, string(model.TranscriptionStatusFailed), errorMessage)

	return scanUpdatedTranscription(row, "mark transcription failed")
}

func scanUpdatedTranscription(row interface {
	Scan(dest ...any) error
}, operation string) (model.Transcription, error) {
	repoTranscription, err := scaner.ScanTranscription(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Transcription{}, model.ErrTranscriptionNotFound
		}
		return model.Transcription{}, fmt.Errorf("%s: %w", operation, err)
	}

	return converter.RepoTranscriptionToModel(repoTranscription)
}
