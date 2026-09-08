package dto

type CreateReportRequest struct {
	Content               string `json:"content,omitempty"`
	TranscriptionRevision int    `json:"transcription_revision,omitempty"`
	Format                string `json:"format"`
	PrivacyVariant        string `json:"privacy_variant,omitempty"`
}

type CreateGlobalReportRequest struct {
	Format          string  `json:"format"`
	Scope           string  `json:"scope"`
	CallUUID        *string `json:"call_uuid"`
	CompanyUUID     *string `json:"company_uuid"`
	DepartmentUUID  *string `json:"department_uuid"`
	ManagerUserUUID *string `json:"manager_user_uuid"`
	PeriodFrom      *string `json:"period_from"`
	PeriodTo        *string `json:"period_to"`
}

type ReportResponse struct {
	Content               string  `json:"content,omitempty"`
	TranscriptionRevision int     `json:"transcription_revision,omitempty"`
	ID                    string  `json:"id"`
	CallUUID              string  `json:"call_uuid"`
	AnalysisUUID          *string `json:"analysis_uuid"`
	RequestedByUserUUID   string  `json:"requested_by_user_uuid"`
	Format                string  `json:"format"`
	Status                string  `json:"status"`
	FileName              string  `json:"file_name"`
	ContentType           string  `json:"content_type"`
	SizeBytes             int64   `json:"size_bytes"`
	ErrorMessage          *string `json:"error_message"`
	DownloadURL           *string `json:"download_url"`
	CreatedAt             string  `json:"created_at"`
	UpdatedAt             string  `json:"updated_at"`
	ExpiresAt             string  `json:"expires_at"`
}

type ReportsResponse struct {
	Reports []ReportResponse `json:"reports"`
}

type ReportCallSummaryResponse struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	CompanyUUID    *string `json:"company_uuid"`
	DepartmentUUID *string `json:"department_uuid"`
}

type ReportWithCallResponse struct {
	ReportResponse
	Call ReportCallSummaryResponse `json:"call"`
}

type GlobalReportsResponse struct {
	Reports []ReportWithCallResponse `json:"reports"`
	Total   int                      `json:"total"`
	Limit   int                      `json:"limit"`
	Offset  int                      `json:"offset"`
}
