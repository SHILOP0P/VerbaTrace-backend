package models

import (
	"time"

	"github.com/google/uuid"
)

type Department struct {
	ID          uuid.UUID
	CompanyUUID uuid.UUID
	Name        string
	CreatedAt   time.Time
	DeletedAt   *time.Time
}

type DepartmentMember struct {
	DepartmentUUID uuid.UUID
	UserUUID       uuid.UUID
	Username       string
	FullName       string
	FullSurname    string
	JobTitle       *string
	Role           DepartmentMemberRole
	Status         MembershipStatus
	CreatedAt      time.Time
}

type DepartmentMemberRole string

const (
	DepartmentMemberRoleLeader   DepartmentMemberRole = "department_leader"
	DepartmentMemberRoleEmployee DepartmentMemberRole = "employee"
)

type CreateDepartmentInput struct {
	CompanyUUID uuid.UUID
	UserID      uuid.UUID
	Name        string
}

type AddDepartmentMemberInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.UUID
	RequestUser    uuid.UUID
	UserUUID       uuid.UUID
	Role           DepartmentMemberRole
}

type UpdateDepartmentMemberRoleInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.UUID
	RequestUser    uuid.UUID
	UserUUID       uuid.UUID
	Role           DepartmentMemberRole
}

type UpdateDepartmentMemberStatusInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.UUID
	RequestUser    uuid.UUID
	UserUUID       uuid.UUID
	Status         MembershipStatus
}

type UpdateDepartmentInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.UUID
	RequestUser    uuid.UUID
	Name           string
}

type DeleteDepartmentInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.UUID
	RequestUser    uuid.UUID
}

type DepartmentMembersOverview struct {
	Department Department
	Members    []DepartmentMember
}
