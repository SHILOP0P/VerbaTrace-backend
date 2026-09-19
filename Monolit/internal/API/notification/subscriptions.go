package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service/delivery"

	"github.com/google/uuid"
)

// Subscriptions reads and changes which events a person gets and where.
type Subscriptions interface {
	Subscriptions(ctx context.Context, user uuid.UUID) ([]delivery.Subscription, error)
	SetSubscriptions(ctx context.Context, user uuid.UUID, items []delivery.Subscription) ([]delivery.Subscription, error)
}

func (h *Handler) SetSubscriptions(subscriptions Subscriptions) { h.subscriptions = subscriptions }

// GetSubscriptions is GET /notification-subscriptions: the whole
// "event × channel" matrix; a channel that cannot be switched on yet comes with
// available=false.
func (h *Handler) GetSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	if h.subscriptions == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "notification subscriptions are not configured")
		return
	}
	items, err := h.subscriptions.Subscriptions(r.Context(), userID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to read notification subscriptions")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// PutSubscriptions is PUT /notification-subscriptions with [{kind, channel, enabled}].
func (h *Handler) PutSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	if h.subscriptions == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "notification subscriptions are not configured")
		return
	}
	var body struct {
		Items []delivery.Subscription `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	items, err := h.subscriptions.SetSubscriptions(r.Context(), userID, body.Items)
	switch {
	case err == nil:
		_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	case errors.Is(err, delivery.ErrInvalidSubscription):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidNotificationSubscription, "unknown event or channel")
	case errors.Is(err, delivery.ErrChannelUnavailable):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeNotificationChannelUnavailable, "Почта и Telegram появятся позже")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to save notification subscriptions")
	}
}
