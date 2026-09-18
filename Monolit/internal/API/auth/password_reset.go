package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/passwordreset"
)

// PasswordReset is the forgotten-password flow.
type PasswordReset interface {
	Enabled() bool
	Request(ctx context.Context, email, ip string)
	Confirm(ctx context.Context, token, newPassword string) error
}

func (h *AuthHandler) SetPasswordReset(reset PasswordReset) { h.reset = reset }

// Capabilities is GET /auth/capabilities: what the sign-in page may offer. The
// reset link is hidden while letters only reach the mock sender.
func (h *AuthHandler) Capabilities(w http.ResponseWriter, _ *http.Request) {
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"password_reset_enabled": h.reset != nil && h.reset.Enabled()})
}

// RequestPasswordReset is POST /auth/password-reset/request {email}: always 202,
// whether the address has an account or not.
func (h *AuthHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	if h.reset != nil {
		ip := ""
		if address := clientIPAddress(r); address != nil {
			ip = *address
		}
		h.reset.Request(r.Context(), body.Email, ip)
	}
	w.WriteHeader(http.StatusAccepted)
}

// ConfirmPasswordReset is POST /auth/password-reset/confirm {token, new_password}.
func (h *AuthHandler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	if h.reset == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "password reset is not configured")
		return
	}
	var body struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	err := h.reset.Confirm(r.Context(), body.Token, body.NewPassword)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, passwordreset.ErrInvalidToken):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidPasswordResetToken, "Ссылка устарела или уже использована. Запросите новую")
	case errors.Is(err, models.ErrInvalidUserInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidUserInput, "Пароль должен быть не короче 8 символов")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to reset password")
	}
}
