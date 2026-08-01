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
		Language:     transcription.Language,
		Provider:     transcription.Provider,
		ErrorMessage: transcription.ErrorMessage,
		CreatedAt:    transcription.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    transcription.UpdatedAt.Format(time.RFC3339),
	}, nil
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
