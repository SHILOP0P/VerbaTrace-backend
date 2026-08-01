package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetCapabilities(t *testing.T) {
	adminService := &adminServiceStub{
		capabilities: models.AdminCapabilities{
			Role: models.UserRoleHelper,
			Permissions: []models.AdminPermission{
				models.AdminPermissionPanelAccess,
				models.AdminPermissionUsersRead,
			},
		},
	}
	handler := NewHandler(adminService)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/capabilities", nil)
	req = req.WithContext(middleware.ContextWithUserRole(req.Context(), string(models.UserRoleHelper)))
	rec := httptest.NewRecorder()

	handler.GetCapabilities(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body dto.AdminCapabilitiesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, string(models.UserRoleHelper), body.Role)
	require.Equal(t, []string{"admin.panel.access", "admin.users.read"}, body.Permissions)
	require.Equal(t, models.UserRoleHelper, adminService.role)
}

func TestGetCapabilitiesRequiresRole(t *testing.T) {
	handler := NewHandler(&adminServiceStub{})
	rec := httptest.NewRecorder()

	handler.GetCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/capabilities", nil))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	requireErrorCode(t, rec, response.CodeUnauthorized)
}

func TestGetCapabilitiesMapsErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "forbidden", err: models.ErrForbidden, status: http.StatusForbidden, code: response.CodeForbidden},
		{name: "internal", err: errors.New("failed"), status: http.StatusInternalServerError, code: response.CodeFailedToGetAdminCapabilities},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(&adminServiceStub{err: tt.err})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/capabilities", nil)
			req = req.WithContext(middleware.ContextWithUserRole(req.Context(), string(models.UserRoleUser)))
			rec := httptest.NewRecorder()

			handler.GetCapabilities(rec, req)

			require.Equal(t, tt.status, rec.Code)
			requireErrorCode(t, rec, tt.code)
		})
	}
}

type adminServiceStub struct {
	capabilities models.AdminCapabilities
	role         models.UserRole
	err          error
}

func (s *adminServiceStub) ResetUsage(context.Context, models.ResetAdminUsageInput) error { return nil }

func (s *adminServiceStub) GetCapabilities(_ context.Context, role models.UserRole) (models.AdminCapabilities, error) {
	s.role = role
	return s.capabilities, s.err
}

func (s *adminServiceStub) RecordAudit(_ context.Context, _ models.CreateAdminAuditLogInput) (models.AdminAuditLog, error) {
	return models.AdminAuditLog{}, nil
}
func (s *adminServiceStub) ListUsers(context.Context, models.ListAdminUsersInput) (models.ListAdminUsersResult, error) {
	return models.ListAdminUsersResult{}, s.err
}
func (s *adminServiceStub) GetUser(context.Context, uuid.UUID) (models.AdminUser, error) {
	return models.AdminUser{}, s.err
}
func (s *adminServiceStub) ListUserCalls(context.Context, uuid.UUID, int, int) (models.ListCallsResult, error) {
	return models.ListCallsResult{}, s.err
}
func (s *adminServiceStub) UpdateUserProfile(context.Context, models.UpdateAdminUserProfileInput) (models.AdminUser, error) {
	return models.AdminUser{}, s.err
}
func (s *adminServiceStub) ChangeUserRole(context.Context, models.ChangeAdminUserRoleInput) (models.AdminUser, error) {
	return models.AdminUser{}, s.err
}
func (s *adminServiceStub) ListUserSessions(context.Context, uuid.UUID, uuid.UUID) ([]models.AdminUserSession, error) {
	return nil, s.err
}
func (s *adminServiceStub) RevokeUserSession(context.Context, models.AdminSessionMutationInput) error {
	return s.err
}
func (s *adminServiceStub) RevokeAllUserSessions(context.Context, models.AdminSessionMutationInput) error {
	return s.err
}
func (s *adminServiceStub) ListCompanies(context.Context, models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error) {
	return models.ListAdminCompaniesResult{}, s.err
}
func (s *adminServiceStub) GetCompany(context.Context, uuid.UUID) (models.AdminCompany, error) {
	return models.AdminCompany{}, s.err
}
func (s *adminServiceStub) GetPersonalSubscription(context.Context, uuid.UUID) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, s.err
}
func (s *adminServiceStub) GetCompanySubscription(context.Context, uuid.UUID) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, s.err
}
func (s *adminServiceStub) GrantSubscription(context.Context, models.GrantAdminSubscriptionInput) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, s.err
}
func (s *adminServiceStub) CancelSubscription(context.Context, models.CancelAdminSubscriptionInput) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, s.err
}
func (s *adminServiceStub) GetCall(context.Context, uuid.UUID) (models.Call, error) {
	return models.Call{}, s.err
}
func (s *adminServiceStub) GetCallAudio(context.Context, uuid.UUID) (models.File, error) {
	return models.File{}, s.err
}

func requireErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body response.ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, code, body.Error.Code)
}
