package models

import (
	"time"

	"github.com/google/uuid"
)

type AppliedInstruction struct {
	AnalysisUUID       uuid.UUID `json:"analysis_uuid"`
	CallUUID           uuid.UUID `json:"call_uuid"`
	InstructionUUID    uuid.UUID `json:"instruction_id"`
	VersionUUID        uuid.UUID `json:"version_id"`
	Version            int       `json:"version"`
	Position           int       `json:"position"`
	Title              string    `json:"title"`
	Scope              string    `json:"scope"`
	SelectionSource    string    `json:"selection_source"`
	ContentSHA256      string    `json:"content_sha256"`
	Content            string    `json:"content,omitempty"`
	OriginalFilename   string    `json:"original_filename,omitempty"`
	InstructionDeleted bool      `json:"instruction_deleted"`
	CreatedAt          time.Time `json:"created_at"`
}
