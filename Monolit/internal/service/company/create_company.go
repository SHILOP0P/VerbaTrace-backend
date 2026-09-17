package company

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const defaultMemberLimit = 1

func (s *Service) CreateCompany(ctx context.Context, input models.CreateCompanyInput) (models.Company, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || input.ManagerUserID == uuid.Nil {
		return models.Company{}, models.ErrInvalidCompanyInput
	}

	// The plan decides how many companies an owner may run, and the answer has
	// to be given here: a company created over the limit would resolve to the
	// same subscription as the paid ones and work for free.
	if s.billingLimiter != nil {
		if err := s.billingLimiter.CanCreateCompany(ctx, input.ManagerUserID); err != nil {
			return models.Company{}, err
		}
	}

	companyID, err := uuid.NewV7()
	if err != nil {
		return models.Company{}, err
	}

	now := time.Now().UTC()
	company := models.Company{
		ID:              companyID,
		Name:            name,
		Tag:             "@" + companyID.String(),
		ManagerUserUUID: input.ManagerUserID,
		MemberLimit:     defaultMemberLimit,
		CreatedAt:       now,
	}

	member := models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    input.ManagerUserID,
		Role:        models.CompanyMemberRoleManager,
		Status:      models.MembershipStatusActive,
		CreatedAt:   now,
	}

	createdCompany, err := s.companyRepository.CreateCompany(ctx, company, member)
	if err != nil {
		s.log.Error(ctx, "failed to create company", zap.String("user_id", input.ManagerUserID.String()), zap.Error(err))
		return models.Company{}, err
	}

	s.log.Info(ctx, "company created", zap.String("user_id", input.ManagerUserID.String()), zap.String("company_id", createdCompany.ID.String()))

	return createdCompany, nil
}
