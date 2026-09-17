package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AdminPermission string

const (
	AdminPermissionPanelAccess         AdminPermission = "admin.panel.access"
	AdminPermissionUsersRead           AdminPermission = "admin.users.read"
	AdminPermissionUsersManage         AdminPermission = "admin.users.manage"
	AdminPermissionCompaniesRead       AdminPermission = "admin.companies.read"
	AdminPermissionCompaniesManage     AdminPermission = "admin.companies.manage"
	AdminPermissionRolesManageHelpers  AdminPermission = "admin.roles.manage_helpers"
	AdminPermissionRolesManageAdmins   AdminPermission = "admin.roles.manage_admins"
	AdminPermissionSessionsRead        AdminPermission = "admin.sessions.read"
	AdminPermissionSessionsManage      AdminPermission = "admin.sessions.manage"
	AdminPermissionSubscriptionsRead   AdminPermission = "admin.subscriptions.read"
	AdminPermissionSubscriptionsManage AdminPermission = "admin.subscriptions.manage"
	AdminPermissionCallsRead           AdminPermission = "admin.calls.read"
	AdminPermissionMonitoringRead      AdminPermission = "admin.monitoring.read"
	AdminPermissionDashboardRead       AdminPermission = "admin.dashboard.read"
	AdminPermissionAuditRead           AdminPermission = "admin.audit.read"
	AdminPermissionActionsRead         AdminPermission = "admin.actions.read"
	AdminPermissionActionsManage       AdminPermission = "admin.actions.manage"
)

var adminPermissionsByRole = map[UserRole][]AdminPermission{
	UserRoleHelper: {
		AdminPermissionPanelAccess,
		AdminPermissionUsersRead,
		AdminPermissionCompaniesRead,
		AdminPermissionSubscriptionsRead,
	},
	UserRoleAdmin: {
		AdminPermissionPanelAccess,
		AdminPermissionUsersRead,
		AdminPermissionUsersManage,
		AdminPermissionCompaniesRead,
		AdminPermissionCompaniesManage,
		AdminPermissionRolesManageHelpers,
		AdminPermissionSessionsRead,
		AdminPermissionSessionsManage,
		AdminPermissionSubscriptionsRead,
		AdminPermissionSubscriptionsManage,
		AdminPermissionCallsRead,
		AdminPermissionMonitoringRead,
		AdminPermissionDashboardRead,
		AdminPermissionAuditRead,
		AdminPermissionActionsRead,
		AdminPermissionActionsManage,
	},
	UserRoleSuperAdmin: {
		AdminPermissionPanelAccess,
		AdminPermissionUsersRead,
		AdminPermissionUsersManage,
		AdminPermissionCompaniesRead,
		AdminPermissionCompaniesManage,
		AdminPermissionRolesManageHelpers,
		AdminPermissionRolesManageAdmins,
		AdminPermissionSessionsRead,
		AdminPermissionSessionsManage,
		AdminPermissionSubscriptionsRead,
		AdminPermissionSubscriptionsManage,
		AdminPermissionCallsRead,
		AdminPermissionMonitoringRead,
		AdminPermissionDashboardRead,
		AdminPermissionAuditRead,
		AdminPermissionActionsRead,
		AdminPermissionActionsManage,
	},
}

type AdminCapabilities struct {
	Role        UserRole
	Permissions []AdminPermission
}

func AdminPermissionsForRole(role UserRole) []AdminPermission {
	permissions := adminPermissionsByRole[role]
	return append([]AdminPermission(nil), permissions...)
}

func HasAdminPermission(role UserRole, permission AdminPermission) bool {
	for _, candidate := range adminPermissionsByRole[role] {
		if candidate == permission {
			return true
		}
	}

	return false
}

type AdminAuditLog struct {
	ID            uuid.UUID
	ActorUserUUID uuid.UUID
	ActorRole     UserRole
	Action        string
	TargetType    string
	TargetUUID    uuid.NullUUID
	BeforeData    json.RawMessage
	AfterData     json.RawMessage
	Reason        *string
	RequestID     *string
	IPAddress     *string
	UserAgent     *string
	CreatedAt     time.Time
}

