package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminPermissionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		role        UserRole
		allowed     []AdminPermission
		denied      []AdminPermission
		permissions int
	}{
		{
			name:        "user",
			role:        UserRoleUser,
			denied:      []AdminPermission{AdminPermissionPanelAccess, AdminPermissionUsersRead, AdminPermissionUsersManage},
			permissions: 0,
		},
		{
			name: "helper",
			role: UserRoleHelper,
			allowed: []AdminPermission{
				AdminPermissionPanelAccess,
				AdminPermissionUsersRead,
				AdminPermissionCompaniesRead,
				AdminPermissionSubscriptionsRead,
			},
			denied: []AdminPermission{
				AdminPermissionUsersManage,
				AdminPermissionCompaniesManage,
				AdminPermissionRolesManageHelpers,
				AdminPermissionSessionsManage,
				AdminPermissionSubscriptionsManage,
				AdminPermissionCallsRead,
				AdminPermissionMonitoringRead,
				AdminPermissionDashboardRead,
				AdminPermissionAuditRead,
			},
			permissions: 4,
		},
		{
			name: "admin",
			role: UserRoleAdmin,
			allowed: []AdminPermission{
				AdminPermissionPanelAccess,
				AdminPermissionUsersManage,
				AdminPermissionCompaniesManage,
				AdminPermissionRolesManageHelpers,
				AdminPermissionSessionsManage,
				AdminPermissionSubscriptionsManage,
				AdminPermissionCallsRead,
				AdminPermissionMonitoringRead,
				AdminPermissionDashboardRead,
				AdminPermissionAuditRead,
				AdminPermissionActionsRead,
				AdminPermissionActionsManage,
			},
			denied:      []AdminPermission{AdminPermissionRolesManageAdmins},
			permissions: 16,
		},
		{
			name: "superadmin",
			role: UserRoleSuperAdmin,
			allowed: []AdminPermission{
				AdminPermissionPanelAccess,
				AdminPermissionUsersManage,
				AdminPermissionCompaniesManage,
				AdminPermissionRolesManageHelpers,
				AdminPermissionRolesManageAdmins,
				AdminPermissionSessionsManage,
				AdminPermissionSubscriptionsManage,
				AdminPermissionCallsRead,
				AdminPermissionMonitoringRead,
				AdminPermissionDashboardRead,
				AdminPermissionAuditRead,
				AdminPermissionActionsRead,
				AdminPermissionActionsManage,
			},
			permissions: 17,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := AdminPermissionsForRole(tt.role)
			require.Len(t, permissions, tt.permissions)

			for _, permission := range tt.allowed {
				require.True(t, HasAdminPermission(tt.role, permission), "permission %q", permission)
				require.Contains(t, permissions, permission)
			}

			for _, permission := range tt.denied {
				require.False(t, HasAdminPermission(tt.role, permission), "permission %q", permission)
				require.NotContains(t, permissions, permission)
			}
		})
	}
}

func TestAdminPermissionsForRoleReturnsCopy(t *testing.T) {
	permissions := AdminPermissionsForRole(UserRoleHelper)
	require.NotEmpty(t, permissions)
	permissions[0] = AdminPermissionAuditRead

	require.Equal(t, AdminPermissionPanelAccess, AdminPermissionsForRole(UserRoleHelper)[0])
}

func TestValidateAdminRoleTransition(t *testing.T) {
	tests := []struct {
		name      string
		actor     UserRole
		target    UserRole
		requested UserRole
		wantErr   error
	}{
		{name: "admin promotes user to helper", actor: UserRoleAdmin, target: UserRoleUser, requested: UserRoleHelper},
		{name: "admin demotes helper to user", actor: UserRoleAdmin, target: UserRoleHelper, requested: UserRoleUser},
		{name: "admin cannot assign admin", actor: UserRoleAdmin, target: UserRoleUser, requested: UserRoleAdmin, wantErr: ErrRoleTransitionForbidden},
		{name: "admin cannot target admin", actor: UserRoleAdmin, target: UserRoleAdmin, requested: UserRoleHelper, wantErr: ErrRoleTransitionForbidden},
		{name: "helper cannot change role", actor: UserRoleHelper, target: UserRoleUser, requested: UserRoleHelper, wantErr: ErrRoleTransitionForbidden},
		{name: "superadmin promotes helper to admin", actor: UserRoleSuperAdmin, target: UserRoleHelper, requested: UserRoleAdmin},
		{name: "superadmin demotes admin to user", actor: UserRoleSuperAdmin, target: UserRoleAdmin, requested: UserRoleUser},
		{name: "no-op forbidden", actor: UserRoleSuperAdmin, target: UserRoleAdmin, requested: UserRoleAdmin, wantErr: ErrRoleTransitionForbidden},
		{name: "cannot target superadmin", actor: UserRoleSuperAdmin, target: UserRoleSuperAdmin, requested: UserRoleAdmin, wantErr: ErrProtectedSuperAdmin},
		{name: "cannot set superadmin", actor: UserRoleSuperAdmin, target: UserRoleAdmin, requested: UserRoleSuperAdmin, wantErr: ErrProtectedSuperAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdminRoleTransition(tt.actor, tt.target, tt.requested)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestValidateAdminSessionTarget(t *testing.T) {
	tests := []struct {
		name    string
		actor   UserRole
		target  UserRole
		wantErr error
	}{
		{name: "admin user", actor: UserRoleAdmin, target: UserRoleUser},
		{name: "admin helper", actor: UserRoleAdmin, target: UserRoleHelper},
		{name: "admin cannot target admin", actor: UserRoleAdmin, target: UserRoleAdmin, wantErr: ErrAdminSessionManagementForbidden},
		{name: "superadmin admin", actor: UserRoleSuperAdmin, target: UserRoleAdmin},
		{name: "superadmin cannot target superadmin", actor: UserRoleSuperAdmin, target: UserRoleSuperAdmin, wantErr: ErrProtectedSuperAdmin},
		{name: "helper denied", actor: UserRoleHelper, target: UserRoleUser, wantErr: ErrAdminSessionManagementForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdminSessionTarget(tt.actor, tt.target)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
