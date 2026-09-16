package models

import (
	"time"

	"github.com/google/uuid"
)

type CallFolderScope string

const (
	CallFolderScopePersonal   CallFolderScope = "personal"
	CallFolderScopeCompany    CallFolderScope = "company"
	CallFolderScopeDepartment CallFolderScope = "department"
)

type CallFolder struct {
	ID                uuid.UUID
	Scope             CallFolderScope
	UserUUID          uuid.NullUUID
	CompanyUUID       uuid.NullUUID
	DepartmentUUID    uuid.NullUUID
	Name              string
	Description       *string
	Color             *string
	CallsCount        int
	CreatedByUserUUID uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
	Instructions      []AnalysisInstruction
}

type CreateCallFolderInput struct {
	UserID         uuid.UUID
	Scope          CallFolderScope
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	Name           string
	Description    *string
	Color          *string
	InstructionIDs []uuid.UUID
}

type UpdateCallFolderInput struct {
	UserID         uuid.UUID
	FolderUUID     uuid.UUID
	Name           *string
	Description    *string
	Color          *string
	InstructionIDs *[]uuid.UUID
}

type ListCallFoldersInput struct {
	UserID         uuid.UUID
	Scope          CallFolderScope
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	Q              string
	Limit          int
	Offset         int
}

type ListCallFoldersResult struct {
	Items  []CallFolder
	Total  int
	Limit  int
	Offset int
}

type AssignCallToFolderInput struct {
	UserID     uuid.UUID
	FolderUUID uuid.UUID
	CallUUID   uuid.UUID
}

type RemoveCallFromFolderInput struct {
	UserID     uuid.UUID
	FolderUUID uuid.UUID
	CallUUID   uuid.UUID
}

type ListFolderCallsInput struct {
	UserID     uuid.UUID
	FolderUUID uuid.UUID
	Limit      int
	Offset     int
}
