package monitoring

import (
	"context"
	"errors"
	"testing"

	"verbatrace/monolit/internal/models"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetProcessingAllowsAdminWithoutCompanyFilter(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	monitoringRepository := repositoryMocks.NewMonitoringRepository(t)
	service := NewService(monitoringRepository, nil)

	monitoringRepository.On("GetMonitoring", mock.Anything, mock.MatchedBy(func(input models.ProcessingMonitoringInput) bool {
		return input.UserID == userID && input.UserRole == models.UserRoleAdmin && !input.CompanyUUID.Valid
	})).Return(models.ProcessingMonitoring{}, nil).Once()

	_, err := service.GetProcessing(ctx, models.ProcessingMonitoringInput{
		UserID:   userID,
		UserRole: models.UserRoleAdmin,
	})

	require.NoError(t, err)
}

func TestGetProcessingAllowsOnlyOwnManagedCompany(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	monitoringRepository := repositoryMocks.NewMonitoringRepository(t)
	companyRepository := repositoryMocks.NewCompanyRepository(t)
	service := NewService(monitoringRepository, companyRepository)

	companyRepository.On("GetCompanyMember", mock.Anything, companyID, userID).Return(models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleManager,
		Status:      models.MembershipStatusActive,
	}, nil).Once()
	monitoringRepository.On("GetMonitoring", mock.Anything, mock.MatchedBy(func(input models.ProcessingMonitoringInput) bool {
		return input.UserID == userID && input.CompanyUUID.Valid && input.CompanyUUID.UUID == companyID
	})).Return(models.ProcessingMonitoring{}, nil).Once()

	_, err := service.GetProcessing(ctx, models.ProcessingMonitoringInput{
		UserID:      userID,
		UserRole:    models.UserRoleUser,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
	})

	require.NoError(t, err)
}

func TestGetProcessingDeniesForeignCompany(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	monitoringRepository := repositoryMocks.NewMonitoringRepository(t)
	companyRepository := repositoryMocks.NewCompanyRepository(t)
	service := NewService(monitoringRepository, companyRepository)

	companyRepository.On("GetCompanyMember", mock.Anything, companyID, userID).Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()

	_, err := service.GetProcessing(ctx, models.ProcessingMonitoringInput{
		UserID:      userID,
		UserRole:    models.UserRoleUser,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
	})

	require.True(t, errors.Is(err, models.ErrCompanyNotFound))
}
