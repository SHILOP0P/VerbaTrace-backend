package auth

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	model "verbatrace/monolit/internal/models"
)

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	user, accessToken, refreshToken, err := h.service.Login(r.Context(), model.LoginInput{
		Email:     req.Email,
		Password:  req.Password,
		UserAgent: optionalString(r.UserAgent()),
		IPAddress: clientIPAddress(r),
	})
	if err != nil {
		if errors.Is(err, model.ErrInvalidCredentials) {
			response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidCredentials, "invalid credentials")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToLogin, "failed to login")
		return
	}
	userResponse, err := converter.UserModelToAPI(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertUser, "failed to convert user")
		return
	}

	h.setAuthCookies(w, r, accessToken, refreshToken)

	resp := dto.AuthResponse{
		User: userResponse,
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}

func clientIPAddress(r *http.Request) *string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if parsedIP := net.ParseIP(host); parsedIP != nil {
			return &host
		}
	}

	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if parsedIP := net.ParseIP(remoteAddr); parsedIP != nil {
		return &remoteAddr
	}

	return nil
}