type CreateAdminAuditLogInput struct {
	ActorUserUUID uuid.UUID
	ActorRole     UserRole
	Action        string
	TargetType    string
	TargetUUID    uuid.NullUUID
	BeforeData    json.RawMessage
	AfterData     json.RawMessage
	Reason        *string
	RequestID     *string
	IPAddress     *string
	UserAgent     *string
}

type AdminSubscriptionStatusFilter string

const (
	AdminSubscriptionStatusActive   AdminSubscriptionStatusFilter = "active"
	AdminSubscriptionStatusCanceled AdminSubscriptionStatusFilter = "canceled"
	AdminSubscriptionStatusExpired  AdminSubscriptionStatusFilter = "expired"
	AdminSubscriptionStatusNone     AdminSubscriptionStatusFilter = "none"
)

type AdminUser struct {
	ID          uuid.UUID
	Email       string
	FullName    string
	FullSurname string
	Username    string
	Role        UserRole
	Post        *string
	Phone       *string
	Timezone    *string
	CreatedAt   time.Time
}

type ListAdminUsersInput struct {
	Query              string
	Role               *UserRole
	SubscriptionStatus *AdminSubscriptionStatusFilter
	PlanCode           *PlanCode
	CreatedFrom        *time.Time
	CreatedTo          *time.Time
	Limit              int
	Offset             int
	VisibleUserUUIDs   []uuid.UUID
}

type ListAdminUsersResult struct {
	Users  []AdminUser
	Total  int
	Limit  int
	Offset int
}

type AdminMutationMetadata struct {
	Reason    string
	RequestID *string
	IPAddress *string
	UserAgent *string
}

type ChangeAdminUserRoleInput struct {
	ActorUserUUID  uuid.UUID
	TargetUserUUID uuid.UUID
	ExpectedRole   UserRole
	Role           UserRole
	Metadata       AdminMutationMetadata
}

type UpdateAdminUserProfileInput struct {
	ActorUserUUID  uuid.UUID
	TargetUserUUID uuid.UUID
	FullName       *string
	FullSurname    *string
	Username       *string
	Post           *string
	Phone          *string
	Timezone       *string
	Metadata       AdminMutationMetadata
}

type AdminUserSession struct {
	ID         uuid.UUID
	UserAgent  *string
	IPAddress  *string
	CreatedAt  time.Time
	LastSeenAt *time.Time
	ExpiresAt  time.Time
}

type AdminUserSessions struct {
	UserUUID uuid.UUID
	Sessions []AdminUserSession
}

type AdminCompany struct {
	ID              uuid.UUID
	Name            string
	Tag             string
	ManagerUserUUID uuid.UUID
	CreatedAt       time.Time
}
type ListAdminCompaniesInput struct {
	Query               string
	Limit               int
	Offset              int
	VisibleCompanyUUIDs []uuid.UUID
}
type ListAdminCompaniesResult struct {
	Companies []AdminCompany
	Total     int
	Limit     int
	Offset    int
}

