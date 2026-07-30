package dto

type CreateCompanyRequest struct {
	Name string `json:"name"`
}

type UpdateCompanyRequest struct {
	Name string `json:"name"`
}
type UpdateCompanyTagRequest struct {
	Tag string `json:"tag"`
}

type CompanyResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Tag             string `json:"tag"`
	ManagerUserUUID string `json:"manager_user_uuid"`
	MemberLimit     int    `json:"member_limit"`
	CreatedAt       string `json:"created_at"`
}

type CreateDepartmentRequest struct {
	Name string `json:"name"`
}

type UpdateDepartmentRequest struct {
	Name string `json:"name"`
}

type DepartmentResponse struct {
	ID          string `json:"id"`
	CompanyUUID string `json:"company_uuid"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
}

type AddCompanyMemberRequest struct {
	UserUUID string `json:"user_uuid"`
	Role     string `json:"role"`
}

type CompanyMemberResponse struct {
	CompanyUUID string  `json:"company_uuid"`
	UserUUID    string  `json:"user_uuid"`
	Username    string  `json:"username"`
	FullName    string  `json:"full_name"`
	FullSurname string  `json:"full_surname"`
	JobTitle    *string `json:"job_title"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
}

type AddDepartmentMemberRequest struct {
	UserUUID string `json:"user_uuid"`
	Role     string `json:"role"`
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role"`
}

type UpdateMemberStatusRequest struct {
	Status string `json:"status"`
}

type UpdateMemberJobTitleRequest struct {
	JobTitle *string `json:"job_title"`
}

type DepartmentMemberResponse struct {
	DepartmentUUID string  `json:"department_uuid"`
	UserUUID       string  `json:"user_uuid"`
	Username       string  `json:"username"`
	FullName       string  `json:"full_name"`
	FullSurname    string  `json:"full_surname"`
	JobTitle       *string `json:"job_title"`
	Role           string  `json:"role"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
}

type CompanyMembersOverviewResponse struct {
	CompanyUUID      string                              `json:"company_uuid"`
	Manager          *CompanyMemberResponse              `json:"manager"`
	CompanyEmployees []CompanyMemberResponse             `json:"company_employees"`
	Departments      []DepartmentMembersOverviewResponse `json:"departments"`
}

type CompanyMemberDepartmentResponse struct {
	DepartmentUUID string `json:"department_uuid"`
	DepartmentName string `json:"department_name"`
	Role           string `json:"role"`
	Status         string `json:"status"`
}

type CompanyMemberListItemResponse struct {
	UserUUID    string                            `json:"user_uuid"`
	Email       string                            `json:"email"`
	Username    string                            `json:"username"`
	FullName    string                            `json:"full_name"`
	FullSurname string                            `json:"full_surname"`
	JobTitle    *string                           `json:"job_title"`
	CompanyRole string                            `json:"company_role"`
	Status      string                            `json:"status"`
	Departments []CompanyMemberDepartmentResponse `json:"departments"`
	CreatedAt   string                            `json:"created_at"`
}

type CompanyMembersResponse struct {
	Members []CompanyMemberListItemResponse `json:"members"`
	Total   int                             `json:"total"`
	Limit   int                             `json:"limit"`
	Offset  int                             `json:"offset"`
}

type DepartmentMembersOverviewResponse struct {
	Department DepartmentResponse         `json:"department"`
	Members    []DepartmentMemberResponse `json:"members"`
}

type CreateInvitationRequest struct {
	UserUUID string `json:"user_uuid"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type InvitationResponse struct {
	ID                string  `json:"id"`
	CompanyUUID       string  `json:"company_uuid"`
	DepartmentUUID    *string `json:"department_uuid"`
	InvitedUserUUID   string  `json:"invited_user_uuid"`
	InvitedByUserUUID string  `json:"invited_by_user_uuid"`
	CompanyRole       string  `json:"company_role"`
	DepartmentRole    *string `json:"department_role"`
	Status            string  `json:"status"`
	ExpiresAt         string  `json:"expires_at"`
	RespondedAt       *string `json:"responded_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}
