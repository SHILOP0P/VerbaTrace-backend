package models

import (
	"time"

	"github.com/google/uuid"
)

type Transcription struct {
	ID           uuid.UUID
	CallUUID     uuid.UUID
	Status       TranscriptionStatus
	Text         *string
	Segments     []TranscriptionSegment
	Words        []TranscriptionWord
	Language     *string
	Provider     string
	ErrorMessage *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type TranscriptionSegment struct {
	Speaker      string   `json:"speaker"`
	StartSeconds *float64 `json:"start_seconds,omitempty"`
	EndSeconds   *float64 `json:"end_seconds,omitempty"`
	Text         string   `json:"text"`
}

type TranscriptionWord struct {
	Text         string   `json:"text"`
	StartSeconds float64  `json:"start_seconds"`
	EndSeconds   float64  `json:"end_seconds"`
	Confidence   *float64 `json:"confidence,omitempty"`
	Speaker      string   `json:"speaker,omitempty"`
}

type TranscriptionStatus string

const (
	TranscriptionStatusProcessing  TranscriptionStatus = "processing"
	TranscriptionStatusTranscribed TranscriptionStatus = "transcribed"
	TranscriptionStatusFailed      TranscriptionStatus = "failed"
)
