package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/auth/token"
	"verbatrace/monolit/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestAuthAcceptsAccessTokenCookie(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "test-secret"

	rawToken, err := token.GenerateAccessTokenWithSession(userID, sessionID, string(models.UserRoleUser), secret, time.Minute, 1)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}

	repo := &authRefreshSessionRepository{
		session: models.RefreshSession{
			ID:            sessionID,
			UserID:        userID,
			AccessVersion: 1,
			ExpiresAt:     time.Now().UTC().Add(time.Hour),
		},
	}

	handler := Auth(secret, repo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID, ok := UserIDFromContext(r.Context())
		if !ok || gotUserID != userID {
			t.Fatalf("user id in context = %v, %v", gotUserID, ok)
		}

		gotSessionID, ok := SessionIDFromContext(r.Context())
		if !ok || gotSessionID != sessionID {
			t.Fatalf("session id in context = %v, %v", gotSessionID, ok)
		}

		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: rawToken, Path: "/"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestAuthFallsBackToCookieWhenAuthorizationHeaderIsMalformed(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "test-secret"

	rawToken, err := token.GenerateAccessTokenWithSession(userID, sessionID, string(models.UserRoleUser), secret, time.Minute, 1)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}

	repo := &authRefreshSessionRepository{
		session: models.RefreshSession{
			ID:            sessionID,
			UserID:        userID,
			AccessVersion: 1,
			ExpiresAt:     time.Now().UTC().Add(time.Hour),
		},
	}

	handler := Auth(secret, repo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID, ok := UserIDFromContext(r.Context())
		if !ok || gotUserID != userID {
			t.Fatalf("user id in context = %v, %v", gotUserID, ok)
		}

		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me/sessions", nil)
	req.Header.Set("Authorization", "Bearer ")
	req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: rawToken, Path: "/"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestAuthRejectsStaleAccessToken(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "test-secret"

	rawToken, err := token.GenerateAccessTokenWithSession(userID, sessionID, string(models.UserRoleAdmin), secret, time.Minute, 1)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}

	repo := &authRefreshSessionRepository{session: models.RefreshSession{
		ID:            sessionID,
		UserID:        userID,
		AccessVersion: 2,
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	}}

	handler := Auth(secret, repo)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("stale token reached protected handler")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	var body response.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != response.CodeAccessTokenStale {
		t.Fatalf("error code = %q, want %q", body.Error.Code, response.CodeAccessTokenStale)
	}
}

func TestAuthTreatsLegacyTokenWithoutAccessVersionAsStale(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "test-secret"
	legacyClaims := struct {
		UserID    uuid.UUID `json:"user_id"`
		SessionID uuid.UUID `json:"session_uuid"`
		Role      string    `json:"role"`
		jwt.RegisteredClaims
	}{
		UserID:    userID,
		SessionID: sessionID,
		Role:      string(models.UserRoleUser),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Minute)),
		},
	}
	rawToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, legacyClaims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}

	repo := &authRefreshSessionRepository{session: models.RefreshSession{
		ID:            sessionID,
		UserID:        userID,
		AccessVersion: 1,
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	}}
	handler := Auth(secret, repo)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("legacy token reached protected handler")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	var body response.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != response.CodeAccessTokenStale {
		t.Fatalf("error code = %q, want %q", body.Error.Code, response.CodeAccessTokenStale)
	}
}

type authRefreshSessionRepository struct {
	session models.RefreshSession
	err     error
}

func (r *authRefreshSessionRepository) CreateRefreshSession(ctx context.Context, session models.RefreshSession) (models.RefreshSession, error) {
	return models.RefreshSession{}, nil
}

func (r *authRefreshSessionRepository) GetRefreshSessionByHash(ctx context.Context, refreshTokenHash string) (models.RefreshSession, error) {
	return models.RefreshSession{}, nil
}

func (r *authRefreshSessionRepository) GetRefreshSessionByUUID(ctx context.Context, sessionID uuid.UUID) (models.RefreshSession, error) {
	if r.err != nil {
		return models.RefreshSession{}, r.err
	}
	if r.session.ID != sessionID {
		return models.RefreshSession{}, models.ErrRefreshSessionNotFound
	}

	return r.session, nil
}

func (r *authRefreshSessionRepository) ListActiveUserRefreshSessions(ctx context.Context, userID uuid.UUID) ([]models.RefreshSession, error) {
	return nil, nil
}

func (r *authRefreshSessionRepository) RotateRefreshSession(ctx context.Context, oldRefreshTokenHash string, newRefreshTokenHash string, expiresAt time.Time) (models.RefreshSession, error) {
	return models.RefreshSession{}, nil
}

func (r *authRefreshSessionRepository) InvalidateSessionAccess(ctx context.Context, sessionID uuid.UUID) error {
	return nil
}

func (r *authRefreshSessionRepository) InvalidateAllUserAccess(ctx context.Context, userID uuid.UUID) error {
	return nil
}

func (r *authRefreshSessionRepository) RevokeRefreshSession(ctx context.Context, sessionID uuid.UUID, reason string) error {
	return nil
}

func (r *authRefreshSessionRepository) RevokeUserRefreshSession(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, reason string) error {
	return nil
}

func (r *authRefreshSessionRepository) RevokeAllUserRefreshSessions(ctx context.Context, userID uuid.UUID, reason string) error {
	return nil
}

func (r *authRefreshSessionRepository) RevokeOtherUserRefreshSessions(ctx context.Context, userID uuid.UUID, keepSessionID uuid.UUID, reason string) error {
	return nil
}
