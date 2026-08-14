package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetCapabilities(t *testing.T) {
	service := NewService(nil)

	tests := []struct {
		name        string
		role        models.UserRole
		permissions int
		wantErr     error
	}{
		{name: "user denied", role: models.UserRoleUser, wantErr: models.ErrForbidden},
		{name: "helper", role: models.UserRoleHelper, permissions: 4},
		{name: "admin", role: models.UserRoleAdmin, permissions: 16},
		{name: "superadmin", role: models.UserRoleSuperAdmin, permissions: 17},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capabilities, err := service.GetCapabilities(context.Background(), tt.role)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.role, capabilities.Role)
			require.Len(t, capabilities.Permissions, tt.permissions)
		})
	}
}

func TestRecordAuditValidatesAndPersistsSanitizedEntry(t *testing.T) {
	repository := &auditRepositoryStub{}
	service := NewService(repository)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	actorID := uuid.New()
	targetID := uuid.New()
	reason := "  manual incident response  "
	requestID := "  request-123  "
	ipAddress := " 127.0.0.1 "
	userAgent := " Chrome "

	created, err := service.RecordAudit(context.Background(), models.CreateAdminAuditLogInput{
		ActorUserUUID: actorID,
		ActorRole:     models.UserRoleAdmin,
		Action:        "  session.revoked  ",
		TargetType:    "  refresh_session  ",
		TargetUUID:    uuid.NullUUID{UUID: targetID, Valid: true},
		BeforeData:    json.RawMessage(`{"revoked":false}`),
		AfterData:     json.RawMessage(`{"revoked":true}`),
		Reason:        &reason,
		RequestID:     &requestID,
		IPAddress:     &ipAddress,
		UserAgent:     &userAgent,
	})

	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)
	require.Equal(t, actorID, created.ActorUserUUID)
	require.Equal(t, "session.revoked", created.Action)
	require.Equal(t, "refresh_session", created.TargetType)
	require.Equal(t, targetID, created.TargetUUID.UUID)
	require.Equal(t, now, created.CreatedAt)
	require.Equal(t, "manual incident response", *created.Reason)
	require.Equal(t, "request-123", *created.RequestID)
	require.Equal(t, "127.0.0.1", *created.IPAddress)
	require.Equal(t, "Chrome", *created.UserAgent)
	require.Equal(t, created, repository.created)
}

func TestRecordAuditRejectsInvalidInput(t *testing.T) {
	service := NewService(&auditRepositoryStub{})
	validActor := uuid.New()

	tests := []struct {
		name  string
		input models.CreateAdminAuditLogInput
	}{
		{name: "empty actor", input: models.CreateAdminAuditLogInput{ActorRole: models.UserRoleAdmin, Action: "x", TargetType: "user"}},
		{name: "user actor", input: models.CreateAdminAuditLogInput{ActorUserUUID: validActor, ActorRole: models.UserRoleUser, Action: "x", TargetType: "user"}},
		{name: "empty action", input: models.CreateAdminAuditLogInput{ActorUserUUID: validActor, ActorRole: models.UserRoleAdmin, TargetType: "user"}},
		{name: "empty target type", input: models.CreateAdminAuditLogInput{ActorUserUUID: validActor, ActorRole: models.UserRoleAdmin, Action: "x"}},
		{name: "array before data", input: models.CreateAdminAuditLogInput{ActorUserUUID: validActor, ActorRole: models.UserRoleAdmin, Action: "x", TargetType: "user", BeforeData: json.RawMessage(`[]`)}},
		{name: "invalid ip", input: models.CreateAdminAuditLogInput{ActorUserUUID: validActor, ActorRole: models.UserRoleAdmin, Action: "x", TargetType: "user", IPAddress: stringPointer("invalid")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.RecordAudit(context.Background(), tt.input)
			require.ErrorIs(t, err, models.ErrInvalidAdminInput)
		})
	}
}

func TestRecordAuditRequiresRepository(t *testing.T) {
	service := NewService(nil)

	_, err := service.RecordAudit(context.Background(), models.CreateAdminAuditLogInput{
		ActorUserUUID: uuid.New(),
		ActorRole:     models.UserRoleAdmin,
		Action:        "user.role_changed",
		TargetType:    "user",
	})

	require.True(t, errors.Is(err, errAuditRepositoryNotConfigured))
}

type auditRepositoryStub struct {
	created models.AdminAuditLog
	err     error
}

func (r *auditRepositoryStub) ResetAdminUsage(context.Context, models.ResetAdminUsageInput) error {
	return nil
}

func (r *auditRepositoryStub) ListAdminUsers(context.Context, models.ListAdminUsersInput) (models.ListAdminUsersResult, error) {
	return models.ListAdminUsersResult{}, r.err
}
func (r *auditRepositoryStub) GetAdminUserByUUID(context.Context, uuid.UUID) (models.AdminUser, error) {
	return models.AdminUser{}, r.err
}
func (r *auditRepositoryStub) UpdateAdminUserProfile(context.Context, models.UpdateAdminUserProfileInput) (models.AdminUser, error) {
	return models.AdminUser{}, r.err
}
func (r *auditRepositoryStub) ChangeAdminUserRole(context.Context, models.ChangeAdminUserRoleInput) (models.AdminUser, error) {
	return models.AdminUser{}, r.err
}
func (r *auditRepositoryStub) ListAdminUserSessions(context.Context, uuid.UUID) ([]models.AdminUserSession, error) {
	return nil, r.err
}
func (r *auditRepositoryStub) RevokeAdminUserSession(context.Context, models.AdminSessionMutationInput) error {
	return r.err
}
func (r *auditRepositoryStub) RevokeAllAdminUserSessions(context.Context, models.AdminSessionMutationInput) error {
	return r.err
}
func (r *auditRepositoryStub) ListAdminCompanies(context.Context, models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error) {
	return models.ListAdminCompaniesResult{}, r.err
}
func (r *auditRepositoryStub) GetAdminCompanyByUUID(context.Context, uuid.UUID) (models.AdminCompany, error) {
	return models.AdminCompany{}, r.err
}
func (r *auditRepositoryStub) GetAdminPersonalSubscription(context.Context, uuid.UUID) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, r.err
}
func (r *auditRepositoryStub) GetAdminCompanySubscription(context.Context, uuid.UUID) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, r.err
}
func (r *auditRepositoryStub) GrantAdminSubscription(context.Context, models.GrantAdminSubscriptionInput) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, r.err
}
func (r *auditRepositoryStub) CancelAdminSubscription(context.Context, models.CancelAdminSubscriptionInput) (models.AdminSubscription, error) {
	return models.AdminSubscription{}, r.err
}

func (r *auditRepositoryStub) CreateAdminAuditLog(_ context.Context, audit models.AdminAuditLog) (models.AdminAuditLog, error) {
	if r.err != nil {
		return models.AdminAuditLog{}, r.err
	}
	r.created = audit
	return audit, nil
}

func stringPointer(value string) *string {
	return &value
}
