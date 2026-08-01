package auth

import (
	"errors"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	model "verbatrace/monolit/internal/models"
)

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := refreshTokenFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	user, accessToken, newRefreshToken, err := h.service.Refresh(r.Context(), model.RefreshTokenInput{
		RefreshToken: refreshToken,
	})
	if err != nil {
		if errors.Is(err, model.ErrRefreshRotationConflict) {
			response.WriteError(w, http.StatusConflict, response.CodeRefreshRotationConflict, "refresh already completed")
			return
		}

		if errors.Is(err, model.ErrInvalidRefreshToken) {
			h.clearAuthCookies(w, r)
			response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidRefreshToken, "invalid refresh token")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToRefreshToken, "failed to refresh token")
		return
	}

	userResponse, err := converter.UserModelToAPI(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertUser, "failed to convert user")
		return
	}

	h.setAuthCookies(w, r, accessToken, newRefreshToken)

	resp := dto.AuthResponse{
		User: userResponse,
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func refreshTokenFromRequest(r *http.Request) (string, error) {
	if cookie, err := r.Cookie(refreshTokenCookieName); err == nil {
		return strings.TrimSpace(cookie.Value), nil
	}
	return "", nil
}
