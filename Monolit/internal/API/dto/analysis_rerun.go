package dto

type CreateAnalysisRerunRequest struct {
	Reason string `json:"reason"`
}

type DecideAnalysisRerunRequest struct {
	Comment string `json:"comment"`
}

type AnalysisRerunRequestResponse struct {
	ID                  string  `json:"id"`
	CallUUID            string  `json:"call_uuid"`
	CompanyUUID         string  `json:"company_uuid"`
	DepartmentUUID      *string `json:"department_uuid,omitempty"`
	RequestedByUserUUID string  `json:"requested_by_user_uuid"`
	Reason              *string `json:"reason,omitempty"`
	Status              string  `json:"status"`
	DecidedByUserUUID   *string `json:"decided_by_user_uuid,omitempty"`
	DecidedAt           *string `json:"decided_at,omitempty"`
	Comment             *string `json:"comment,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type AnalysisRerunRequestsResponse struct {
	Items []AnalysisRerunRequestResponse `json:"items"`
}
