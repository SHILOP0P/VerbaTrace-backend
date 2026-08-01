package auth

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
)

func (h *AuthHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.LogoutAll(r.Context(), userID, sessionID); err != nil {
		if errors.Is(err, models.ErrRefreshSessionNotFound) {
			h.clearAuthCookies(w, r)
			response.WriteError(w, http.StatusUnauthorized, response.CodeRefreshSessionNotFound, "session not found")
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
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToLogoutAll, "failed to logout all")
		return
	}

	h.clearAuthCookies(w, r)
	response.WriteNoContent(w)
}
