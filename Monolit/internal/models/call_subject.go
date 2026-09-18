package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// How a user reaches a call they can see.
const (
	CallAccessViaUploader   = "uploader"
	CallAccessViaManagement = "management"
	CallAccessViaSubject    = "subject"
)

// CallAccess is what the call page needs to know about its viewer: whether to
// offer editing at all, and why the call is visible.
type CallAccess struct {
	CanEdit bool
	Via     string
}

// Where a subject of a call came from.
const (
	CallSubjectSourceUploader     = "uploader"
	CallSubjectSourceSpeakerMatch = "speaker_match"
	CallSubjectSourceManual       = "manual"
)

// Signals that tie a speaker to an employee. The strong ones were set by a
// person or a system of record and may share the call; the weak ones are read
// from the text and only feed statistics.
const (
	SubjectSignalManualAssignment = "manual_assignment"
	SubjectSignalHintName         = "hint_name"
	SubjectSignalMemberName       = "member_name"
	SubjectSignalSelfIntroduction = "self_introduction"
	SubjectSignalUploader         = "uploader"
)

// Why the subjects of a call were resolved again.
const (
	CallSubjectCauseTranscription      = "transcription"
	CallSubjectCauseTranscriptionEdit  = "transcription_edit"
	CallSubjectCauseSpeakerAssignments = "speaker_assignments"
	CallSubjectCauseAnalysis           = "analysis"
	CallSubjectCauseManual             = "manual"
	CallSubjectCauseBackfill           = "backfill"
	CallSubjectCauseCompanyTransfer    = "company_transfer"
)

// CallSubject is an employee a call counts for.
type CallSubject struct {
	UserID       uuid.UUID
	FullName     string
	Source       string
	IsPrimary    bool
	SpeakerKey   *string
	TalkShare    *float64
	MatchSignals []string
	GrantsAccess bool
	SetBy        uuid.NullUUID
	CreatedAt    time.Time
}

// CallSubjects is the composition of a call with the flags derived from it.
type CallSubjects struct {
	Subjects                []CallSubject
	IsShared                bool
	IsInternal              bool
	SubjectsChangedManually bool
}

// CallSubjectCandidate is an employee who may be marked as a speaker of a call.
type CallSubjectCandidate struct {
	UserID   uuid.UUID
	FullName string
	Username string
}

// SetCallSubjectsInput replaces the automatic composition by hand. An empty
// list hands the call back to automatic resolution.
type SetCallSubjectsInput struct {
	CallID  uuid.UUID
	ActorID uuid.UUID
	UserIDs []uuid.UUID
	Primary uuid.UUID
}

var (
	ErrInvalidCallSubjects = errors.New("invalid call subjects")
	ErrCallSubjectsLocked  = errors.New("call subjects cannot be changed")
)
