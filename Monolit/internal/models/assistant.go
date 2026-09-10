package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AssistantCapabilities struct {
	SearchEnabled    bool        `json:"search_enabled"`
	ChatEnabled      bool        `json:"chat_enabled"`
	AggregateEnabled bool        `json:"aggregate_enabled"`
	ExportEnabled    bool        `json:"export_enabled"`
	CompanyUUID      uuid.UUID   `json:"company_uuid"`
	Role             string      `json:"role"`
	DepartmentUUIDs  []uuid.UUID `json:"department_uuids"`
	ReasonCode       string      `json:"reason_code,omitempty"`
}

type ContentSearchInput struct {
	UserUUID      uuid.UUID
	CompanyUUID   uuid.UUID
	Query         string
	CallIDs       []uuid.UUID
	DepartmentIDs []uuid.UUID
	FolderIDs     []uuid.UUID
	From          *time.Time
	To            *time.Time
	Limit         int
}

type ContentSearchItem struct {
	ChunkUUID     uuid.UUID `json:"chunk_uuid"`
	CallUUID      uuid.UUID `json:"call_uuid"`
	Title         string    `json:"title"`
	Quote         string    `json:"quote"`
	Speaker       *string   `json:"speaker,omitempty"`
	StartSeconds  *float64  `json:"start_seconds,omitempty"`
	EndSeconds    *float64  `json:"end_seconds,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Revision      int       `json:"transcription_revision"`
	Score         float64   `json:"score"`
	RetrievalMode string    `json:"retrieval_mode"`
}

type ContentSearchResult struct {
	Items           []ContentSearchItem `json:"items"`
	RetrievalMode   string              `json:"retrieval_mode"`
	EvaluatedCalls  int                 `json:"evaluated_calls"`
	IndexReadyCalls int                 `json:"index_ready_calls"`
	Warnings        []string            `json:"warnings"`
}

type AssistantChat struct {
	ID             uuid.UUID  `json:"id"`
	CompanyUUID    uuid.UUID  `json:"company_uuid"`
	Title          string     `json:"title"`
	ResponseDetail string     `json:"response_detail"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	LockVersion    int64      `json:"lock_version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type AssistantSource struct {
	ID           uuid.UUID `json:"id"`
	CallUUID     uuid.UUID `json:"call_uuid"`
	CallTitle    string    `json:"call_title"`
	Quote        string    `json:"quote"`
	StartSeconds *float64  `json:"start_seconds,omitempty"`
	EndSeconds   *float64  `json:"end_seconds,omitempty"`
	Revision     int       `json:"transcription_revision"`
}

type AssistantBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ArtifactID *uuid.UUID      `json:"artifact_id,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type AssistantArtifact struct {
	ID            uuid.UUID       `json:"id"`
	MessageUUID   uuid.UUID       `json:"message_uuid"`
	Type          string          `json:"type"`
	Title         string          `json:"title"`
	SchemaVersion int             `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
	CreatedAt     time.Time       `json:"created_at"`
}

type AssistantMessage struct {
	ID          uuid.UUID           `json:"id"`
	ChatUUID    uuid.UUID           `json:"chat_uuid"`
	Sequence    int64               `json:"sequence"`
	Role        string              `json:"role"`
	Status      string              `json:"status"`
	Text        string              `json:"text"`
	Blocks      []AssistantBlock    `json:"blocks"`
	Sources     []AssistantSource   `json:"sources"`
	Artifacts   []AssistantArtifact `json:"artifacts"`
	CreatedAt   time.Time           `json:"created_at"`
	CompletedAt *time.Time          `json:"completed_at,omitempty"`
}

type AssistantRun struct {
	ID                uuid.UUID         `json:"id"`
	ChatUUID          uuid.UUID         `json:"chat_uuid"`
	State             string            `json:"state"`
	ResponseDetail    string            `json:"response_detail"`
	DataSnapshotAt    *time.Time        `json:"data_snapshot_at,omitempty"`
	RequestReceivedAt time.Time         `json:"request_received_at"`
	CompletedAt       *time.Time        `json:"completed_at,omitempty"`
	ErrorCode         string            `json:"error_code,omitempty"`
	AssistantMessage  *AssistantMessage `json:"assistant_message,omitempty"`
}

type CreateAssistantMessageInput struct {
	UserUUID        uuid.UUID
	CompanyUUID     uuid.UUID
	ChatUUID        uuid.UUID
	Text            string
	ClientMessageID string
	IdempotencyKey  string
	ResponseDetail  string
	CallIDs         []uuid.UUID
	DepartmentIDs   []uuid.UUID
	FolderIDs       []uuid.UUID
	ContextLabels   []string
	From            *time.Time
	To              *time.Time
}

type AssistantDraft struct {
	Text        string          `json:"text"`
	Context     json.RawMessage `json:"context"`
	LockVersion int64           `json:"lock_version"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
