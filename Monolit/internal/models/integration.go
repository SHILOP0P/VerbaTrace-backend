package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type IntegrationConnection struct {
	ID                  uuid.UUID       `json:"connection_uuid"`
	ApplicationID       uuid.UUID       `json:"application_uuid"`
	CompanyID           uuid.NullUUID   `json:"company_uuid"`
	DepartmentID        uuid.NullUUID   `json:"department_uuid"`
	FolderID            uuid.NullUUID   `json:"folder_uuid"`
	CreatedBy           uuid.UUID       `json:"created_by_user_uuid"`
	Name                string          `json:"name"`
	Provider            string          `json:"provider"`
	Status              string          `json:"status"`
	DisablePolicy       string          `json:"disable_policy"`
	AllowFolderOverride bool            `json:"allow_folder_override"`
	SettingsVersion     int64           `json:"settings_version"`
	Settings            json.RawMessage `json:"settings"`
	LastEventAt         *time.Time      `json:"last_event_at,omitempty"`
	LastSuccessAt       *time.Time      `json:"last_success_at,omitempty"`
	LastErrorCode       *string         `json:"last_error_code,omitempty"`
	LockVersion         int64           `json:"lock_version"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type CreateIntegrationConnectionInput struct {
	ApplicationID, ActorID            uuid.UUID
	CompanyID, DepartmentID, FolderID uuid.NullUUID
	Name, Provider, DisablePolicy     string
	AllowFolderOverride               bool
	Settings                          json.RawMessage
}

type UpdateIntegrationConnectionInput struct {
	ConnectionID        uuid.UUID
	ActorID             uuid.UUID
	Name                string
	DisablePolicy       string
	Settings            json.RawMessage
	ExpectedLockVersion int64
}

type IntegrationServiceAccount struct {
	ID            uuid.UUID  `json:"service_account_uuid"`
	ApplicationID uuid.UUID  `json:"application_uuid"`
	ConnectionID  uuid.UUID  `json:"connection_uuid"`
	CreatedBy     uuid.UUID  `json:"created_by_user_uuid"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Scopes        []string   `json:"scopes"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}

type IngestCallInput struct {
	SchemaVersion    int                 `json:"schema_version"`
	ExternalEventID  string              `json:"external_event_id"`
	ExternalCallID   string              `json:"external_call_id"`
	Title            string              `json:"title"`
	RecordingURL     string              `json:"recording_url"`
	AIMode           string              `json:"ai_mode,omitempty"`
	OriginalFilename string              `json:"original_filename"`
	OccurredAt       *time.Time          `json:"occurred_at"`
	Participants     []map[string]string `json:"participants"`
	Metadata         map[string]any      `json:"metadata"`
	Destination      *IngestDestination  `json:"destination,omitempty"`
	InstructionMode  string              `json:"instruction_mode,omitempty"`
}

type IngestDestination struct {
	Scope        string    `json:"scope"`
	CompanyID    uuid.UUID `json:"company_uuid,omitempty"`
	DepartmentID uuid.UUID `json:"department_uuid,omitempty"`
	FolderID     uuid.UUID `json:"folder_uuid,omitempty"`
}

type IntegrationDestination struct {
	Scope        string        `json:"scope"`
	UserID       uuid.NullUUID `json:"user_uuid,omitempty"`
	CompanyID    uuid.NullUUID `json:"company_uuid,omitempty"`
	DepartmentID uuid.NullUUID `json:"department_uuid,omitempty"`
	Name         string        `json:"name"`
}

type IntegrationFolder struct {
	ID         uuid.UUID `json:"folder_uuid"`
	Name       string    `json:"name"`
	SystemType *string   `json:"system_type,omitempty"`
	IsSystem   bool      `json:"is_system"`
}

type IntegrationCallView struct {
	ID              uuid.UUID     `json:"call_uuid"`
	Title           string        `json:"title"`
	Status          string        `json:"status"`
	DurationSeconds int           `json:"duration_seconds"`
	VisibilityScope string        `json:"visibility_scope"`
	CompanyID       uuid.NullUUID `json:"company_uuid,omitempty"`
	DepartmentID    uuid.NullUUID `json:"department_uuid,omitempty"`
	FolderID        uuid.NullUUID `json:"folder_uuid,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	ExternalCallID  string        `json:"external_call_id"`
	SourceRef       string        `json:"source_ref"`
}

type IntegrationCallFilter struct {
	UpdatedSince    *time.Time
	From            *time.Time
	To              *time.Time
	Status          string
	Limit           int
	CursorUpdatedAt *time.Time
	CursorID        uuid.UUID
}

