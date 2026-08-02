package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type Transcription struct {
	ID           uuid.UUID
	CallUUID     uuid.UUID
	Status       TranscriptionStatus
	Text         sql.NullString
	Segments     sql.NullString
	Words        sql.NullString
	Language     sql.NullString
	Provider     string
	ErrorMessage sql.NullString
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type TranscriptionStatus string

const (
	TranscriptionStatusProcessing  TranscriptionStatus = "processing"
	TranscriptionStatusTranscribed TranscriptionStatus = "transcribed"
	TranscriptionStatusFailed      TranscriptionStatus = "failed"
)
