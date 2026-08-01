package department

import (
	"context"

	"verbatrace/monolit/internal/logger"
	repo "verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

type BillingLimiter interface {
	CanUseCompany(ctx context.Context, companyID uuid.UUID) error
	CanCreateDepartment(ctx context.Context, companyID uuid.UUID) error
	CanAddCompanyMember(ctx context.Context, companyID uuid.UUID) error
}

type Service struct {
	companyRepository    repo.CompanyRepository
	departmentRepository repo.DepartmentRepository
	billingLimiter       BillingLimiter
	log                  logger.Logger
}

func NewService(companyRepository repo.CompanyRepository, departmentRepository repo.DepartmentRepository, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		companyRepository:    companyRepository,
		departmentRepository: departmentRepository,
		log:                  log,
	}
}

func (s *Service) SetBillingLimiter(limiter BillingLimiter) {
	s.billingLimiter = limiter
}
