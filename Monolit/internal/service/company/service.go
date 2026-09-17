package company

import (
	"context"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
	"go.uber.org/zap"
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
	// CanCreateCompany answers whether the owner's plan covers one more company.
	// A company over the limit is not created at all, rather than created and
	// quietly given the same plan as the ones that are paid for.
	CanCreateCompany(ctx context.Context, ownerID uuid.UUID) error
}

type NotificationService interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
}

type Service struct {
	companyRepository   repo.CompanyRepository
	billingLimiter      BillingLimiter
	notificationService NotificationService
	freezeNotifier      FreezeNotifier
	log                 logger.Logger
}

func (s *Service) SetNotificationService(notificationService NotificationService) {
	s.notificationService = notificationService
}

// notify never fails the operation: a missing notification must not roll back a
// membership decision that already happened.
func (s *Service) notify(ctx context.Context, userID uuid.UUID, notificationType models.NotificationType, title string, body string, entityType string, entityID uuid.UUID) {
	if s.notificationService == nil || userID == uuid.Nil {
		return
	}

	_, err := s.notificationService.Create(ctx, models.CreateNotificationInput{
		UserUUID:   userID,
		Type:       notificationType,
		Title:      title,
		Body:       body,
		EntityType: &entityType,
		EntityUUID: uuid.NullUUID{UUID: entityID, Valid: entityID != uuid.Nil},
	})
	if err != nil {
		s.log.Warn(ctx, "failed to create company notification", zap.String("user_id", userID.String()), zap.String("type", string(notificationType)), zap.Error(err))
	}
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
