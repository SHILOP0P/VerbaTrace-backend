package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/stretchr/testify/require"
)

func TestRequirePermission(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		permission models.AdminPermission
		status     int
		code       string
	}{
		{name: "missing role", permission: models.AdminPermissionPanelAccess, status: http.StatusUnauthorized, code: response.CodeUnauthorized},
		{name: "user denied", role: string(models.UserRoleUser), permission: models.AdminPermissionPanelAccess, status: http.StatusForbidden, code: response.CodeForbidden},
		{name: "helper reads users", role: string(models.UserRoleHelper), permission: models.AdminPermissionUsersRead, status: http.StatusNoContent},
		{name: "helper cannot manage subscriptions", role: string(models.UserRoleHelper), permission: models.AdminPermissionSubscriptionsManage, status: http.StatusForbidden, code: response.CodeForbidden},
		{name: "admin manages helpers", role: string(models.UserRoleAdmin), permission: models.AdminPermissionRolesManageHelpers, status: http.StatusNoContent},
		{name: "admin cannot manage admins", role: string(models.UserRoleAdmin), permission: models.AdminPermissionRolesManageAdmins, status: http.StatusForbidden, code: response.CodeForbidden},
		{name: "superadmin manages admins", role: string(models.UserRoleSuperAdmin), permission: models.AdminPermissionRolesManageAdmins, status: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := RequirePermission(tt.permission)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/capabilities", nil)
			if tt.role != "" {
				req = req.WithContext(ContextWithUserRole(req.Context(), tt.role))
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			require.Equal(t, tt.status, rec.Code)
			if tt.code != "" {
				var body response.ErrorResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				require.Equal(t, tt.code, body.Error.Code)
			}
		})
	}
}
