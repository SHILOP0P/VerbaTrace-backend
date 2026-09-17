package auth

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/config"
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
		if errors.Is(err, model.ErrTooManyAttempts) {
			response.WriteError(w, http.StatusTooManyRequests, response.CodeTooManyAttempts, "Слишком много попыток входа, попробуйте позже")
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

// clientIPAddress is what the login rate limit counts against, so it has to be
// the caller's own address.
//
// Behind a reverse proxy every request arrives from the proxy, which would turn
// a per-address limit into one shared counter: twenty strangers mistyping their
// password would lock everybody out. Forwarded headers fix that but can be set
// by anyone, so they are read only when the immediate peer is a proxy the
// deployment declared trustworthy in TRUSTED_PROXY_CIDRS. With no trusted proxy
// configured the header is ignored, which is the safe default for running the
// API directly.
func clientIPAddress(r *http.Request) *string {
	peer := remoteIP(r)
	if peer == nil {
		return nil
	}
	if !trustedProxy(*peer) {
		return peer
	}

	for _, candidate := range forwardedCandidates(r) {
		if parsed := net.ParseIP(candidate); parsed != nil && !trustedProxy(candidate) {
			return &candidate
		}
	}

	return peer
}

// trustedProxy is deliberately closed when the configuration has not been
// loaded: believing a forwarded header by default is how a rate limit becomes
// trivial to sidestep.
func trustedProxy(address string) bool {
	settings := config.AppConfig()
	if settings == nil || settings.HTTPConfig == nil {
		return false
	}

	return settings.HTTPConfig.IsTrustedProxy(address)
}

// forwardedCandidates walks X-Forwarded-For from the closest hop outwards, which
// is the order in which trust decreases.
func forwardedCandidates(r *http.Request) []string {
	raw := r.Header.Get("X-Forwarded-For")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	candidates := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		if value := strings.TrimSpace(parts[i]); value != "" {
			candidates = append(candidates, value)
		}
	}

	return candidates
}

func remoteIP(r *http.Request) *string {
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
