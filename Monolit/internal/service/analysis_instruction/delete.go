package analysis_instruction

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	if id == uuid.Nil || userID == uuid.Nil {
		return models.ErrInvalidAnalysisInstructionInput
	}

	instruction, err := s.repository.GetByUUID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.authorizeDelete(ctx, instruction, userID); err != nil {
		return err
	}

	return s.repository.Deactivate(ctx, id)
}
