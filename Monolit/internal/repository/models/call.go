package models

import (
	"time"

	"github.com/google/uuid"
)

type Call struct {
	ID                     uuid.UUID
	Title                  string
	Status                 string
	AudioPath              string
	ASRCachePath           string
	OriginalFilename       string
	MimeType               string
	SizeBytes              int64
	DurationSeconds        int
	UploadedByUserUUID     uuid.NullUUID
	CompanyUUID            uuid.NullUUID
	DepartmentUUID         uuid.NullUUID
	VisibilityScope        string
	SkipCustomInstructions bool
	IsTest                 bool
	CreatedAt              time.Time
}

const (
	CallStatusNew         string = "new"
	CallStatusProcessing  string = "processing"
	CallStatusTranscribed string = "transcribed"
	CallStatusAnalyzed    string = "analyzed"
	CallStatusFailed      string = "failed"
)
