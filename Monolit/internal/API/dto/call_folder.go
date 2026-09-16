package dto

type CallFolderResponse struct {
	ID                string                `json:"id"`
	Scope             string                `json:"scope"`
	UserUUID          *string               `json:"user_uuid"`
	CompanyUUID       *string               `json:"company_uuid"`
	DepartmentUUID    *string               `json:"department_uuid"`
	Name              string                `json:"name"`
	Description       *string               `json:"description"`
	Color             *string               `json:"color"`
	CallsCount        int                   `json:"calls_count"`
	CreatedByUserUUID string                `json:"created_by_user_uuid"`
	CreatedAt         string                `json:"created_at"`
	UpdatedAt         string                `json:"updated_at"`
	Instructions      []AnalysisInstruction `json:"instructions"`
}

type CallFoldersListResponse struct {
	Items  []CallFolderResponse `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

type CreateCallFolderRequest struct {
	Scope            string   `json:"scope"`
	CompanyUUID      *string  `json:"company_uuid"`
	DepartmentUUID   *string  `json:"department_uuid"`
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	Color            *string  `json:"color"`
	InstructionUUIDs []string `json:"instruction_uuids"`
}

type UpdateCallFolderRequest struct {
	Name             *string   `json:"name"`
	Description      *string   `json:"description"`
	Color            *string   `json:"color"`
	InstructionUUIDs *[]string `json:"instruction_uuids"`
}

type AssignCallToFolderRequest struct {
	CallUUID string `json:"call_uuid"`
}
