package models

import (
	"time"

	"github.com/google/uuid"
)

type ProcessingJob struct {
	ID                uuid.UUID
	Type              ProcessingJobType
	TranscriptionMode TranscriptionMode
	EntityUUID        uuid.UUID
	Status            ProcessingJobStatus
	Attempts          int
	MaxAttempts       int
	AvailableAt       time.Time
	LockedAt          *time.Time
	LockedBy          *string
	LastError         *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type TranscriptionMode string

const (
	TranscriptionModeStandard   TranscriptionMode = "standard"
	TranscriptionModeDiarized   TranscriptionMode = "diarized"
	TranscriptionModeIdentified TranscriptionMode = "identified"
)

type ProcessingJobType string
type ProcessingJobStatus string

const (
	ProcessingJobTypeTranscribeCall ProcessingJobType = "transcribe_call"
	ProcessingJobTypeAnalyzeCall    ProcessingJobType = "analyze_call"
)

const DefaultProcessingJobMaxAttempts = 5

const (
	ProcessingJobStatusPending ProcessingJobStatus = "pending"
	ProcessingJobStatusRunning ProcessingJobStatus = "running"
	ProcessingJobStatusDone    ProcessingJobStatus = "done"
	ProcessingJobStatusFailed  ProcessingJobStatus = "failed"
)
