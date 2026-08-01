package analysis_instruction

import (
	"context"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
)

type BillingLimiter interface {
	CanCreatePersonalInstruction(ctx context.Context, userID uuid.UUID) error
	CanCreateCompanyInstruction(ctx context.Context, companyID uuid.UUID) error
	CanCreateDepartmentInstruction(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) error
}

type Service struct {
	repository           repo.AnalysisInstructionRepository
	companyRepository    repo.CompanyRepository
	departmentRepository repo.DepartmentRepository
	instructionStorage   storage.InstructionStorage
	billingLimiter       BillingLimiter
	log                  logger.Logger
}

func NewService(
	repository repo.AnalysisInstructionRepository,
	companyRepository repo.CompanyRepository,
	departmentRepository repo.DepartmentRepository,
	instructionStorage storage.InstructionStorage,
	log logger.Logger,
) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		repository:           repository,
		companyRepository:    companyRepository,
		departmentRepository: departmentRepository,
		instructionStorage:   instructionStorage,
		log:                  log,
	}
}

func (s *Service) SetBillingLimiter(limiter BillingLimiter) {
	s.billingLimiter = limiter
}

func (s *Service) checkBillingLimit(ctx context.Context, input models.CreateAnalysisInstructionInput, ownerFilter models.ListAnalysisInstructionsInput) (int, error) {
	if s.billingLimiter != nil {
		switch input.Scope {
		case models.AnalysisInstructionScopePersonal:
			if err := s.billingLimiter.CanCreatePersonalInstruction(ctx, input.UserUUID); err != nil {
				return 0, err
			}
		case models.AnalysisInstructionScopeCompany:
			if err := s.billingLimiter.CanCreateCompanyInstruction(ctx, input.CompanyUUID.UUID); err != nil {
				return 0, err
			}
		case models.AnalysisInstructionScopeDepartment:
			if err := s.billingLimiter.CanCreateDepartmentInstruction(ctx, input.CompanyUUID.UUID, input.DepartmentUUID.UUID); err != nil {
				return 0, err
			}
		default:
			return 0, models.ErrInvalidAnalysisInstructionInput
		}

		return s.repository.CountActive(ctx, ownerFilter)
	}

	limit := instructionLimit(input.Scope)
	count, err := s.repository.CountActive(ctx, ownerFilter)
	if err != nil {
		return 0, err
	}
	if count >= limit {
		return 0, models.ErrInstructionLimitExceeded
	}

	return count, nil
}
