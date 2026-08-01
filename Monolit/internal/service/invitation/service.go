package invitation

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/username"

	"github.com/google/uuid"
)

const defaultInvitationTTL = 7 * 24 * time.Hour

type BillingLimiter interface {
	CanUseCompany(ctx context.Context, companyID uuid.UUID) error
	CanAddCompanyMember(ctx context.Context, companyID uuid.UUID) error
}

type Service struct {
	invitationRepository repo.InvitationRepository
	userRepository       repo.UserRepository
	companyRepository    repo.CompanyRepository
	departmentRepository repo.DepartmentRepository
	notificationService  interface {
		Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
	}
	billingLimiter BillingLimiter
	now            func() time.Time
	log            logger.Logger
}

func NewService(invitationRepository repo.InvitationRepository, userRepository repo.UserRepository, companyRepository repo.CompanyRepository, departmentRepository repo.DepartmentRepository, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		invitationRepository: invitationRepository,
		userRepository:       userRepository,
		companyRepository:    companyRepository,
		departmentRepository: departmentRepository,
		now:                  func() time.Time { return time.Now().UTC() },
		log:                  log,
	}
}

func (s *Service) SetBillingLimiter(limiter BillingLimiter) {
	s.billingLimiter = limiter
}

func (s *Service) SetNotificationService(notificationService interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
}) {
	s.notificationService = notificationService
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func validInvitationStatus(status models.InvitationStatus) bool {
	return status == "" ||
		status == models.InvitationStatusPending ||
		status == models.InvitationStatusAccepted ||
		status == models.InvitationStatusDeclined ||
		status == models.InvitationStatusCanceled ||
		status == models.InvitationStatusExpired
}

func (s *Service) ensureTargetUser(ctx context.Context, requestUser uuid.UUID, targetUser uuid.UUID) error {
	if requestUser == uuid.Nil || targetUser == uuid.Nil || requestUser == targetUser {
		return models.ErrInvalidInvitationInput
	}

	_, err := s.userRepository.GetUserByUUID(ctx, targetUser)
	return err
}

func (s *Service) resolveTargetUser(ctx context.Context, requestUser uuid.UUID, userID uuid.UUID, rawUsername string) (uuid.UUID, error) {
	if userID != uuid.Nil && rawUsername != "" {
		return uuid.Nil, models.ErrInvalidInvitationInput
	}
	if userID != uuid.Nil {
		if err := s.ensureTargetUser(ctx, requestUser, userID); err != nil {
			return uuid.Nil, err
		}
		return userID, nil
	}

	normalized, ok := username.Normalize(rawUsername)
	if !ok {
		return uuid.Nil, models.ErrInvalidInvitationInput
	}

	user, err := s.userRepository.GetUserByUsername(ctx, normalized)
	if err != nil {
		return uuid.Nil, err
	}
	if user.ID == requestUser {
		return uuid.Nil, models.ErrInvalidInvitationInput
	}

	return user.ID, nil
}

func (s *Service) isActiveCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (bool, error) {
	_, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, models.ErrCompanyNotFound) {
		return false, nil
	}
	return false, err
}

func (s *Service) isActiveDepartmentMember(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) (bool, error) {
	_, err := s.departmentRepository.GetDepartmentMember(ctx, companyID, departmentID, userID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, models.ErrDepartmentNotFound) {
		return false, nil
	}
	return false, err
}
