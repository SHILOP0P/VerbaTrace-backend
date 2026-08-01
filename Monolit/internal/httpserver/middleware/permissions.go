package middleware

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"
)

func RequirePermission(permission models.AdminPermission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := UserRoleFromContext(r.Context())
			if !ok {
				response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
				return
			}

			if !models.HasAdminPermission(models.UserRole(role), permission) {
				response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
