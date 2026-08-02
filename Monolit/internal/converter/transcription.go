package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func TranscriptionModelToAPI(transcription models.Transcription) (dto.TranscriptionResponse, error) {
	return dto.TranscriptionResponse{
		ID:           transcription.ID.String(),
		CallUUID:     transcription.CallUUID.String(),
		Status:       string(transcription.Status),
		Text:         transcription.Text,
		Segments:     transcriptionSegmentsToAPI(transcription.Segments),
		Words:        transcriptionWordsToAPI(transcription.Words),
		Language:     transcription.Language,
		Provider:     transcription.Provider,
		ErrorMessage: transcription.ErrorMessage,
		CreatedAt:    transcription.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    transcription.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func transcriptionWordsToAPI(words []models.TranscriptionWord) []dto.TranscriptionWordResponse {
	result := make([]dto.TranscriptionWordResponse, 0, len(words))
	for _, word := range words {
		result = append(result, dto.TranscriptionWordResponse{
			Text: word.Text, StartSeconds: word.StartSeconds, EndSeconds: word.EndSeconds,
			Confidence: word.Confidence, Speaker: word.Speaker,
		})
	}
	return result
}

func transcriptionSegmentsToAPI(segments []models.TranscriptionSegment) []dto.TranscriptionSegmentResponse {
	result := make([]dto.TranscriptionSegmentResponse, 0, len(segments))
	for _, segment := range segments {
		result = append(result, dto.TranscriptionSegmentResponse{
			Speaker:      segment.Speaker,
			StartSeconds: segment.StartSeconds,
			EndSeconds:   segment.EndSeconds,
			Text:         segment.Text,
		})
	}

	return result
}
