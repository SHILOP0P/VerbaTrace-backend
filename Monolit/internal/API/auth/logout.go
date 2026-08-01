package auth

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	model "verbatrace/monolit/internal/models"
)

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := middleware.SessionIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	if err := h.service.Logout(r.Context(), sessionID); err != nil {
		if errors.Is(err, model.ErrRefreshSessionNotFound) {
			response.WriteError(w, http.StatusUnauthorized, response.CodeRefreshSessionNotFound, "session not found")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToLogout, "failed to logout")
		return
	}

	h.clearAuthCookies(w, r)
	response.WriteNoContent(w)
}
