package department

import (
	"context"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type BillingLimiter interface {
	CanUseCompany(ctx context.Context, companyID uuid.UUID) error
	CanCreateDepartment(ctx context.Context, companyID uuid.UUID) error
	CanAddCompanyMember(ctx context.Context, companyID uuid.UUID) error
}

type NotificationService interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
}

type Service struct {
	companyRepository    repo.CompanyRepository
	departmentRepository repo.DepartmentRepository
	billingLimiter       BillingLimiter
	notificationService  NotificationService
	log                  logger.Logger
}

func (s *Service) SetNotificationService(notificationService NotificationService) {
	s.notificationService = notificationService
}

// notify never fails the membership decision it reports on.
func (s *Service) notify(ctx context.Context, userID uuid.UUID, notificationType models.NotificationType, title string, body string, entityID uuid.UUID) {
	if s.notificationService == nil || userID == uuid.Nil {
		return
	}

	entityType := "department"
	if _, err := s.notificationService.Create(ctx, models.CreateNotificationInput{
		UserUUID:   userID,
		Type:       notificationType,
		Title:      title,
		Body:       body,
		EntityType: &entityType,
		EntityUUID: uuid.NullUUID{UUID: entityID, Valid: entityID != uuid.Nil},
	}); err != nil {
		s.log.Warn(ctx, "failed to create department notification", zap.Error(err), zap.String("user_id", userID.String()))
	}
}

// approverForCompany points approval requests at the deputy, with the owner as
// the fallback.
func (s *Service) approverForCompany(ctx context.Context, companyID uuid.UUID) (uuid.UUID, error) {
	overview, err := s.companyRepository.GetCompanyMembersOverview(ctx, companyID)
	if err != nil {
		return uuid.Nil, err
	}
	if overview.Deputy != nil {
		return overview.Deputy.UserUUID, nil
	}
	if overview.Manager != nil {
		return overview.Manager.UserUUID, nil
	}

	return uuid.Nil, models.ErrCompanyNotFound
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
