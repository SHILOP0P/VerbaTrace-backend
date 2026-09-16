package models

import (
	"time"

	"github.com/google/uuid"
)

type SupportAccessRequest struct {
	ID                       uuid.UUID     `json:"request_uuid"`
	RequestedByUserID        uuid.UUID     `json:"requested_by_user_uuid"`
	ApproverUserID           uuid.UUID     `json:"approver_user_uuid"`
	SubjectType              string        `json:"subject_type"`
	SubjectUserID            uuid.NullUUID `json:"subject_user_uuid,omitempty"`
	SubjectCompanyID         uuid.NullUUID `json:"subject_company_uuid,omitempty"`
	Reason                   string        `json:"reason"`
	RequestedResources       []string      `json:"requested_resources"`
	RequestedCommands        []string      `json:"requested_commands"`
	RequestedDurationMinutes int           `json:"requested_duration_minutes"`
	Status                   string        `json:"status"`
	DecisionComment          *string       `json:"decision_comment,omitempty"`
	LockVersion              int64         `json:"lock_version"`
	CreatedAt                time.Time     `json:"created_at"`
	ExpiresAt                time.Time     `json:"expires_at"`
	DecidedAt                *time.Time    `json:"decided_at,omitempty"`
}

type SupportAccessGrant struct {
	ID                uuid.UUID     `json:"grant_uuid"`
	RequestID         uuid.UUID     `json:"request_uuid"`
	GranteeUserID     uuid.UUID     `json:"grantee_user_uuid"`
	GrantedByUserID   uuid.UUID     `json:"granted_by_user_uuid"`
	SubjectType       string        `json:"subject_type"`
	SubjectUserID     uuid.NullUUID `json:"subject_user_uuid,omitempty"`
	SubjectCompanyID  uuid.NullUUID `json:"subject_company_uuid,omitempty"`
	ResourceAllowlist []string      `json:"resource_allowlist"`
	CommandAllowlist  []string      `json:"command_allowlist"`
	ValidFrom         time.Time     `json:"valid_from"`
	ExpiresAt         time.Time     `json:"expires_at"`
	RevokedAt         *time.Time    `json:"revoked_at,omitempty"`
}

type CreateSupportAccessRequestInput struct {
	RequesterUserID          uuid.UUID
	SubjectType              string
	SubjectUserID            uuid.NullUUID
	SubjectCompanyID         uuid.NullUUID
	Reason                   string
	Resources                []string
	Commands                 []string
	RequestedDurationMinutes int
}

// SupportAccessJournalEntry is one line of the company's own record of support
// activity: who looked, when, under which reason, and until when the access was
// valid. Modelled on Access Transparency: the customer sees it, not just us.
type SupportAccessJournalEntry struct {
	ID              uuid.UUID  `json:"id"`
	EventType       string     `json:"event_type"`
	Resource        string     `json:"resource"`
	Command         string     `json:"command"`
	ActorUsername   string     `json:"actor_username"`
	Reason          string     `json:"reason"`
	AccessExpiresAt *time.Time `json:"access_expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}
