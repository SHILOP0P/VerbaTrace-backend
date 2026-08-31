package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ActionExternalSync struct {
	ID                uuid.UUID       `json:"sync_uuid"`
	ActionID          uuid.UUID       `json:"action_uuid"`
	ConnectionID      uuid.UUID       `json:"connection_uuid"`
	Provider          string          `json:"provider"`
	RequesterUserID   uuid.UUID       `json:"requester_user_uuid"`
	ApproverUserID    uuid.NullUUID   `json:"approver_user_uuid,omitempty"`
	State             string          `json:"state"`
	ExternalTaskID    *string         `json:"external_task_id,omitempty"`
	ExternalTaskURL   *string         `json:"external_task_url,omitempty"`
	IdempotencyMarker string          `json:"idempotency_marker"`
	RequestPayload    json.RawMessage `json:"request_payload"`
	ActionLockVersion int64           `json:"action_lock_version"`
	Attempts          int             `json:"attempts"`
	LastErrorCode     *string         `json:"last_error_code,omitempty"`
	LockVersion       int64           `json:"lock_version"`
	CreatedAt         time.Time       `json:"created_at"`
	ApprovedAt        *time.Time      `json:"approved_at,omitempty"`
	RejectedAt        *time.Time      `json:"rejected_at,omitempty"`
	SyncedAt          *time.Time      `json:"synced_at,omitempty"`
	ExternalSnapshot  json.RawMessage `json:"external_snapshot,omitempty"`
	ReviewState       *string         `json:"review_state,omitempty"`
	ReviewReason      *string         `json:"review_reason,omitempty"`
	LastCheckedAt     *time.Time      `json:"last_checked_at,omitempty"`
	ReviewedByUserID  uuid.NullUUID   `json:"reviewed_by_user_uuid,omitempty"`
	ReviewedAt        *time.Time      `json:"reviewed_at,omitempty"`
	UpdatedAt         time.Time       `json:"updated_at"`
	CanApprove        bool            `json:"can_approve"`
	CanReject         bool            `json:"can_reject"`
	CanResolve        bool            `json:"can_resolve"`
}

type ResolveActionExternalSyncInput struct {
	SyncID              uuid.UUID
	ActorID             uuid.UUID
	ExpectedLockVersion int64
	Resolution          string
	ExternalTaskID      string
	Reason              string
}

type ActionExternalSyncPreview struct {
	ActionID       uuid.UUID                        `json:"action_uuid"`
	Title          string                           `json:"title"`
	DueAt          time.Time                        `json:"due_at"`
	AssigneeUserID uuid.UUID                        `json:"assignee_user_uuid"`
	Options        []ActionExternalConnectionOption `json:"options"`
}

type ActionExternalConnectionOption struct {
	ConnectionID       uuid.UUID `json:"connection_uuid"`
	ConnectionName     string    `json:"connection_name"`
	PortalDomain       string    `json:"portal_domain"`
	ExternalAssigneeID *string   `json:"external_assignee_id,omitempty"`
	Available          bool      `json:"available"`
	UnavailableReason  string    `json:"unavailable_reason,omitempty"`
}
