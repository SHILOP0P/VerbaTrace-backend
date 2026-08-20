package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) requireCompanyManager(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	if s.companyRepository == nil {
		return models.ErrForbidden
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}

	if member.Role != models.CompanyMemberRoleManager {
		return models.ErrForbidden
	}

	return nil
}

func (s *Service) requireActiveCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	if s.companyRepository == nil {
		return models.ErrForbidden
	}
	_, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	return err
}
