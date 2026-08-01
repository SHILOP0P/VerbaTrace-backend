package auth

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetPreferences(ctx context.Context, userID uuid.UUID) (models.UserPreferences, error) {
	if s.preferencesRepository == nil {
		return models.UserPreferences{}, models.ErrInvalidUserInput
	}
	return s.preferencesRepository.Get(ctx, userID)
}

func (s *Service) UpdatePreferences(ctx context.Context, input models.UpdateUserPreferencesInput) (models.UserPreferences, error) {
	if s.preferencesRepository == nil {
		return models.UserPreferences{}, models.ErrInvalidUserInput
	}
	if input.UserUUID == uuid.Nil {
		return models.UserPreferences{}, models.ErrInvalidUserInput
	}

	if input.Theme != nil {
		theme := strings.TrimSpace(*input.Theme)
		if theme != "system" && theme != "light" && theme != "dark" {
			return models.UserPreferences{}, models.ErrInvalidUserInput
		}
		input.Theme = &theme
	}

	if input.DateRange != nil {
		if err := validateDateRange(*input.DateRange); err != nil {
			return models.UserPreferences{}, err
		}
	}

	if input.ActiveCompanyUUID != nil && input.ActiveCompanyUUID.Valid {
		if s.companyRepository == nil {
			return models.UserPreferences{}, models.ErrInvalidUserInput
		}
		if _, err := s.companyRepository.GetCompanyMember(ctx, input.ActiveCompanyUUID.UUID, input.UserUUID); err != nil {
			return models.UserPreferences{}, err
		}
	}

	return s.preferencesRepository.Upsert(ctx, input)
}

func validateDateRange(dateRange models.PreferencesDateRange) error {
	var fromTime time.Time
	var toTime time.Time
	if dateRange.From != nil {
		parsed, err := time.Parse("2006-01-02", *dateRange.From)
		if err != nil {
			return models.ErrInvalidUserInput
		}
		fromTime = parsed
	}
	if dateRange.To != nil {
		parsed, err := time.Parse("2006-01-02", *dateRange.To)
		if err != nil {
			return models.ErrInvalidUserInput
		}
		toTime = parsed
	}
	if dateRange.From != nil && dateRange.To != nil && fromTime.After(toTime) {
		return models.ErrInvalidUserInput
	}
	return nil
}
