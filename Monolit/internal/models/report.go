package models

import (
	"io"
	"time"

	"github.com/google/uuid"
)

type ReportFormat string
type ReportStatus string
type ReportScope string
type ReportSortField string
type SortOrder string

const (
	ReportFormatPDF  ReportFormat = "pdf"
	ReportFormatDOCX ReportFormat = "docx"
	ReportFormatMD   ReportFormat = "md"
	ReportFormatXLSX ReportFormat = "xlsx"
)

const (
	ReportStatusPending ReportStatus = "pending"
	ReportStatusReady   ReportStatus = "ready"
	ReportStatusFailed  ReportStatus = "failed"
)

const (
	ReportScopeCall       ReportScope = "call"
	ReportScopeCompany    ReportScope = "company"
	ReportScopeDepartment ReportScope = "department"
	ReportScopeManager    ReportScope = "manager"
	ReportScopePeriod     ReportScope = "period"
)

const (
	ReportSortCreatedAt ReportSortField = "created_at"
	ReportSortUpdatedAt ReportSortField = "updated_at"
)

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

type ReportExport struct {
	Content               string
	TranscriptionRevision int
	ID                    uuid.UUID
	CallUUID              uuid.UUID
	AnalysisUUID          uuid.UUID
	RequestedByUserUUID   uuid.UUID
	Format                ReportFormat
	Status                ReportStatus
	StoragePath           *string
	FileName              string
	ContentType           string
	SizeBytes             int64
	ErrorMessage          *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ExpiresAt             time.Time
}

type CreateReportInput struct {
	Content               string
	TranscriptionRevision int
	CallUUID              uuid.UUID
	UserUUID              uuid.UUID
	Format                ReportFormat
}

type CreateGlobalReportInput struct {
	UserUUID        uuid.UUID
	Format          ReportFormat
	Scope           ReportScope
	CallUUID        uuid.NullUUID
	CompanyUUID     uuid.NullUUID
	DepartmentUUID  uuid.NullUUID
	ManagerUserUUID uuid.NullUUID
	PeriodFrom      *time.Time
	PeriodTo        *time.Time
}

type ReportCallSummary struct {
	ID             uuid.UUID
	Title          string
	Status         CallStatus
	CreatedAt      time.Time
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
}

type ReportWithCall struct {
	Report ReportExport
	Call   ReportCallSummary
}

type ListReportsInput struct {
	UserUUID       uuid.UUID
	Format         ReportFormat
	Status         ReportStatus
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	CallUUID       uuid.NullUUID
	From           *time.Time
	To             *time.Time
	Sort           ReportSortField
	Order          SortOrder
	Limit          int
	Offset         int
}

type ListReportsResult struct {
	Reports []ReportWithCall
	Total   int
	Limit   int
	Offset  int
}

type CreateReportExportInput struct {
	Report ReportExport
}

type MarkReportReadyInput struct {
	ID          uuid.UUID
	StoragePath string
	FileName    string
	ContentType string
	SizeBytes   int64
}

type MarkReportFailedInput struct {
	ID           uuid.UUID
	ErrorMessage string
}

type SaveReportInput struct {
	ReportUUID            uuid.UUID
	CallUUID              uuid.UUID
	AggregateAnalysisUUID uuid.UUID
	Format                ReportFormat
	FileName              string
	MimeType              string
	Content               io.Reader
}

type SavedReportFile struct {
	Path      string
	MimeType  string
	SizeBytes int64
}

type ReportFile struct {
	Report  ReportExport
	Content io.ReadCloser
}

type AggregateReportExport struct {
	ID                    uuid.UUID
	AggregateAnalysisUUID uuid.UUID
	RequestedByUserUUID   uuid.UUID
	Format                ReportFormat
	Status                ReportStatus
	StoragePath           *string
	FileName              string
	ContentType           string
	SizeBytes             int64
	ErrorMessage          *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ExpiresAt             time.Time
}

type CreateAggregateReportInput struct {
	AggregateAnalysisUUID uuid.UUID
	UserUUID              uuid.UUID
	Format                ReportFormat
}

type MarkAggregateReportReadyInput struct {
	ID          uuid.UUID
	StoragePath string
	FileName    string
	ContentType string
	SizeBytes   int64
}

type MarkAggregateReportFailedInput struct {
	ID           uuid.UUID
	ErrorMessage string
}

type AggregateReportFile struct {
	Report  AggregateReportExport
	Content io.ReadCloser
}
