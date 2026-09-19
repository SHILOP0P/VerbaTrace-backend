package analysis_instruction

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// AuthorizeRead answers for other domains built on an instruction, such as its
// scorecard: whoever may read the instruction may read what was derived from it.
func (s *Service) AuthorizeRead(ctx context.Context, instructionID uuid.UUID, userID uuid.UUID) (models.AnalysisInstruction, error) {
	if instructionID == uuid.Nil || userID == uuid.Nil {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}
	instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, instructionID)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}
	if err := s.authorizeRead(ctx, instruction, userID); err != nil {
		return models.AnalysisInstruction{}, err
	}
	return instruction, nil
}

// AuthorizeEdit is the same for changes: whoever may edit the instruction may
// edit its scorecard.
func (s *Service) AuthorizeEdit(ctx context.Context, instructionID uuid.UUID, userID uuid.UUID) (models.AnalysisInstruction, error) {
	if instructionID == uuid.Nil || userID == uuid.Nil {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}
	instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, instructionID)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}
	if err := s.authorizeEdit(ctx, instruction, userID); err != nil {
		return models.AnalysisInstruction{}, err
	}
	return instruction, nil
}
