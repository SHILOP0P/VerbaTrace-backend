package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNotificationsListParsesUnreadPagination(t *testing.T) {
	service := &fakeNotificationService{}
	handler := NewHandler(service)
	userID := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?unread_only=true&limit=10&offset=5", nil)
	request = request.WithContext(middleware.ContextWithUserID(request.Context(), userID))

	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, models.ListNotificationsInput{UserUUID: userID, UnreadOnly: true, Limit: 10, Offset: 5}, service.lastList)
}

func TestMarkReadUsesCurrentUser(t *testing.T) {
	service := &fakeNotificationService{}
	handler := NewHandler(service)
	userID := uuid.New()
	notificationID := uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+notificationID.String()+"/read", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("uuid", notificationID.String())
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = middleware.ContextWithUserID(ctx, userID)
	request = request.WithContext(ctx)

	recorder := httptest.NewRecorder()
	handler.MarkRead(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, notificationID, service.markReadID)
	require.Equal(t, userID, service.markReadUserID)
}

func TestMarkAllReadUsesCurrentUser(t *testing.T) {
	service := &fakeNotificationService{}
	handler := NewHandler(service)
	userID := uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	request = request.WithContext(middleware.ContextWithUserID(request.Context(), userID))

	recorder := httptest.NewRecorder()
	handler.MarkAllRead(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, userID, service.markAllUserID)
}

func TestNotificationEventsStreamsCurrentUserNotifications(t *testing.T) {
	userID := uuid.New()
	notificationID := uuid.New()
	service := &fakeNotificationService{listResult: models.ListNotificationsResult{Notifications: []models.Notification{{
		ID: notificationID, UserUUID: userID, Type: models.NotificationTypeInvitation,
		Title: "Новое приглашение", Body: "Вас пригласили", CreatedAt: time.Now().UTC(),
	}}}}
	handler := NewHandler(service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/events", nil)
	ctx, cancel := context.WithCancel(middleware.ContextWithUserID(request.Context(), userID))
	cancel()
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()

	handler.Events(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), "event: notification")
	require.Contains(t, recorder.Body.String(), notificationID.String())
}

type fakeNotificationService struct {
	lastList       models.ListNotificationsInput
	markReadID     uuid.UUID
	markReadUserID uuid.UUID
	markAllUserID  uuid.UUID
	listResult     models.ListNotificationsResult
}

func (s *fakeNotificationService) Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error) {
	return models.Notification{}, nil
}

func (s *fakeNotificationService) List(ctx context.Context, input models.ListNotificationsInput) (models.ListNotificationsResult, error) {
	s.lastList = input
	return s.listResult, nil
}

func (s *fakeNotificationService) MarkRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Notification, error) {
	s.markReadID = id
	s.markReadUserID = userID
	return models.Notification{ID: id, UserUUID: userID}, nil
}

func (s *fakeNotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	s.markAllUserID = userID
	return nil
}
