package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanTranscription(row rowScanner) (repoModel.Transcription, error) {
	var transcription repoModel.Transcription

	err := row.Scan(
		&transcription.ID,
		&transcription.CallUUID,
		&transcription.Status,
		&transcription.Text,
		&transcription.Segments,
		&transcription.Language,
		&transcription.Provider,
		&transcription.ErrorMessage,
		&transcription.CreatedAt,
		&transcription.UpdatedAt,
	)
	if err != nil {
		return repoModel.Transcription{}, err
	}

	return transcription, nil
}
