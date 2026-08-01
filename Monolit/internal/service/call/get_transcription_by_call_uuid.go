package call

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetTranscriptionByCallUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Transcription, error) {
	if s.transcriptionRepository == nil {
		return models.Transcription{}, fmt.Errorf("transcription repository is not configured")
	}

	if _, err := s.repository.GetByUUID(ctx, id, userID); err != nil {
		return models.Transcription{}, err
	}

	return s.transcriptionRepository.GetByCallUUID(ctx, id)
}
