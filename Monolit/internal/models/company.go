package models

import (
	"time"

	"github.com/google/uuid"
)

type Company struct {
	ID              uuid.UUID
	Name            string
	Tag             string
	ManagerUserUUID uuid.UUID
	MemberLimit     int
	CreatedAt       time.Time
	DeletedAt       *time.Time
}

type CompanyMember struct {
	CompanyUUID uuid.UUID
	UserUUID    uuid.UUID
	Username    string
	FullName    string
	FullSurname string
	JobTitle    *string
	Role        CompanyMemberRole
	Status      MembershipStatus
	CreatedAt   time.Time
}

type CompanyMemberRole string

const (
	CompanyMemberRoleManager  CompanyMemberRole = "company_manager"
	CompanyMemberRoleEmployee CompanyMemberRole = "employee"
)

type MembershipStatus string

const (
	MembershipStatusActive    MembershipStatus = "active"
	MembershipStatusSuspended MembershipStatus = "suspended"
	MembershipStatusLeft      MembershipStatus = "left"
)

type CreateCompanyInput struct {
	Name          string
	ManagerUserID uuid.UUID
}

type AddCompanyMemberInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
	Role        CompanyMemberRole
}

type UpdateCompanyMemberRoleInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
	Role        CompanyMemberRole
}

type UpdateCompanyMemberStatusInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
	Status      MembershipStatus
}

type UpdateCompanyMemberJobTitleInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
	JobTitle    *string
}

type UpdateCompanyInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	Name        string
}

type UpdateCompanyTagInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	Tag         string
}

type DeleteCompanyInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
}

type ListCompanyMembersInput struct {
	CompanyUUID    uuid.UUID
	RequestUser    uuid.UUID
	Status         *MembershipStatus
	Role           *string
	DepartmentUUID uuid.UUID
	Query          string
	Limit          int
	Offset         int
}

type CompanyMemberDepartment struct {
	DepartmentUUID uuid.UUID
	DepartmentName string
	Role           DepartmentMemberRole
	Status         MembershipStatus
}

type CompanyMemberListItem struct {
	UserUUID    uuid.UUID
	Email       string
	Username    string
	FullName    string
	FullSurname string
	JobTitle    *string
	CompanyRole CompanyMemberRole
	Status      MembershipStatus
	Departments []CompanyMemberDepartment
	CreatedAt   time.Time
}

type CompanyMembersResult struct {
	Members []CompanyMemberListItem
	Total   int
	Limit   int
	Offset  int
}

type CompanyMembersOverview struct {
	CompanyUUID      uuid.UUID
	Manager          *CompanyMember
	CompanyEmployees []CompanyMember
	Departments      []DepartmentMembersOverview
}
