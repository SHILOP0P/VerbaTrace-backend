package company

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/username"

	"github.com/google/uuid"
)

func (s *Service) UpdateCompany(ctx context.Context, input models.UpdateCompanyInput) (models.Company, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.Name == "" {
		return models.Company{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Company{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.Company{}, err
	}

	return s.companyRepository.UpdateCompany(ctx, input.CompanyUUID, input.Name)
}

func (s *Service) UpdateCompanyTag(ctx context.Context, input models.UpdateCompanyTagInput) (models.Company, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.Company{}, models.ErrInvalidCompanyInput
	}
	tag, err := normalizeCompanyTag(input.CompanyUUID, input.Tag)
	if err != nil {
		return models.Company{}, err
	}
	if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Company{}, err
	}
	return s.companyRepository.UpdateCompanyTag(ctx, input.CompanyUUID, tag)
}

func (s *Service) UpdateCompanyTagAsAdmin(ctx context.Context, companyID uuid.UUID, tag string) (models.Company, error) {
	normalized, err := normalizeCompanyTag(companyID, tag)
	if err != nil {
		return models.Company{}, err
	}
	return s.companyRepository.UpdateCompanyTag(ctx, companyID, normalized)
}

func normalizeCompanyTag(companyID uuid.UUID, tag string) (string, error) {
	tag = strings.TrimSpace(tag)
	if companyID == uuid.Nil || tag == "" {
		return "", models.ErrInvalidCompanyInput
	}
	defaultTag := "@" + companyID.String()
	if strings.EqualFold(tag, defaultTag) {
		return defaultTag, nil
	}
	normalized, ok := username.Normalize(tag)
	if !ok {
		return "", models.ErrInvalidCompanyInput
	}
	return normalized, nil
}

func (s *Service) DeleteCompany(ctx context.Context, input models.DeleteCompanyInput) error {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return err
	}

	// A company is deleted only once it is empty, otherwise members would lose
	// access to their calls without ever being told.
	others, err := s.companyRepository.CountActiveCompanyMembersExcept(ctx, input.CompanyUUID, input.RequestUser)
	if err != nil {
		return err
	}
	if others > 0 {
		return models.ErrCompanyNotEmpty
	}

	// Deleting a company starts its lifecycle rather than ending it: 30 days
	// frozen, 30 days soft deleted, and only then gone. Data that somebody
	// wants to keep has to be moved out before that runs out.
	if lifecycle, ok := s.companyRepository.(lifecycleRepository); ok {
		return lifecycle.FreezeCompany(ctx, input.CompanyUUID, time.Now().UTC())
	}

	return s.companyRepository.ArchiveCompany(ctx, input.CompanyUUID)
}
