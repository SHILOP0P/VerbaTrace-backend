package action

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidInput = errors.New("invalid action input")
	ErrNotFound     = errors.New("action not found")
	ErrForbidden    = errors.New("action forbidden")
	ErrConflict     = errors.New("action conflict")
)

type EvidenceInput struct {
	WordStartIndex *int   `json:"word_start_index"`
	WordEndIndex   *int   `json:"word_end_index"`
	LegacyQuote    string `json:"legacy_quote"`
}

type CreateInput struct {
	ActorUserUUID    uuid.UUID
	CallUUID         uuid.UUID
	AnalysisUUID     uuid.UUID
	SourceDepartment uuid.UUID
	TargetDepartment uuid.UUID
	AssigneeUserUUID uuid.UUID
	Title            string
	Description      string
	DueAt            time.Time
	Evidence         []EvidenceInput
	IdempotencyKey   string
}

type Evidence struct {
	ID             uuid.UUID `json:"id"`
	Kind           string    `json:"kind"`
	Position       int       `json:"position"`
	WordStartIndex *int      `json:"word_start_index,omitempty"`
	WordEndIndex   *int      `json:"word_end_index,omitempty"`
	Quote          string    `json:"quote"`
	Speaker        *string   `json:"speaker,omitempty"`
	StartSeconds   *float64  `json:"start_seconds,omitempty"`
	EndSeconds     *float64  `json:"end_seconds,omitempty"`
}

type Item struct {
	ID                    uuid.UUID    `json:"id"`
	CompanyUUID           *uuid.UUID   `json:"company_uuid,omitempty"`
	CompanyName           string       `json:"company_name"`
	CompanyTag            string       `json:"company_tag"`
	ScopeType             string       `json:"scope_type"`
	ScopeTag              string       `json:"scope_tag"`
	SourceDepartmentUUID  *uuid.UUID   `json:"source_department_uuid,omitempty"`
	SourceDepartmentName  string       `json:"source_department_name"`
	TargetDepartmentUUID  *uuid.UUID   `json:"target_department_uuid,omitempty"`
	TargetDepartmentName  string       `json:"target_department_name"`
	CallUUID              uuid.UUID    `json:"call_uuid"`
	AnalysisUUID          uuid.UUID    `json:"analysis_uuid"`
	TranscriptionRevision int          `json:"transcription_revision"`
	Title                 string       `json:"title"`
	Description           string       `json:"description"`
	Status                string       `json:"status"`
	AssignmentState       string       `json:"assignment_state"`
	AssigneeUserUUID      uuid.UUID    `json:"assignee_user_uuid"`
	AssigneeUsername      string       `json:"assignee_username"`
	DueAt                 time.Time    `json:"due_at"`
	GraceExpiresAt        time.Time    `json:"grace_expires_at"`
	LockVersion           int64        `json:"lock_version"`
	CreatedByUserUUID     uuid.UUID    `json:"created_by_user_uuid"`
	CreatedAt             time.Time    `json:"created_at"`
	UpdatedAt             time.Time    `json:"updated_at"`
	CompletedAt           *time.Time   `json:"completed_at,omitempty"`
	CancelledAt           *time.Time   `json:"cancelled_at,omitempty"`
	CancelReason          *string      `json:"cancel_reason,omitempty"`
	Evidence              []Evidence   `json:"evidence"`
	Capabilities          Capabilities `json:"capabilities"`
}

type Capabilities struct {
	CanStart           bool `json:"can_start"`
	CanComplete        bool `json:"can_complete"`
	CanCancel          bool `json:"can_cancel"`
	CanReschedule      bool `json:"can_reschedule"`
	CanReassign        bool `json:"can_reassign"`
	CanRequestTransfer bool `json:"can_request_transfer"`
	CanResolveTransfer bool `json:"can_resolve_transfer"`
	CanReopen          bool `json:"can_reopen"`
}

type ListInput struct {
	ActorUserUUID  uuid.UUID
	CompanyUUID    uuid.NullUUID
	CallUUID       uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	AssigneeUUID   uuid.NullUUID
	Status         string
	Query          string
	CompanyTag     string
	Department     string
	Mine           bool
	Limit          int
	Offset         int
	Admin          bool
}

type ListResult struct {
	Items  []Item `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type UpdateInput struct {
	ActorUserUUID   uuid.UUID
	ActionUUID      uuid.UUID
	ExpectedVersion int64
	Reason          string
	Admin           bool
}

type RescheduleInput struct {
	UpdateInput
	DueAt time.Time
}
type ReassignInput struct {
	UpdateInput
	AssigneeUserUUID, TargetDepartmentUUID uuid.UUID
}

type TransferInput struct {
	ActorUserUUID      uuid.UUID
	ActionUUID         uuid.UUID
	Reason             string
	ProposedAssignee   uuid.NullUUID
	ProposedDepartment uuid.NullUUID
}

type ResolveTransferInput struct {
	ActorUserUUID   uuid.UUID
	ActionUUID      uuid.UUID
	RequestUUID     uuid.UUID
	Approve         bool
	Comment         string
	ExpectedVersion int64
}

type TransferRequest struct {
	ID                 uuid.UUID  `json:"id"`
	ActionUUID         uuid.UUID  `json:"action_uuid"`
	RequestedBy        uuid.UUID  `json:"requested_by_user_uuid"`
	ProposedAssignee   *uuid.UUID `json:"proposed_assignee_user_uuid,omitempty"`
	ProposedDepartment *uuid.UUID `json:"proposed_department_uuid,omitempty"`
	Reason             string     `json:"reason"`
	Status             string     `json:"status"`
	ResolutionComment  *string    `json:"resolution_comment,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`
}

type Event struct {
	ID            uuid.UUID       `json:"id"`
	Type          string          `json:"type"`
	ActorUserUUID *uuid.UUID      `json:"actor_user_uuid,omitempty"`
	Reason        *string         `json:"reason,omitempty"`
	OldData       json.RawMessage `json:"old_data,omitempty"`
	NewData       json.RawMessage `json:"new_data,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Assignee struct {
	UserUUID    uuid.UUID            `json:"user_uuid"`
	Username    string               `json:"username"`
	FullName    string               `json:"full_name"`
	FullSurname string               `json:"full_surname"`
	JobTitle    *string              `json:"job_title,omitempty"`
	Departments []AssigneeDepartment `json:"departments"`
}
type AssigneeDepartment struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type Word struct {
	Text    string  `json:"text"`
	Start   float64 `json:"start_seconds"`
	End     float64 `json:"end_seconds"`
	Speaker string  `json:"speaker"`
}
