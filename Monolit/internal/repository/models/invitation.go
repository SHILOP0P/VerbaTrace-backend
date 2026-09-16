package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type MembershipInvitation struct {
	ID                uuid.UUID
	CompanyUUID       uuid.UUID
	DepartmentUUID    uuid.NullUUID
	InvitedUserUUID   uuid.UUID
	InvitedByUserUUID uuid.UUID
	CompanyRole       string
	DepartmentRole    sql.NullString
	Status            string
	ApprovalStatus    string
	ApprovalDecidedBy uuid.NullUUID
	ApprovalDecidedAt sql.NullTime
	ExpiresAt         time.Time
	RespondedAt       sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
