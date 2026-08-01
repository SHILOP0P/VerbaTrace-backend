package company

import (
	"context"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

type jobTitleRepository interface {
	UpdateCompanyMemberJobTitle(
		ctx context.Context,
		companyID uuid.UUID,
		userID uuid.UUID,
		jobTitle *string,
	) (models.CompanyMember, error)
}

type BillingLimiter interface {
	CanUseCompany(ctx context.Context, companyID uuid.UUID) error
	CanAddCompanyMember(ctx context.Context, companyID uuid.UUID) error
}

type Service struct {
	companyRepository repo.CompanyRepository
	billingLimiter    BillingLimiter
	log               logger.Logger
}

func NewService(companyRepository repo.CompanyRepository, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		companyRepository: companyRepository,
		log:               log,
	}
}

func (s *Service) SetBillingLimiter(limiter BillingLimiter) {
	s.billingLimiter = limiter
}
