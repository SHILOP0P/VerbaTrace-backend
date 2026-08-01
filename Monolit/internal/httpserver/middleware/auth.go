package middleware

import (
	"net/http"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/auth/token"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

const accessTokenCookieName = "access_token"

func Auth(secret string, refreshSessionRepository repository.RefreshSessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawTokens, sawMalformedAuthorization := accessTokensFromRequest(r)
			sawStaleToken := false
			if len(rawTokens) == 0 {
				response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidAuthorizationHeader, "invalid authorization header")
				return
			}

			for _, rawToken := range rawTokens {
				claims, err := token.ParseAccessToken(rawToken, secret)
				if err != nil {
					continue
				}

				if claims.SessionID == uuid.Nil {
					continue
				}

				session, err := refreshSessionRepository.GetRefreshSessionByUUID(r.Context(), claims.SessionID)
				if err != nil {
					continue
				}

				if session.UserID != claims.UserID || session.RevokedAt != nil || !session.ExpiresAt.After(time.Now().UTC()) {
					continue
				}
				if claims.AccessVersion != session.AccessVersion {
					sawStaleToken = true
					continue
				}

				ctx := ContextWithUserID(r.Context(), claims.UserID)
				ctx = ContextWithSessionID(ctx, claims.SessionID)
				ctx = ContextWithUserRole(ctx, claims.Role)
				ctx = logger.ContextWithUserID(ctx, claims.UserID.String())

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if sawStaleToken {
				response.WriteError(w, http.StatusUnauthorized, response.CodeAccessTokenStale, "access token must be refreshed")
				return
			}

			if sawMalformedAuthorization && len(rawTokens) == 0 {
				response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidAuthorizationHeader, "invalid authorization header")
				return
			}

			response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidAccessToken, "invalid access token")
		})
	}
}

func accessTokensFromRequest(r *http.Request) ([]string, bool) {
	tokens := make([]string, 0, 2)
	sawMalformedAuthorization := false

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(authHeader, bearerPrefix) {
			rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
			if rawToken != "" {
				tokens = append(tokens, rawToken)
			} else {
				sawMalformedAuthorization = true
			}
		} else {
			sawMalformedAuthorization = true
		}
	}

	if cookie, err := r.Cookie(accessTokenCookieName); err == nil {
		rawToken := strings.TrimSpace(cookie.Value)
		if rawToken != "" {
			tokens = append(tokens, rawToken)
		}
	}

	return tokens, sawMalformedAuthorization
}
