package dto

import "mime/multipart"

type CreateCallRequest struct {
	Title                  string
	Media                  *multipart.FileHeader
	CompanyUUID            string
	DepartmentUUID         string
	FolderUUID             string
	SkipCustomInstructions bool
}

type CallResponse struct {
	ID                    string                    `json:"id"`
	Title                 string                    `json:"title"`
	Status                string                    `json:"status"`
	OriginalFilename      string                    `json:"original_filename"`
	MimeType              string                    `json:"mime_type"`
	SizeBytes             int64                     `json:"size_bytes"`
	DurationSeconds       int                       `json:"duration_seconds"`
	AudioURL              string                    `json:"audio_url"`
	MediaURL              string                    `json:"media_url"`
	MediaKind             string                    `json:"media_kind"`
	UploadedByUserUUID    *string                   `json:"uploaded_by_user_uuid"`
	CompanyUUID           *string                   `json:"company_uuid"`
	DepartmentUUID        *string                   `json:"department_uuid"`
	VisibilityScope       string                    `json:"visibility_scope"`
	UseCustomInstructions bool                      `json:"use_custom_instructions"`
	IsTest                bool                      `json:"is_test"`
	SpeakerHints          []SpeakerHintResponse     `json:"speaker_hints,omitempty"`
	DiarizationRoles      []DiarizationRoleResponse `json:"diarization_roles,omitempty"`
	CreatedAt             string                    `json:"created_at"`
}

type DiarizationRoleResponse struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SpeakerHintResponse struct {
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Role     string `json:"role"`
	Note     string `json:"note,omitempty"`
}

type CallsListResponse struct {
	Items  []CallResponse `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type CallFilterOptionsResponse struct {
	Statuses []string                 `json:"statuses"`
	Scopes   []string                 `json:"scopes"`
	Managers []CallFilterUserResponse `json:"managers"`
}

type CallFilterUserResponse struct {
	ID          string `json:"id"`
	FullName    string `json:"full_name"`
	FullSurname string `json:"full_surname"`
	Username    string `json:"username"`
}

type CallStatusEvent struct {
	CallID    string `json:"call_id"`
	Status    string `json:"status"`
	Terminal  bool   `json:"terminal"`
	Timestamp string `json:"timestamp"`
}

type UpdateCallTitleRequest struct {
	Title string `json:"title"`
}
