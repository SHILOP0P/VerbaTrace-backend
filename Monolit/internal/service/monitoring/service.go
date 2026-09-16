package monitoring

import (
	"context"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

type Service struct {
	monitoringRepository repository.MonitoringRepository
	companyRepository    repository.CompanyRepository
}

func NewService(monitoringRepository repository.MonitoringRepository, companyRepository repository.CompanyRepository) *Service {
	return &Service{
		monitoringRepository: monitoringRepository,
		companyRepository:    companyRepository,
	}
}

func (s *Service) GetProcessing(ctx context.Context, input models.ProcessingMonitoringInput) (models.ProcessingMonitoring, error) {
	if input.UserRole == models.UserRoleAdmin || input.UserRole == models.UserRoleSuperAdmin {
		return s.monitoringRepository.GetMonitoring(ctx, input)
	}

	if s.companyRepository == nil {
		return models.ProcessingMonitoring{}, models.ErrForbidden
	}

	if input.CompanyUUID.Valid {
		if err := s.requireCompanyManager(ctx, input.CompanyUUID.UUID, input.UserID); err != nil {
			return models.ProcessingMonitoring{}, err
		}
		return s.monitoringRepository.GetMonitoring(ctx, input)
	}

	company, err := s.companyRepository.GetManagedCompanyByUserUUID(ctx, input.UserID)
	if err != nil {
		return models.ProcessingMonitoring{}, err
	}

	input.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	return s.monitoringRepository.GetMonitoring(ctx, input)
}

func (s *Service) requireCompanyManager(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}
	if !member.Role.ManagesCompany() {
		return models.ErrForbidden
	}
	return nil
}
