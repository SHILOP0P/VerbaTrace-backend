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

func (r *Repository) GetByCallUUID(ctx context.Context, callID uuid.UUID) (model.Transcription, error) {
	query := `
	SELECT ` + transcriptionReturningColumns + `
	FROM call_transcriptions
	WHERE call_uuid = $1
	`

	row := r.db.QueryRowContext(ctx, query, callID)

	repoTranscription, err := scaner.ScanTranscription(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Transcription{}, model.ErrTranscriptionNotFound
		}
		return model.Transcription{}, fmt.Errorf("get transcription by call uuid: %w", err)
	}

	return converter.RepoTranscriptionToModel(repoTranscription)
}
