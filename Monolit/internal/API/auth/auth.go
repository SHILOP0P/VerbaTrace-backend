package auth

import (
	"time"

	"verbatrace/monolit/internal/service"
)

type AuthHandler struct {
	service         service.AuthService
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewAuthHandler(service service.AuthService, accessTokenTTL time.Duration, refreshTokenTTL time.Duration) *AuthHandler {
	return &AuthHandler{
		service:         service,
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}
}
