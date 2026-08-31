package models

import (
	"time"

	"github.com/google/uuid"
)

type BitrixOAuthStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type BitrixConnectionHealth struct {
	ConnectionID      uuid.UUID  `json:"connection_uuid"`
	Status            string     `json:"status"`
	PortalDomain      string     `json:"portal_domain"`
	CallsReadable     bool       `json:"calls_readable"`
	TasksWritable     bool       `json:"tasks_writable"`
	UsersReadable     bool       `json:"users_readable"`
	ReconnectRequired bool       `json:"reconnect_required"`
	OAuthConfigured   bool       `json:"oauth_configured"`
	ConnectorVerified bool       `json:"connector_verified"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	LastErrorCode     *string    `json:"last_error_code,omitempty"`
}

type BitrixExternalUser struct {
	ExternalUserID string        `json:"external_user_id"`
	DisplayName    string        `json:"display_name"`
	Active         bool          `json:"active"`
	MappingID      uuid.NullUUID `json:"mapping_uuid,omitempty"`
	InternalUserID uuid.NullUUID `json:"internal_user_uuid,omitempty"`
	DepartmentID   uuid.NullUUID `json:"department_uuid,omitempty"`
	MappingStatus  string        `json:"mapping_status"`
	LockVersion    int64         `json:"lock_version"`
}

type UpdateBitrixUserMappingInput struct {
	ConnectionID        uuid.UUID
	ExternalUserID      string
	InternalUserID      uuid.NullUUID
	DepartmentID        uuid.NullUUID
	Status              string
	ActorID             uuid.UUID
	ExpectedLockVersion int64
}

type BitrixMappingChange struct {
	ExternalUserID      string        `json:"external_user_id"`
	InternalUserID      uuid.NullUUID `json:"internal_user_uuid,omitempty"`
	DepartmentID        uuid.NullUUID `json:"department_uuid,omitempty"`
	Status              string        `json:"status"`
	ExpectedLockVersion int64         `json:"expected_lock_version"`
}

type BitrixMappingDiff struct {
	ExternalUserID       string        `json:"external_user_id"`
	DisplayName          string        `json:"display_name"`
	BeforeInternalUserID uuid.NullUUID `json:"before_internal_user_uuid,omitempty"`
	BeforeDepartmentID   uuid.NullUUID `json:"before_department_uuid,omitempty"`
	BeforeStatus         string        `json:"before_status"`
	AfterInternalUserID  uuid.NullUUID `json:"after_internal_user_uuid,omitempty"`
	AfterDepartmentID    uuid.NullUUID `json:"after_department_uuid,omitempty"`
	AfterStatus          string        `json:"after_status"`
	LockVersion          int64         `json:"lock_version"`
	Changed              bool          `json:"changed"`
}

type BitrixMappingPreview struct {
	ConnectionID uuid.UUID           `json:"connection_uuid"`
	PreviewHash  string              `json:"preview_hash"`
	ChangesCount int                 `json:"changes_count"`
	Items        []BitrixMappingDiff `json:"items"`
}

type BulkUpdateBitrixMappingsInput struct {
	ConnectionID uuid.UUID
	ActorID      uuid.UUID
	PreviewHash  string
	RequestKey   string
	Changes      []BitrixMappingChange
}

type BitrixMappingBulkResult struct {
	CommandID    uuid.UUID            `json:"command_uuid"`
	ConnectionID uuid.UUID            `json:"connection_uuid"`
	PreviewHash  string               `json:"preview_hash"`
	ChangesCount int                  `json:"changes_count"`
	Mappings     []BitrixExternalUser `json:"mappings"`
	Created      bool                 `json:"created"`
}

type BitrixBackfillPreview struct {
	ConnectionID   uuid.UUID `json:"connection_uuid"`
	RangeFrom      time.Time `json:"range_from"`
	RangeTo        time.Time `json:"range_to"`
	EstimatedCalls int       `json:"estimated_calls"`
}

type BitrixBackfill struct {
	ID              uuid.UUID  `json:"backfill_uuid"`
	ConnectionID    uuid.UUID  `json:"connection_uuid"`
	RequestedByID   uuid.UUID  `json:"requested_by_user_uuid"`
	RangeFrom       time.Time  `json:"range_from"`
	RangeTo         time.Time  `json:"range_to"`
	Status          string     `json:"status"`
	EstimatedCalls  *int       `json:"estimated_calls,omitempty"`
	DiscoveredCalls int        `json:"discovered_calls"`
	ImportedCalls   int        `json:"imported_calls"`
	PendingCalls    int        `json:"pending_calls"`
	SkippedCalls    int        `json:"skipped_calls"`
	ErrorCalls      int        `json:"error_calls"`
	LockVersion     int64      `json:"lock_version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}