type IntegrationTranscriptionView struct {
	ID        uuid.UUID       `json:"transcription_uuid"`
	CallID    uuid.UUID       `json:"call_uuid"`
	Status    string          `json:"status"`
	Text      *string         `json:"text,omitempty"`
	Segments  json.RawMessage `json:"segments,omitempty"`
	Words     json.RawMessage `json:"words,omitempty"`
	Language  *string         `json:"language,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type IntegrationAnalysisView struct {
	ID         uuid.UUID       `json:"analysis_uuid"`
	CallID     uuid.UUID       `json:"call_uuid"`
	Status     string          `json:"status"`
	Model      *string         `json:"model,omitempty"`
	ResultJSON json.RawMessage `json:"result_json,omitempty"`
	ResultText *string         `json:"result_text,omitempty"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type IntegrationUsageView struct {
	Environment               string     `json:"environment"`
	AvailableCredits          int64      `json:"available_credits"`
	KeyCreditsUsed            int64      `json:"key_credits_used"`
	PermanentCreditLimit      *int64     `json:"permanent_credit_limit,omitempty"`
	PermanentCreditsRemaining *int64     `json:"permanent_credits_remaining,omitempty"`
	TemporaryCreditLimit      *int64     `json:"temporary_credit_limit,omitempty"`
	TemporaryCreditsUsed      *int64     `json:"temporary_credits_used,omitempty"`
	TemporaryCreditsRemaining *int64     `json:"temporary_credits_remaining,omitempty"`
	TemporaryLimitStartsAt    *time.Time `json:"temporary_limit_starts_at,omitempty"`
	TemporaryLimitEndsAt      *time.Time `json:"temporary_limit_ends_at,omitempty"`
	ActiveIngestItems         int        `json:"active_ingest_items"`
	MaximumUploadBytes        int64      `json:"maximum_upload_bytes"`
}

type IngestItem struct {
	ID                       uuid.UUID       `json:"ingest_item_uuid"`
	ApplicationID            uuid.UUID       `json:"application_uuid"`
	ConnectionID             uuid.UUID       `json:"connection_uuid"`
	EventID                  uuid.UUID       `json:"event_uuid"`
	BillingAccountID         uuid.UUID       `json:"-"`
	ExternalCallID           string          `json:"external_call_id"`
	SourceRef                string          `json:"source_ref"`
	KeyID                    uuid.NullUUID   `json:"-"`
	IdempotencyKey           string          `json:"-"`
	SourceKind               string          `json:"source_kind"`
	Title                    string          `json:"title"`
	Status                   string          `json:"status"`
	Stage                    string          `json:"stage"`
	AIMode                   string          `json:"ai_mode"`
	BillingEnvironment       string          `json:"billing_environment"`
	DestinationScope         string          `json:"destination_scope"`
	DestinationUserID        uuid.NullUUID   `json:"destination_user_uuid,omitempty"`
	DestinationCompanyID     uuid.NullUUID   `json:"destination_company_uuid,omitempty"`
	DestinationDepartmentID  uuid.NullUUID   `json:"destination_department_uuid,omitempty"`
	DestinationFolderID      uuid.NullUUID   `json:"destination_folder_uuid,omitempty"`
	PlacementSource          string          `json:"placement_source"`
	InheritScopeInstructions bool            `json:"inherit_scope_instructions"`
	OriginalFilename         *string         `json:"original_filename,omitempty"`
	OccurredAt               *time.Time      `json:"occurred_at,omitempty"`
	Metadata                 json.RawMessage `json:"metadata,omitempty"`
	Attempts                 int             `json:"attempts"`
	MaxAttempts              int             `json:"max_attempts"`
	AvailableAt              time.Time       `json:"available_at"`
	CallID                   uuid.NullUUID   `json:"call_uuid"`
	ErrorCode                *string         `json:"error_code,omitempty"`
	ErrorMessage             *string         `json:"error_message,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
	CompletedAt              *time.Time      `json:"completed_at,omitempty"`
	CancelledAt              *time.Time      `json:"cancelled_at,omitempty"`
}

type IntegrationAuditEvent struct {
	ID            uuid.UUID       `json:"audit_event_uuid"`
	ApplicationID uuid.UUID       `json:"application_uuid"`
	ConnectionID  uuid.NullUUID   `json:"connection_uuid"`
	ActorType     string          `json:"actor_type"`
	ActorID       uuid.NullUUID   `json:"actor_uuid"`
	EventType     string          `json:"event_type"`
	EntityType    string          `json:"entity_type"`
	EntityID      uuid.UUID       `json:"entity_uuid"`
	Metadata      json.RawMessage `json:"metadata"`
	RequestID     *string         `json:"request_id,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type WebhookEndpoint struct {
	ID            uuid.UUID     `json:"webhook_endpoint_uuid"`
	ApplicationID uuid.UUID     `json:"application_uuid"`
	ConnectionID  uuid.NullUUID `json:"connection_uuid"`
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	EventTypes    []string      `json:"event_types"`
	LockVersion   int64         `json:"lock_version"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type WebhookDelivery struct {
	ID           uuid.UUID `json:"delivery_uuid"`
	OutboxID     uuid.UUID `json:"outbox_uuid"`
	EndpointID   uuid.UUID `json:"webhook_endpoint_uuid"`
	Attempt      int       `json:"attempt"`
	Status       string    `json:"status"`
	HTTPStatus   *int      `json:"http_status,omitempty"`
	LatencyMS    *int64    `json:"latency_ms,omitempty"`
	ResponseSize *int64    `json:"response_size_bytes,omitempty"`
	ErrorCode    *string   `json:"error_code,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ClaimedIngestItem struct {
	IngestItem
	CreatedByUserID                   uuid.UUID
	CompanyID, DepartmentID, FolderID uuid.NullUUID
	ConnectionStatus, DisablePolicy   string
	LocatorCiphertext                 []byte
	LocatorKeyVersion                 int
}

type WebhookTarget struct {
	EndpointID         uuid.UUID
	URL, SigningSecret string
	Attempt            int
}
type ClaimedWebhookEvent struct {
	OutboxID, EventID, ApplicationID, ConnectionID, AggregateID uuid.UUID
	EventType                                                   string
	Payload                                                     json.RawMessage
	CreatedAt                                                   time.Time
	Targets                                                     []WebhookTarget
}
