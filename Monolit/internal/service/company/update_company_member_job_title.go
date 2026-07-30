package company

import (
	"context"
	"strings"
	"unicode/utf8"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const maxJobTitleRunes = 200

func (s *Service) UpdateCompanyMemberJobTitle(
	ctx context.Context,
	input models.UpdateCompanyMemberJobTitleInput,
) (models.CompanyMember, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.JobTitle != nil {
		value := strings.TrimSpace(*input.JobTitle)
		if value == "" {
			input.JobTitle = nil
		} else {
			if utf8.RuneCountInString(value) > maxJobTitleRunes {
				return models.CompanyMember{}, models.ErrInvalidCompanyInput
			}
			input.JobTitle = &value
		}
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyMember{}, err
	}
	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMember{}, err
	}

	repository, ok := s.companyRepository.(jobTitleRepository)
	if !ok {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	member, err := repository.UpdateCompanyMemberJobTitle(
		ctx,
		input.CompanyUUID,
		input.UserUUID,
		input.JobTitle,
	)
	if err != nil {
		s.log.Error(ctx, "failed to update company member job title",
			zap.String("company_id", input.CompanyUUID.String()),
			zap.String("request_user_id", input.RequestUser.String()),
			zap.String("user_id", input.UserUUID.String()),
			zap.Error(err))
		return models.CompanyMember{}, err
	}

	return member, nil
}
