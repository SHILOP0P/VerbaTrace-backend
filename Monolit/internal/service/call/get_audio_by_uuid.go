package call

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetAudioByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.File, error) {
	call, err := s.repository.GetByUUID(ctx, id, userID)
	if err != nil {
		return models.File{}, err
	}

	content, err := s.audioStorage.OpenReadSeeker(ctx, call.AudioPath)
	if err != nil {
		return models.File{}, fmt.Errorf("error opening audio storage: %w", err)
	}

	return models.File{
		Content:          content,
		ReadSeeker:       content,
		Path:             call.AudioPath,
		OriginalFilename: call.OriginalFilename,
		MimeType:         call.MimeType,
		SizeBytes:        call.SizeBytes,
	}, nil
}
