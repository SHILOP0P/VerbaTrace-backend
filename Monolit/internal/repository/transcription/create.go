package transcription

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) Create(ctx context.Context, transcription model.Transcription) (model.Transcription, error) {
	repoTranscription, err := converter.ModelTranscriptionToRepoModel(transcription)
	if err != nil {
		return model.Transcription{}, model.ErrInvalidTranscriptionInput
	}

	query := `
	INSERT INTO call_transcriptions (
		transcription_uuid,
		call_uuid,
		status,
		text,
		segments,
		words,
		language,
		provider,
		error_message,
		created_at,
		updated_at
	)
	VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9, $10, $11)
	ON CONFLICT (call_uuid) DO UPDATE
	SET status = EXCLUDED.status,
	    text = NULL,
	    segments = NULL,
	    words = NULL,
	    language = NULL,
	    provider = EXCLUDED.provider,
	    error_message = NULL,
	    updated_at = now()
	RETURNING ` + transcriptionReturningColumns

	row := r.db.QueryRowContext(ctx, query,
		repoTranscription.ID,
		repoTranscription.CallUUID,
		repoTranscription.Status,
		repoTranscription.Text,
		repoTranscription.Segments,
		repoTranscription.Words,
		repoTranscription.Language,
		repoTranscription.Provider,
		repoTranscription.ErrorMessage,
		repoTranscription.CreatedAt,
		repoTranscription.UpdatedAt,
	)

	createdTranscription, err := scaner.ScanTranscription(row)
	if err != nil {
		return model.Transcription{}, fmt.Errorf("create transcription: %w", err)
	}

	return converter.RepoTranscriptionToModel(createdTranscription)
}
