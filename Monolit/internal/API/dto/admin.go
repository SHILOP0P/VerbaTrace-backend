package dto

type AdminCapabilitiesResponse struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type AdminUserResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	FullName    string  `json:"full_name"`
	FullSurname string  `json:"full_surname"`
	Username    string  `json:"username"`
	Role        string  `json:"role"`
	Headline    *string `json:"headline,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	CreatedAt   string  `json:"created_at"`
}
type AdminUsersResponse struct {
	Items  []AdminUserResponse `json:"items"`
	Total  int                 `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}
type ChangeAdminUserRoleRequest struct {
	Role         string `json:"role"`
	ExpectedRole string `json:"expected_role"`
	Reason       string `json:"reason"`
}
type UpdateAdminUserProfileRequest struct {
	FullName    *string `json:"full_name"`
	FullSurname *string `json:"full_surname"`
	Username    *string `json:"username"`
	Headline    *string `json:"headline"`
	Reason      string  `json:"reason"`
}
type AdminSessionResponse struct {
	ID         string  `json:"id"`
	UserAgent  *string `json:"user_agent,omitempty"`
	IP         *string `json:"ip,omitempty"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at,omitempty"`
	ExpiresAt  string  `json:"expires_at"`
}
type AdminSessionsResponse struct {
	UserUUID string                 `json:"user_uuid"`
	Sessions []AdminSessionResponse `json:"sessions"`
}
type AdminReasonRequest struct {
	Reason string `json:"reason"`
}
type AdminCompanyResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Tag             string `json:"tag"`
	ManagerUserUUID string `json:"manager_user_uuid"`
	CreatedAt       string `json:"created_at"`
	// The panel decides from these which actions on the card are still worth
	// offering: a restore applies to a company being deleted, once.
	LifecycleState string `json:"lifecycle_state"`
	FreezeReason   string `json:"freeze_reason,omitempty"`
	RestoreUsed    bool   `json:"restore_used"`
}

// UpdateAdminCompanyTagRequest is an administrator changing a customer's company
// tag. The reason is required, because the change lands in the audit trail the
// customer can read.
type UpdateAdminCompanyTagRequest struct {
	Tag    string `json:"tag"`
	Reason string `json:"reason"`
}

// RestoreAdminCompanyRequest is the superadmin's one-time rescue of a company
// that is being deleted.
type RestoreAdminCompanyRequest struct {
	Reason string `json:"reason"`
}

// AdminCompanyLifecycleResponse says where a company stands after the rescue:
// frozen again, and unable to be rescued a second time.
type AdminCompanyLifecycleResponse struct {
	CompanyUUID  string `json:"company_uuid"`
	State        string `json:"state"`
	FreezeReason string `json:"freeze_reason"`
	RestoreUsed  bool   `json:"restore_used"`
}

type AdminCompaniesResponse struct {
	Items  []AdminCompanyResponse `json:"items"`
	Total  int                    `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

// AdminRestorableCompanyResponse is one company waiting to be erased. The
// deadline is what the superadmin decides by, so it comes with the row rather
// than being looked up per company.
type AdminRestorableCompanyResponse struct {
	CompanyUUID     string  `json:"company_uuid"`
	Name            string  `json:"name"`
	Tag             string  `json:"tag"`
	ManagerUserUUID string  `json:"manager_user_uuid"`
	SoftDeletedAt   string  `json:"soft_deleted_at"`
	PurgeAfter      *string `json:"purge_after"`
}

type AdminRestorableCompaniesResponse struct {
	Items []AdminRestorableCompanyResponse `json:"items"`
}
type AdminSubscriptionResponse struct {
	ID          string  `json:"id"`
	PlanCode    string  `json:"plan_code"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	UserUUID    *string `json:"user_uuid"`
	CompanyUUID *string `json:"company_uuid"`
	StartsAt    string  `json:"starts_at"`
	EndsAt      *string `json:"ends_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}
type GrantAdminSubscriptionRequest struct {
	PlanCode string  `json:"plan_code"`
	StartsAt *string `json:"starts_at,omitempty"`
	EndsAt   string  `json:"ends_at"`
	Reason   string  `json:"reason"`
	// ActiveCompanyUUIDs answers "which companies keep working" when the new
	// business plan covers fewer than the owner has. Without it such a grant is
	// refused with company_selection_required and the list to choose from.
	ActiveCompanyUUIDs []string `json:"active_company_uuids,omitempty"`
}
