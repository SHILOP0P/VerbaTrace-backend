package notification

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestListValidationAndReadUsesCurrentUser(t *testing.T) {
	repo := &fakeNotificationRepository{}
	svc := NewService(repo)
	userID := uuid.New()

	_, err := svc.List(context.Background(), models.ListNotificationsInput{UserUUID: userID, UnreadOnly: true, Limit: 2, Offset: 3})
	require.NoError(t, err)
	require.Equal(t, models.ListNotificationsInput{UserUUID: userID, UnreadOnly: true, Limit: 2, Offset: 3}, repo.lastList)

	_, err = svc.List(context.Background(), models.ListNotificationsInput{UserUUID: userID, Limit: 101})
	require.ErrorIs(t, err, models.ErrInvalidNotificationInput)

	notificationID := uuid.New()
	_, err = svc.MarkRead(context.Background(), notificationID, userID)
	require.NoError(t, err)
	require.Equal(t, notificationID, repo.markReadID)
	require.Equal(t, userID, repo.markReadUserID)

	require.NoError(t, svc.MarkAllRead(context.Background(), userID))
	require.Equal(t, userID, repo.markAllUserID)
}

type fakeNotificationRepository struct {
	lastList       models.ListNotificationsInput
	markReadID     uuid.UUID
	markReadUserID uuid.UUID
	markAllUserID  uuid.UUID
}

func (r *fakeNotificationRepository) Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error) {
	return models.Notification{}, nil
}

func (r *fakeNotificationRepository) List(ctx context.Context, input models.ListNotificationsInput) (models.ListNotificationsResult, error) {
	r.lastList = input
	return models.ListNotificationsResult{}, nil
}

func (r *fakeNotificationRepository) MarkRead(ctx context.Context, id uuid.UUID, userID uuid.UUID, readAt time.Time) (models.Notification, error) {
	r.markReadID = id
	r.markReadUserID = userID
	return models.Notification{ID: id, UserUUID: userID, ReadAt: &readAt}, nil
}

func (r *fakeNotificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID, readAt time.Time) error {
	r.markAllUserID = userID
	return nil
}
