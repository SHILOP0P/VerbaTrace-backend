package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type ScorecardStatus string

const (
	ScorecardStatusQueued    ScorecardStatus = "queued"
	ScorecardStatusCompiling ScorecardStatus = "compiling"
	ScorecardStatusReady     ScorecardStatus = "ready"
	ScorecardStatusFailed    ScorecardStatus = "failed"
	// ScorecardStatusNotCompiled is answered for a version that has no scorecard
	// row at all; it is never stored.
	ScorecardStatusNotCompiled ScorecardStatus = "not_compiled"
)

const (
	ScorecardOriginCompiled = "compiled"
	ScorecardOriginCopied   = "copied"
	ScorecardOriginEdited   = "edited"
)

const (
	CriterionChangeNew       = "new"
	CriterionChangeUnchanged = "unchanged"
	CriterionChangeReworded  = "reworded"
)

// Scorecard modes recorded on an analysis result.
const (
	ScorecardModeFixed   = "fixed"
	ScorecardModePartial = "partial"
	ScorecardModeAdhoc   = "adhoc"
	ScorecardModeNone    = "none"
)

// ScorecardCompileUsageMode marks the usage operation of compiling a scorecard
// among analysis operations.
const ScorecardCompileUsageMode = "scorecard_compile"

// Error codes stored on a scorecard.
const (
	ScorecardErrorNoCriteria      = "scorecard_no_criteria"
	ScorecardErrorInvalidOutput   = "scorecard_invalid_output"
	ScorecardErrorProvider        = "scorecard_provider_error"
	ScorecardErrorAwaitingCredits = "awaiting_credits"
	ScorecardErrorCompanyFrozen   = "company_frozen"
	ScorecardErrorEmptyText       = "scorecard_empty_text"
)

type Scorecard struct {
	ID                   uuid.UUID
	InstructionID        uuid.UUID
	InstructionTitle     string
	VersionID            uuid.UUID
	InstructionVersion   int
	Revision             int
	Status               ScorecardStatus
	Origin               string
	IsCurrent            bool
	AwaitingConfirmation bool
	ConfirmRequired      bool
	ContentSHA256        string
	CompilerVersion      string
	Model                *string
	Attempts             int
	CompileAfter         *time.Time
	ErrorCode            *string
	ErrorMessage         *string
	LockVersion          int
	Criteria             []ScorecardCriterion
	RemovedCriteria      []RemovedCriterion
	ConfirmedAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ScorecardCriterion struct {
	Key              uuid.UUID
	Position         int
	Title            string
	Requirement      string
	SourceExcerpt    string
	Applicability    string
	Depth            string
	RequiredQuestion bool
	CrossCutting     bool
	Weight           int
	IsCritical       bool
	Enabled          bool
	EditedFields     []string
	ChangeKind       string
	Warnings         []string
	// SameAs is the criterion of the previous scorecard the owner tied this one
	// to. It is read from the aliases, not stored with the criterion.
	SameAs *RemovedCriterion
}

type RemovedCriterion struct {
	Key   uuid.UUID
	Title string
}

// ScorecardCriterionEdit is one row of a scorecard edit. A nil field is left
// as it is.
type ScorecardCriterionEdit struct {
	Key        uuid.UUID
	Title      *string
	Weight     *int
	IsCritical *bool
	Enabled    *bool
}

type EditScorecardInput struct {
	InstructionID   uuid.UUID
	UserID          uuid.UUID
	LockVersion     int
	Criteria        []ScorecardCriterionEdit
	ConfirmRequired *bool
}

// AnalysisRequirement is one scorecard criterion handed to the analysis. The
// server, not the model, attaches its identity to the resulting card.
type AnalysisRequirement struct {
	CriterionKey      uuid.UUID
	AlsoCriterionKeys []uuid.UUID
	ScorecardID       uuid.UUID
	InstructionID     uuid.UUID
	InstructionTitle  string
	Title             string
	Requirement       string
	Applicability     string
	Depth             string
	RequiredQuestion  bool
	CrossCutting      bool
	Weight            int
	IsCritical        bool
}

// InstructionOwner is whose wallet pays for work done on an instruction that
// has no call behind it: a personal instruction is paid by its owner, a company
// or department one by the company.
type InstructionOwner struct {
	UserID       uuid.UUID
	CompanyID    uuid.UUID
	DepartmentID uuid.UUID
}

// AppliedScorecard records which scorecard revision an analysis used.
type AppliedScorecard struct {
	ScorecardID   uuid.UUID
	InstructionID uuid.UUID
	VersionID     uuid.UUID
	Revision      int
}

// ScorecardPlan is what the analysis gets from the scorecards of its
// instructions.
type ScorecardPlan struct {
	// Instructions are the instructions to analyse with. With confirmation
	// switched on, an instruction is analysed with the version its applied
	// scorecard belongs to, which may be older than the one first read.
	Instructions []AnalysisInstructionContent
	Requirements []AnalysisRequirement
	// Adhoc lists instructions without a usable scorecard; the pipeline breaks
	// only these into requirements itself.
	Adhoc        []AnalysisInstructionContent
	Scorecards   []AppliedScorecard
	Mode         string
	LimitApplied bool
}

var (
	ErrScorecardNotFound         = errors.New("scorecard not found")
	ErrScorecardNotReady         = errors.New("scorecard is not ready")
	ErrScorecardVersionConflict  = errors.New("scorecard was changed by someone else")
	ErrScorecardInvalid          = errors.New("invalid scorecard edit")
	ErrScorecardRecompileLimited = errors.New("scorecard recompiled too recently")
	ErrScorecardLimit            = errors.New("too many enabled criteria")
)
