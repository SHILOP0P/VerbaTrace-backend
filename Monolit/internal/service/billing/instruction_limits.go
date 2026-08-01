package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) CanCreatePersonalInstruction(ctx context.Context, userID uuid.UUID) error {
	subscription, err := s.activePersonalSubscription(ctx, userID)
	if err != nil {
		return err
	}

	count, err := s.repository.CountActiveInstructions(ctx, models.ListAnalysisInstructionsInput{
		Scope:    models.AnalysisInstructionScopePersonal,
		UserUUID: userID,
	})
	if err != nil {
		return err
	}

	if count >= subscription.Plan.ActiveInstructionLimit {
		return models.ErrInstructionLimitExceeded
	}

	return nil
}

func (s *Service) CanCreateCompanyInstruction(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	count, err := s.repository.CountActiveInstructions(ctx, models.ListAnalysisInstructionsInput{
		Scope:       models.AnalysisInstructionScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
	})
	if err != nil {
		return err
	}

	if subscription.Plan.InstructionsPerDepartmentLimit == nil {
		return models.ErrInstructionLimitExceeded
	}

	if count >= *subscription.Plan.InstructionsPerDepartmentLimit {
		return models.ErrInstructionLimitExceeded
	}

	return nil
}

func (s *Service) CanCreateDepartmentInstruction(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	count, err := s.repository.CountActiveInstructions(ctx, models.ListAnalysisInstructionsInput{
		Scope:          models.AnalysisInstructionScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
	})
	if err != nil {
		return err
	}

	if subscription.Plan.InstructionsPerDepartmentLimit == nil {
		return models.ErrInstructionLimitExceeded
	}

	if count >= *subscription.Plan.InstructionsPerDepartmentLimit {
		return models.ErrInstructionLimitExceeded
	}

	return nil
}
