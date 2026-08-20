package notification

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

const (
	defaultNotificationLimit = 20
	maxNotificationLimit     = 100
)

type Service struct {
	repository repository.NotificationRepository
	now        func() time.Time
}

func NewService(repository repository.NotificationRepository) *Service {
	return &Service{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	if input.UserUUID == uuid.Nil || input.Type == "" || input.Title == "" || input.Body == "" {
		return models.Notification{}, models.ErrInvalidNotificationInput
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = s.now()
	}
	return s.repository.Create(ctx, input)
}

func (s *Service) List(ctx context.Context, input models.ListNotificationsInput) (models.ListNotificationsResult, error) {
	if input.UserUUID == uuid.Nil || input.Offset < 0 || input.Limit < 0 || input.Limit > maxNotificationLimit {
		return models.ListNotificationsResult{}, models.ErrInvalidNotificationInput
	}
	if input.Limit == 0 {
		input.Limit = defaultNotificationLimit
	}
	return s.repository.List(ctx, input)
}

func (s *Service) MarkRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Notification, error) {
	if id == uuid.Nil || userID == uuid.Nil {
		return models.Notification{}, models.ErrInvalidNotificationInput
	}
	return s.repository.MarkRead(ctx, id, userID, s.now())
}

func (s *Service) MarkUnread(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Notification, error) {
	if id == uuid.Nil || userID == uuid.Nil {
		return models.Notification{}, models.ErrInvalidNotificationInput
	}
	return s.repository.MarkUnread(ctx, id, userID)
}

func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return models.ErrInvalidNotificationInput
	}
	return s.repository.MarkAllRead(ctx, userID, s.now())
}
