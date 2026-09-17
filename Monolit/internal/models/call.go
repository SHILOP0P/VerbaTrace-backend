package models

import (
	"io"
	"time"

	"github.com/google/uuid"
)

type Call struct {
	ID                        uuid.UUID
	Title                     string
	Status                    CallStatus
	AudioPath                 string
	ASRCachePath              string
	OriginalFilename          string
	MimeType                  string
	SizeBytes                 int64
	DurationSeconds           int
	UploadedByUserUUID        uuid.NullUUID
	CompanyUUID               uuid.NullUUID
	DepartmentUUID            uuid.NullUUID
	VisibilityScope           CallVisibilityScope
	SkipCustomInstructions    bool
	TranscriptionOnly         bool
	IsTest                    bool
	FolderUUID                uuid.NullUUID
	SpeakerHints              []SpeakerHint
	DiarizationRoles          []DiarizationRole
	OccurredAt                *time.Time
	DisplayTime               time.Time
	TimeSource                string
	SourceProvider            *string
	IntegrationConnectionUUID uuid.NullUUID
	ExternalCallID            *string
	ImportedAt                *time.Time
	IngestErrorCode           *string
	HasAnalysis               bool
	HasActions                bool
	CreatedAt                 time.Time
}

type CallStatus string
type CallVisibilityScope string

const (
	CallStatusNew CallStatus = "new"
	// CallStatusAwaitingCredits is a call that was accepted but cannot be sent to
	// a provider yet: the credit limit or the wallet is exhausted. It waits in the
	// queue and starts on its own once there is room again. It is not a failure
	// and must never be shown as one.
	CallStatusAwaitingCredits CallStatus = "awaiting_credits"
	CallStatusProcessing      CallStatus = "processing"
	// CallStatusCancelled is a call whose processing was stopped on purpose. The
	// audio is still there and the call can be started again, switched to
	// transcription only, downloaded or binned.
	CallStatusCancelled   CallStatus = "cancelled"
	CallStatusTranscribed CallStatus = "transcribed"
	CallStatusAnalyzed    CallStatus = "analyzed"
	CallStatusFailed      CallStatus = "failed"
)

// CallStatuses lists every state a call can be in, in the order the work moves
// through them. The filter and the API's allow-list both read it, so a new
// status cannot be added to one and forgotten in the other.
func CallStatuses() []CallStatus {
	return []CallStatus{
		CallStatusNew,
		CallStatusProcessing,
		CallStatusAwaitingCredits,
		CallStatusCancelled,
		CallStatusTranscribed,
		CallStatusAnalyzed,
		CallStatusFailed,
	}
}

// Valid answers whether a status came from the list above. A filter that names
// an unknown status is a client mistake, not an empty result.
func (s CallStatus) Valid() bool {
	for _, known := range CallStatuses() {
		if s == known {
			return true
		}
	}

	return false
}

const (
	CallVisibilityScopePersonal   CallVisibilityScope = "personal"
	CallVisibilityScopeCompany    CallVisibilityScope = "company"
	CallVisibilityScopeDepartment CallVisibilityScope = "department"
)

type CreateCallInput struct {
	Title                  string
	OriginalFilename       string
	MimeType               string
	SizeBytes              int64
	Content                io.Reader
	UploadedByUserUUID     uuid.UUID
	CompanyUUID            uuid.NullUUID
	DepartmentUUID         uuid.NullUUID
	VisibilityScope        CallVisibilityScope
	SkipCustomInstructions bool
	TranscriptionOnly      bool
	FolderUUID             uuid.NullUUID
	SpeakerHints           []SpeakerHint
	DiarizationRoles       []DiarizationRole
	// IntegrationPrincipalUUID is set only by the internal durable ingest
	// worker after it revalidates connection placement. HTTP callers cannot set it.
	IntegrationPrincipalUUID uuid.NullUUID
}

type SpeakerHint struct {
	UserID   uuid.UUID
	Name     string
	Username string
	Role     string
	Note     string
}

type DiarizationRole struct {
	Name        string
	Description string
}

type UpdateCallStatusInput struct {
	CallUUID uuid.UUID
	Status   CallStatus
}

type ListCallsInput struct {
	UserID                uuid.UUID
	Q                     string
	Status                CallStatus
	VisibilityScope       CallVisibilityScope
	CompanyUUID           uuid.NullUUID
	DepartmentUUID        uuid.NullUUID
	UploadedByUserUUID    uuid.NullUUID
	From                  *time.Time
	To                    *time.Time
	FolderUUID            uuid.NullUUID
	Statuses              []CallStatus
	VisibilityScopes      []CallVisibilityScope
	DepartmentUUIDs       []uuid.UUID
	ParticipantUserUUIDs  []uuid.UUID
	FolderUUIDs           []uuid.UUID
	ConnectionUUIDs       []uuid.UUID
	SourceProvider        string
	OccurredFrom          *time.Time
	OccurredTo            *time.Time
	ImportedFrom          *time.Time
	ImportedTo            *time.Time
	DurationMinSeconds    *int
	DurationMaxSeconds    *int
	HasAnalysis           *bool
	HasActions            *bool
	HasProcessingError    *bool
	FavoriteOnly          bool
	IncludeUploadFallback bool
	Sort                  string
	Order                 string
	Cursor                *CallListCursor
	Limit                 int
	Offset                int
}

type CallListCursor struct {
	SortValue     string     `json:"sort_value"`
	CallID        uuid.UUID  `json:"call_uuid"`
	Sort          string     `json:"sort"`
	Order         string     `json:"order"`
	TimeValue     *time.Time `json:"-"`
	DurationValue *int       `json:"-"`
}

type ListCallsResult struct {
	Items      []Call
	Total      int
	Limit      int
	Offset     int
	NextCursor *CallListCursor
}

// CallBinRetention is how long a deleted call waits before it is purged along
// with its files. The grace period exists so an accidental deletion can be
// undone without a database restore.
const CallBinRetention = 30 * 24 * time.Hour

type DeletedCall struct {
	Call              Call
	DeletedAt         time.Time
	PurgeAfter        time.Time
	DeletedByUserUUID uuid.NullUUID
}

type ListDeletedCallsInput struct {
	UserID uuid.UUID
	Limit  int
	Offset int
}

type ListDeletedCallsResult struct {
	Items  []DeletedCall
	Total  int
	Limit  int
	Offset int
}

type CallFilterOptionsInput struct {
	UserID         uuid.UUID
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
}

type CallFilterOptions struct {
	Statuses    []CallStatus
	Scopes      []CallVisibilityScope
	Managers    []CallFilterUser
	Connections []CallFilterConnection
}

type CallFilterUser struct {
	ID          uuid.UUID
	FullName    string
	FullSurname string
	Username    string
}

type CallFilterConnection struct {
	ID       uuid.UUID
	Name     string
	Provider string
}
