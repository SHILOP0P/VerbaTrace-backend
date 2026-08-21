package models

import (
	"io"
	"time"

	"github.com/google/uuid"
)

type AnalysisInstructionScope string

const (
	AnalysisInstructionScopePersonal   AnalysisInstructionScope = "personal"
	AnalysisInstructionScopeCompany    AnalysisInstructionScope = "company"
	AnalysisInstructionScopeDepartment AnalysisInstructionScope = "department"
)

const (
	CompanyInstructionLimit         = 10
	DepartmentInstructionLimit      = 10
	DefaultPersonalInstructionLimit = 5
)

type AnalysisInstruction struct {
	ID                uuid.UUID
	Scope             AnalysisInstructionScope
	UserUUID          uuid.NullUUID
	CompanyUUID       uuid.NullUUID
	DepartmentUUID    uuid.NullUUID
	Title             string
	OriginalFilename  string
	FilePath          string
	MimeType          string
	SizeBytes         int64
	ContentSHA256     string
	SortOrder         int
	IsActive          bool
	CreatedByUserUUID uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateAnalysisInstructionInput struct {
	Scope             AnalysisInstructionScope
	UserUUID          uuid.UUID
	CompanyUUID       uuid.NullUUID
	DepartmentUUID    uuid.NullUUID
	Title             string
	OriginalFilename  string
	MimeType          string
	SizeBytes         int64
	Content           io.Reader
	CreatedByUserUUID uuid.UUID
}

type ListAnalysisInstructionsInput struct {
	Scope           AnalysisInstructionScope
	UserUUID        uuid.UUID
	CompanyUUID     uuid.NullUUID
	DepartmentUUID  uuid.NullUUID
	IncludeInactive bool
	Query           string
	Limit           int
	Offset          int
}

type UpdateAnalysisInstructionInput struct {
	ID        uuid.UUID
	UserUUID  uuid.UUID
	Title     *string
	IsActive  *bool
	SortOrder *int
}

type ReplaceAnalysisInstructionFileInput struct {
	ID               uuid.UUID
	UserUUID         uuid.UUID
	OriginalFilename string
	MimeType         string
	SizeBytes        int64
	Content          io.Reader
}

type ReorderAnalysisInstructionItem struct {
	ID        uuid.UUID
	SortOrder int
}

type ReorderAnalysisInstructionsInput struct {
	Scope          AnalysisInstructionScope
	UserUUID       uuid.UUID
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	Items          []ReorderAnalysisInstructionItem
}

type UpdateAnalysisInstructionRepositoryInput struct {
	ID               uuid.UUID
	Title            *string
	IsActive         *bool
	SortOrder        *int
	OriginalFilename *string
	FilePath         *string
	MimeType         *string
	SizeBytes        *int64
	ContentSHA256    *string
}

type SaveInstructionInput struct {
	InstructionUUID  uuid.UUID
	Scope            AnalysisInstructionScope
	UserUUID         uuid.NullUUID
	CompanyUUID      uuid.NullUUID
	DepartmentUUID   uuid.NullUUID
	OriginalFilename string
	Content          io.Reader
	MimeType         string
}

type SavedInstructionFile struct {
	Path          string
	MimeType      string
	SizeBytes     int64
	ContentSHA256 string
}

type AnalysisInstructionVersion struct {
	ID                uuid.UUID `json:"id"`
	InstructionID     uuid.UUID `json:"instruction_id"`
	Version           int       `json:"version"`
	Title             string    `json:"title"`
	Scope             string    `json:"scope"`
	OriginalFilename  string    `json:"original_filename"`
	MimeType          string    `json:"mime_type"`
	SizeBytes         int64     `json:"size_bytes"`
	Status            string    `json:"status"`
	CreatedByUserUUID uuid.UUID `json:"created_by_user_uuid"`
	CreatedAt         time.Time `json:"created_at"`
	PublishedAt       time.Time `json:"published_at"`
	FilePath          string    `json:"-"`
}
