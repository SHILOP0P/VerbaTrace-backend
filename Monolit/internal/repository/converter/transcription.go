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
	words, err := nullStringToTranscriptionWords(repoTranscription.Words)
	if err != nil {
		return model.Transcription{}, err
	}

	return model.Transcription{
		ID:           repoTranscription.ID,
		CallUUID:     repoTranscription.CallUUID,
		Status:       model.TranscriptionStatus(repoTranscription.Status),
		Text:         nullStringToStringPtr(repoTranscription.Text),
		Segments:     segments,
		Words:        words,
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
	words, err := TranscriptionWordsToNullString(transcription.Words)
	if err != nil {
		return repoModel.Transcription{}, err
	}

	return repoModel.Transcription{
		ID:           transcription.ID,
		CallUUID:     transcription.CallUUID,
		Status:       repoModel.TranscriptionStatus(transcription.Status),
		Text:         stringPtrToNullString(transcription.Text),
		Segments:     segments,
		Words:        words,
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

func TranscriptionWordsToNullString(words []model.TranscriptionWord) (sql.NullString, error) {
	if len(words) == 0 {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(words)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal transcription words: %w", err)
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

func nullStringToTranscriptionWords(value sql.NullString) ([]model.TranscriptionWord, error) {
	if !value.Valid || value.String == "" {
		return []model.TranscriptionWord{}, nil
	}
	var words []model.TranscriptionWord
	if err := json.Unmarshal([]byte(value.String), &words); err != nil {
		return nil, fmt.Errorf("decode transcription words: %w", err)
	}
	return words, nil
}
