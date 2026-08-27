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
}

type AnalysisRequest struct {
	CallUUID        uuid.UUID
	Transcription   string
	Instructions    []AnalysisInstructionContent
	Personalization []string
}

type AnalysisResult struct {
	ResultJSON json.RawMessage
	ResultText *string
	Model      *string
	Usage      *ProviderUsage
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