// AdminRestorableCompany is a company inside the soft-deletion window that the
// superadmin may still pull back. It cannot appear in the ordinary company list,
// which filters deleted rows — that is exactly what these are — so without a
// list of its own the one-time rescue could not be reached at all.
type AdminRestorableCompany struct {
	ID              uuid.UUID
	Name            string
	Tag             string
	ManagerUserUUID uuid.UUID
	SoftDeletedAt   time.Time
	// PurgeAfter is the deadline. Once it passes the company is erased and this
	// row disappears, so it is the only number that matters on the screen.
	PurgeAfter *time.Time
}
type AdminSubscription struct {
	ID          uuid.UUID
	PlanCode    PlanCode
	Type        PlanType
	Status      SubscriptionStatus
	UserUUID    uuid.NullUUID
	CompanyUUID uuid.NullUUID
	StartsAt    time.Time
	EndsAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type GrantAdminSubscriptionInput struct {
	ActorUserUUID uuid.UUID
	UserUUID      uuid.UUID
	CompanyUUID   uuid.UUID
	PlanCode      PlanCode
	StartsAt      time.Time
	EndsAt        time.Time
	// ActiveCompanyUUIDs is the answer to "which companies keep working" when a
	// business plan no longer covers all of them. The choice is made before the
	// plan changes, so nobody finds their companies frozen without being asked.
	ActiveCompanyUUIDs []uuid.UUID
	Metadata           AdminMutationMetadata
}

// UpdateAdminCompanyTagInput is an administrator changing a customer's company
// tag. The reason is not optional: this is somebody else's data.
type UpdateAdminCompanyTagInput struct {
	ActorUserUUID uuid.UUID
	CompanyUUID   uuid.UUID
	Tag           string
	Metadata      AdminMutationMetadata
}

// CompanySelectionRequired carries what the interface needs to ask which
// companies stay active: the owner's companies and how many the new plan covers.
type CompanySelectionRequired struct {
	OwnerUserUUID uuid.UUID
	CompanyUUIDs  []uuid.UUID
	CompanyLimit  int
}

func (e *CompanySelectionRequired) Error() string {
	return ErrCompanySelectionRequired.Error()
}

func (e *CompanySelectionRequired) Unwrap() error {
	return ErrCompanySelectionRequired
}

type CancelAdminSubscriptionInput struct {
	ActorUserUUID uuid.UUID
	UserUUID      uuid.UUID
	CompanyUUID   uuid.UUID
	Metadata      AdminMutationMetadata
}

type ResetAdminUsageInput struct {
	ActorUserUUID uuid.UUID
	UserUUID      uuid.UUID
	CompanyUUID   uuid.UUID
	Metadata      AdminMutationMetadata
}

type AdminSessionMutationInput struct {
	ActorUserUUID  uuid.UUID
	TargetUserUUID uuid.UUID
	SessionUUID    uuid.UUID
	Metadata       AdminMutationMetadata
}

func IsValidUserRole(role UserRole) bool {
	switch role {
	case UserRoleUser, UserRoleHelper, UserRoleAdmin, UserRoleSuperAdmin:
		return true
	default:
		return false
	}
}

func ValidateAdminRoleTransition(actorRole UserRole, targetRole UserRole, requestedRole UserRole) error {
	if targetRole == UserRoleSuperAdmin || requestedRole == UserRoleSuperAdmin {
		return ErrProtectedSuperAdmin
	}
	if targetRole == requestedRole {
		return ErrRoleTransitionForbidden
	}

	switch actorRole {
	case UserRoleAdmin:
		if (targetRole == UserRoleUser && requestedRole == UserRoleHelper) ||
			(targetRole == UserRoleHelper && requestedRole == UserRoleUser) {
			return nil
		}
	case UserRoleSuperAdmin:
		if (targetRole == UserRoleUser || targetRole == UserRoleHelper || targetRole == UserRoleAdmin) &&
			(requestedRole == UserRoleUser || requestedRole == UserRoleHelper || requestedRole == UserRoleAdmin) {
			return nil
		}
	}

	return ErrRoleTransitionForbidden
}

func ValidateAdminSessionTarget(actorRole UserRole, targetRole UserRole) error {
	if targetRole == UserRoleSuperAdmin {
		return ErrProtectedSuperAdmin
	}

	switch actorRole {
	case UserRoleAdmin:
		if targetRole == UserRoleUser || targetRole == UserRoleHelper {
			return nil
		}
	case UserRoleSuperAdmin:
		if targetRole == UserRoleUser || targetRole == UserRoleHelper || targetRole == UserRoleAdmin {
			return nil
		}
	}

	return ErrAdminSessionManagementForbidden
}
