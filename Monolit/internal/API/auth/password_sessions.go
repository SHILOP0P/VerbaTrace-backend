package auth

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *AuthHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	sessionID, ok := middleware.SessionIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	result, err := h.service.UpdatePassword(r.Context(), models.UpdatePasswordInput{
		UserUUID:        userID,
		SessionUUID:     sessionID,
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	})
	if err != nil {
		writePasswordSessionError(w, err, response.CodeInternalServerError, "failed to update password")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, dto.UpdatePasswordResponse{
		UpdatedAt: result.UpdatedAt.Format(time.RFC3339),
	})
}

func (h *AuthHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	sessionID, ok := middleware.SessionIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	sessions, err := h.service.ListSessions(r.Context(), userID, sessionID)
	if err != nil {
		writePasswordSessionError(w, err, response.CodeInternalServerError, "failed to list sessions")
		return
	}

	resp := dto.UserSessionsResponse{Sessions: make([]dto.UserSessionResponse, 0, len(sessions))}
	availabilityService, ok := h.service.(interface {
		OtherSessionManagementAvailableAt(context.Context, uuid.UUID, uuid.UUID) (time.Time, error)
	})
	if ok {
		if availableAt, availabilityErr := availabilityService.OtherSessionManagementAvailableAt(r.Context(), userID, sessionID); availabilityErr == nil {
			resp.CanManageOtherSessions = !time.Now().UTC().Before(availableAt)
			if !resp.CanManageOtherSessions {
				formatted := availableAt.Format(time.RFC3339)
				resp.AvailableAt = &formatted
				resp.RetryAfterSeconds = max(0, int(math.Ceil(time.Until(availableAt).Seconds())))
			}
		}
	}
	for _, session := range sessions {
		var lastSeenAt *string
		if session.LastSeenAt != nil {
			formatted := session.LastSeenAt.Format(time.RFC3339)
			lastSeenAt = &formatted
		}

		resp.Sessions = append(resp.Sessions, dto.UserSessionResponse{
			ID:         session.ID.String(),
			Current:    session.Current,
			UserAgent:  session.UserAgent,
			IP:         session.IPAddress,
			CreatedAt:  session.CreatedAt.Format(time.RFC3339),
			LastSeenAt: lastSeenAt,
		})
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	currentSessionID, ok := middleware.SessionIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	sessionID, err := uuid.Parse(chi.URLParam(r, "session_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeRefreshSessionNotFound, "invalid session uuid")
		return
	}

	if err := h.service.RevokeSession(r.Context(), userID, currentSessionID, sessionID); err != nil {
		writePasswordSessionError(w, err, response.CodeFailedToLogout, "failed to delete session")
		return
	}

	if sessionID == currentSessionID {
		h.clearAuthCookies(w, r)
	}
	response.WriteNoContent(w)
}

func writePasswordSessionError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidUserInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidUserInput, "invalid user input")
		return
	}
	if errors.Is(err, models.ErrInvalidCredentials) {
		response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidCredentials, "invalid credentials")
		return
	}
	if errors.Is(err, models.ErrUserNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
		return
	}
	if errors.Is(err, models.ErrRefreshSessionNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeRefreshSessionNotFound, "session not found")
		return
	}
	var trustErr models.SessionTrustError
	if errors.As(err, &trustErr) {
		retryAfter := max(0, int(math.Ceil(time.Until(trustErr.AvailableAt).Seconds())))
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		response.WriteErrorWithDetails(w, http.StatusForbidden, response.CodeSessionTrustAgeRequired, "management of other sessions is not available yet", map[string]any{
			"available_at":        trustErr.AvailableAt.Format(time.RFC3339),
			"retry_after_seconds": retryAfter,
		})
		return
	}
	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
