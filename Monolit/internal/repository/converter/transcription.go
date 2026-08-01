package converter

import (
	"database/sql"
	"encoding/json"
	"fmt"

	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoTranscriptionToModel(repoTranscription repoModel.Transcription) (model.Transcription, error) {
	segments, err := nullStringToTranscriptionSegments(repoTranscription.Segments)
	if err != nil {
		return model.Transcription{}, err
	}

	return model.Transcription{
		ID:           repoTranscription.ID,
		CallUUID:     repoTranscription.CallUUID,
		Status:       model.TranscriptionStatus(repoTranscription.Status),
		Text:         nullStringToStringPtr(repoTranscription.Text),
		Segments:     segments,
		Language:     nullStringToStringPtr(repoTranscription.Language),
		Provider:     repoTranscription.Provider,
		ErrorMessage: nullStringToStringPtr(repoTranscription.ErrorMessage),
		CreatedAt:    repoTranscription.CreatedAt,
		UpdatedAt:    repoTranscription.UpdatedAt,
	}, nil
}

func ModelTranscriptionToRepoModel(transcription model.Transcription) (repoModel.Transcription, error) {
	segments, err := TranscriptionSegmentsToNullString(transcription.Segments)
	if err != nil {
		return repoModel.Transcription{}, err
	}

	return repoModel.Transcription{
		ID:           transcription.ID,
		CallUUID:     transcription.CallUUID,
		Status:       repoModel.TranscriptionStatus(transcription.Status),
		Text:         stringPtrToNullString(transcription.Text),
		Segments:     segments,
		Language:     stringPtrToNullString(transcription.Language),
		Provider:     transcription.Provider,
		ErrorMessage: stringPtrToNullString(transcription.ErrorMessage),
		CreatedAt:    transcription.CreatedAt,
		UpdatedAt:    transcription.UpdatedAt,
	}, nil
}

func TranscriptionSegmentsToNullString(segments []model.TranscriptionSegment) (sql.NullString, error) {
	if len(segments) == 0 {
		return sql.NullString{}, nil
	}

	data, err := json.Marshal(segments)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal transcription segments: %w", err)
	}

	return sql.NullString{String: string(data), Valid: true}, nil
}

func nullStringToTranscriptionSegments(value sql.NullString) ([]model.TranscriptionSegment, error) {
	if !value.Valid || value.String == "" {
		return []model.TranscriptionSegment{}, nil
	}

	var segments []model.TranscriptionSegment
	if err := json.Unmarshal([]byte(value.String), &segments); err != nil {
		return nil, fmt.Errorf("decode transcription segments: %w", err)
	}

	return segments, nil
}
