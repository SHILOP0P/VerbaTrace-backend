package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type CallAnalysis struct {
	ID           uuid.UUID
	CallUUID     uuid.UUID
	Status       CallAnalysisStatus
	Provider     string
	Model        *string
	ResultJSON   json.RawMessage
	ResultText   *string
	ErrorMessage *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CallAnalysisStatus string

const (
	CallAnalysisStatusPending    CallAnalysisStatus = "pending"
	CallAnalysisStatusProcessing CallAnalysisStatus = "processing"
	CallAnalysisStatusDone       CallAnalysisStatus = "done"
	CallAnalysisStatusFailed     CallAnalysisStatus = "failed"
	CallAnalysisStatusStale      CallAnalysisStatus = "stale"
)

type AnalyzeCallInput struct {
	CallUUID uuid.UUID
	UserUUID uuid.UUID
}

type AnalysisInstructionContent struct {
	ID            uuid.UUID
	Scope         AnalysisInstructionScope
	Title         string
	Content       string
	ContentSHA256 string
	// VersionID is the stored version whose file was read. It is empty only when
	// the version could not be resolved; the snapshot then falls back to the
	// latest version, as it did before versions were resolved at read time. It
	// stays out of JSON because instructions are serialized into provider input,
	// which must not change when only the storage bookkeeping does.
	VersionID uuid.UUID `json:"-"`
	// ScorecardID is the scorecard revision the analysis scored this instruction
	// by, recorded in the instruction snapshot.
	ScorecardID uuid.UUID `json:"-"`
}

// InstructionVersionText is the version an analysis reads and, when it has
// been extracted before, its text.
type InstructionVersionText struct {
	VersionID uuid.UUID
	Text      *string
}

type AnalysisRequest struct {
	CallUUID        uuid.UUID
	Transcription   string
	Instructions    []AnalysisInstructionContent
	Personalization []string
	Redaction       *AnalysisRedactionContext
	Task            *AnalysisTask
	// Scorecards is set when the instructions went through their scorecards.
	// The pipeline then scores Requirements as they are and asks the model to
	// break into requirements only the instructions listed in AdhocInstructions.
	// Without it every instruction is broken down by the model, as before
	// scorecards existed.
	Scorecards *AnalysisScorecards
}

type AnalysisScorecards struct {
	Requirements      []AnalysisRequirement
	AdhocInstructions []uuid.UUID
	Applied           []AppliedScorecard
	Mode              string
	LimitApplied      bool
}

// AnalysisTask is a bounded, structured step of the server-owned analysis flow.
type AnalysisTask struct {
	Name   string
	System string
	// Context is sent before Input as its own message. Steps of one run pass it
	// byte-identical so providers can serve the long shared prefix from cache.
	Context   string
	Input     string
	Schema    map[string]any
	MaxTokens int
}

type AnalysisResult struct {
	CreditOperationID     uuid.UUID
	PipelineRunKey        string
	TranscriptionRevision int
	ResultJSON            json.RawMessage
	ResultText            *string
	Model                 *string
	Usage                 *ProviderUsage
}

type ProviderUsage struct {
	ProviderRequestID        string
	PromptTokens             int64
	CachedTokens             int64
	CompletionTokens         int64
	ReasoningTokens          int64
	TotalTokens              int64
	CostNanoUSD              int64
	UpstreamInferenceNanoUSD *int64
}

type CallAnalysisAttempt struct {
	ID                    uuid.UUID
	CallUUID              uuid.UUID
	TranscriptionRevision int
	Status                string
	ErrorMessage          *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}
