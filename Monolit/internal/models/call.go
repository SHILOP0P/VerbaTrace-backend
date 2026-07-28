package models

import (
	"io"
	"time"

	"github.com/google/uuid"
)

type Call struct {
	ID                     uuid.UUID
	Title                  string
	Status                 CallStatus
	AudioPath              string
	ASRCachePath           string
	OriginalFilename       string
	MimeType               string
	SizeBytes              int64
	DurationSeconds        int
	UploadedByUserUUID     uuid.NullUUID
	CompanyUUID            uuid.NullUUID
	DepartmentUUID         uuid.NullUUID
	VisibilityScope        CallVisibilityScope
	SkipCustomInstructions bool
	FolderUUID             uuid.NullUUID
	SpeakerHints           []SpeakerHint
	DiarizationRoles       []DiarizationRole
	CreatedAt              time.Time
}

type CallStatus string
type CallVisibilityScope string

const (
	CallStatusNew         CallStatus = "new"
	CallStatusProcessing  CallStatus = "processing"
	CallStatusTranscribed CallStatus = "transcribed"
	CallStatusAnalyzed    CallStatus = "analyzed"
	CallStatusFailed      CallStatus = "failed"
)

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
	FolderUUID             uuid.NullUUID
	SpeakerHints           []SpeakerHint
	DiarizationRoles       []DiarizationRole
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
	UserID             uuid.UUID
	Q                  string
	Status             CallStatus
	VisibilityScope    CallVisibilityScope
	CompanyUUID        uuid.NullUUID
	DepartmentUUID     uuid.NullUUID
	UploadedByUserUUID uuid.NullUUID
	From               *time.Time
	To                 *time.Time
	FolderUUID         uuid.NullUUID
	Limit              int
	Offset             int
}

type ListCallsResult struct {
	Items  []Call
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
	Statuses []CallStatus
	Scopes   []CallVisibilityScope
	Managers []CallFilterUser
}

type CallFilterUser struct {
	ID          uuid.UUID
	FullName    string
	FullSurname string
	Username    string
}
