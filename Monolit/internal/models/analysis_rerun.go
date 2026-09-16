package models

import (
	"time"

	"github.com/google/uuid"
)

type AnalysisRerunRequestStatus string

const (
	AnalysisRerunRequestStatusPending  AnalysisRerunRequestStatus = "pending"
	AnalysisRerunRequestStatusApproved AnalysisRerunRequestStatus = "approved"
	AnalysisRerunRequestStatusRejected AnalysisRerunRequestStatus = "rejected"
	AnalysisRerunRequestStatusCanceled AnalysisRerunRequestStatus = "canceled"
)

// AnalysisRerunRequest is how an employee asks for the analysis of a company
// call to be run again: they cannot start it themselves.
type AnalysisRerunRequest struct {
	ID                  uuid.UUID
	CallUUID            uuid.UUID
	CompanyUUID         uuid.UUID
	DepartmentUUID      uuid.NullUUID
	RequestedByUserUUID uuid.UUID
	Reason              *string
	Status              AnalysisRerunRequestStatus
	DecidedByUserUUID   uuid.NullUUID
	DecidedAt           *time.Time
	Comment             *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateAnalysisRerunRequestInput struct {
	CallUUID uuid.UUID
	UserUUID uuid.UUID
	Reason   string
}

type DecideAnalysisRerunRequestInput struct {
	RequestUUID uuid.UUID
	UserUUID    uuid.UUID
	Approve     bool
	Comment     string
}

type ListAnalysisRerunRequestsInput struct {
	CompanyUUID uuid.UUID
	UserUUID    uuid.UUID
	Status      AnalysisRerunRequestStatus
}
