package notification

import (
	"errors"
	"net/http"
	"strconv"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	service service.NotificationService
}

func NewHandler(service service.NotificationService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input, err := parseListNotificationsInput(r, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidNotificationInput, "invalid notification input")
		return
	}

	result, err := h.service.List(r.Context(), input)
	if err != nil {
		if errors.Is(err, models.ErrInvalidNotificationInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidNotificationInput, "invalid notification input")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListNotifications, "failed to list notifications")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.NotificationsModelToAPI(result))
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	notificationID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidNotificationInput, "invalid notification input")
		return
	}

	notification, err := h.service.MarkRead(r.Context(), notificationID, userID)
	if err != nil {
		writeNotificationError(w, err, response.CodeFailedToMarkNotificationRead)
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.NotificationModelToAPI(notification))
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	if err := h.service.MarkAllRead(r.Context(), userID); err != nil {
		writeNotificationError(w, err, response.CodeFailedToMarkNotificationRead)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseListNotificationsInput(r *http.Request, userID uuid.UUID) (models.ListNotificationsInput, error) {
	query := r.URL.Query()
	input := models.ListNotificationsInput{UserUUID: userID}

	if rawUnreadOnly := query.Get("unread_only"); rawUnreadOnly != "" {
		unreadOnly, err := strconv.ParseBool(rawUnreadOnly)
		if err != nil {
			return models.ListNotificationsInput{}, models.ErrInvalidNotificationInput
		}
		input.UnreadOnly = unreadOnly
	}
	if rawLimit := query.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return models.ListNotificationsInput{}, models.ErrInvalidNotificationInput
		}
		input.Limit = limit
	}
	if rawOffset := query.Get("offset"); rawOffset != "" {
		offset, err := strconv.Atoi(rawOffset)
		if err != nil {
			return models.ListNotificationsInput{}, models.ErrInvalidNotificationInput
		}
		input.Offset = offset
	}

	return input, nil
}

func writeNotificationError(w http.ResponseWriter, err error, fallbackCode string) {
	switch {
	case errors.Is(err, models.ErrNotificationNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeNotificationNotFound, "notification not found")
	case errors.Is(err, models.ErrInvalidNotificationInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidNotificationInput, "invalid notification input")
	default:
		response.WriteError(w, http.StatusInternalServerError, fallbackCode, "failed to update notification")
	}
}
