package dto

import (
	"mime/multipart"

	"verbatrace/monolit/internal/models"
)

type CreateCallRequest struct {
	Title                  string
	Media                  *multipart.FileHeader
	CompanyUUID            string
	DepartmentUUID         string
	FolderUUID             string
	SkipCustomInstructions bool
}

type CallResponse struct {
	TranscriptionOnly     bool                      `json:"transcription_only"`
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
	OccurredAt            *string                   `json:"occurred_at"`
	DisplayTime           string                    `json:"display_time"`
	TimeSource            string                    `json:"time_source"`
	SourceProvider        *string                   `json:"source_provider"`
	ConnectionUUID        *string                   `json:"connection_uuid"`
	ExternalCallID        *string                   `json:"external_call_id"`
	ImportedAt            *string                   `json:"imported_at"`
	IngestErrorCode       *string                   `json:"ingest_error_code"`
	HasAnalysis           bool                      `json:"has_analysis"`
	HasActions            bool                      `json:"has_actions"`
	CreatedAt             string                    `json:"created_at"`
	Privacy               *CallPrivacyResponse      `json:"privacy,omitempty"`
	// Filled only for a single call: how the viewer reaches it and whom it
	// counts for.
	Access                  *CallAccessResponse   `json:"access,omitempty"`
	Subjects                []CallSubjectResponse `json:"subjects,omitempty"`
	IsShared                *bool                 `json:"is_shared,omitempty"`
	IsInternal              *bool                 `json:"is_internal,omitempty"`
	SubjectsChangedManually *bool                 `json:"subjects_changed_manually,omitempty"`
}

type CallAccessResponse struct {
	CanEdit bool   `json:"can_edit"`
	Via     string `json:"via"`
}

type CallSubjectResponse struct {
	UserUUID     string   `json:"user_uuid"`
	FullName     string   `json:"full_name"`
	Source       string   `json:"source"`
	IsPrimary    bool     `json:"is_primary"`
	GrantsAccess bool     `json:"grants_access"`
	SpeakerKey   *string  `json:"speaker_key"`
	TalkShare    *float64 `json:"talk_share"`
	MatchSignals []string `json:"match_signals"`
}

type SetCallSubjectsRequest struct {
	UserUUIDs       []string `json:"user_uuids"`
	PrimaryUserUUID *string  `json:"primary_user_uuid"`
}

type CallPrivacyResponse struct {
	Status                  string                     `json:"status"`
	Protected               bool                       `json:"protected"`
	MarkerContract          string                     `json:"marker_contract"`
	PolicyVersion           int                        `json:"policy_version,omitempty"`
	PolicySourceLabel       string                     `json:"policy_source_label"`
	DetectedSpans           int                        `json:"detected_spans"`
	RecommendedMediaVariant string                     `json:"recommended_media_variant"`
	SanitizedMediaStatus    string                     `json:"sanitized_media_status"`
	Capabilities            models.PrivacyCapabilities `json:"capabilities"`
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
	Items      []CallResponse `json:"items"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	NextCursor *string        `json:"next_cursor,omitempty"`
}

type DeletedCallResponse struct {
	Call              CallResponse `json:"call"`
	DeletedAt         string       `json:"deleted_at"`
	PurgeAfter        string       `json:"purge_after"`
	DeletedByUserUUID *string      `json:"deleted_by_user_uuid,omitempty"`
}

type DeletedCallsListResponse struct {
	Items  []DeletedCallResponse `json:"items"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

type CallFilterOptionsResponse struct {
	Statuses    []string                       `json:"statuses"`
	Scopes      []string                       `json:"scopes"`
	Managers    []CallFilterUserResponse       `json:"managers"`
	Connections []CallFilterConnectionResponse `json:"connections"`
}

type CallFilterUserResponse struct {
	ID          string `json:"id"`
	FullName    string `json:"full_name"`
	FullSurname string `json:"full_surname"`
	Username    string `json:"username"`
}

type CallFilterConnectionResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
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
