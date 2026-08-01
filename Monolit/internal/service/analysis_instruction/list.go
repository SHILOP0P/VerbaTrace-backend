package analysis_instruction

import (
	"context"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) List(ctx context.Context, input models.ListAnalysisInstructionsInput) ([]models.AnalysisInstruction, error) {
	input = normalizeListInput(input)
	if err := validateListInput(input); err != nil {
		return nil, err
	}

	if err := s.authorizeList(ctx, input); err != nil {
		return nil, err
	}

	return s.repository.List(ctx, input)
}

func validateListInput(input models.ListAnalysisInstructionsInput) error {
	if input.UserUUID == uuid.Nil {
		return models.ErrInvalidAnalysisInstructionInput
	}
	if input.Limit < 0 || input.Offset < 0 {
		return models.ErrInvalidAnalysisInstructionInput
	}

	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		if input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidAnalysisInstructionInput
		}
	case models.AnalysisInstructionScopeCompany:
		if !input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidAnalysisInstructionInput
		}
	case models.AnalysisInstructionScopeDepartment:
		if !input.CompanyUUID.Valid || !input.DepartmentUUID.Valid {
			return models.ErrInvalidAnalysisInstructionInput
		}
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}

	return nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.AnalysisInstruction, error) {
	if id == uuid.Nil || userID == uuid.Nil {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}

	instruction, err := s.repository.GetByUUID(ctx, id)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}

	if err := s.authorizeRead(ctx, instruction, userID); err != nil {
		return models.AnalysisInstruction{}, err
	}

	return instruction, nil
}

func normalizeListInput(input models.ListAnalysisInstructionsInput) models.ListAnalysisInstructionsInput {
	input.Query = strings.TrimSpace(input.Query)
	return input
}
