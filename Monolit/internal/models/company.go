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
	CompanyMemberRoleDeputy   CompanyMemberRole = "company_deputy"
	CompanyMemberRoleEmployee CompanyMemberRole = "employee"
)

// ManagesCompany reports whether the role runs the company day to day. The
// deputy shares the owner's rights except the owner-only actions.
func (r CompanyMemberRole) ManagesCompany() bool {
	return r == CompanyMemberRoleManager || r == CompanyMemberRoleDeputy
}

type MembershipStatus string

const (
	MembershipStatusActive MembershipStatus = "active"
	MembershipStatusLeft   MembershipStatus = "left"
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
	Deputy           *CompanyMember
	CompanyEmployees []CompanyMember
	Departments      []DepartmentMembersOverview
}

type AssignCompanyDeputyInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
}

type RevokeCompanyDeputyInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
}

type RemoveCompanyMemberInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
	Reason      string
}

// CompanyMembershipRestriction keeps an exclusion decision visible for a while
// so a department leader cannot silently invite the person back.
type CompanyMembershipRestriction struct {
	ID                uuid.UUID
	CompanyUUID       uuid.UUID
	UserUUID          uuid.UUID
	Kind              string
	Reason            *string
	CreatedByUserUUID uuid.UUID
	CreatedAt         time.Time
	ExpiresAt         time.Time
}

const CompanyRestrictionExcludedByManager = "excluded_by_manager"

type DepartmentTransferStatus string

const (
	DepartmentTransferStatusPending  DepartmentTransferStatus = "pending"
	DepartmentTransferStatusApproved DepartmentTransferStatus = "approved"
	DepartmentTransferStatusRejected DepartmentTransferStatus = "rejected"
	DepartmentTransferStatusCanceled DepartmentTransferStatus = "canceled"
	DepartmentTransferStatusExpired  DepartmentTransferStatus = "expired"
)

type DepartmentTransferRequest struct {
	ID                  uuid.UUID
	CompanyUUID         uuid.UUID
	UserUUID            uuid.UUID
	FromDepartmentUUID  uuid.NullUUID
	ToDepartmentUUID    uuid.UUID
	RequestedByUserUUID uuid.UUID
	Reason              *string
	Status              DepartmentTransferStatus
	DecidedByUserUUID   uuid.NullUUID
	DecidedAt           *time.Time
	DecisionComment     *string
	LockVersion         int64
	CreatedAt           time.Time
	ExpiresAt           time.Time
}

type CreateDepartmentTransferInput struct {
	CompanyUUID      uuid.UUID
	ToDepartmentUUID uuid.UUID
	UserUUID         uuid.UUID
	RequestUser      uuid.UUID
	Reason           string
}

type DecideDepartmentTransferInput struct {
	RequestUUID uuid.UUID
	RequestUser uuid.UUID
	Approve     bool
	Comment     string
	LockVersion int64
}

type CompanyOwnershipTransferStatus string

const (
	CompanyOwnershipTransferPending  CompanyOwnershipTransferStatus = "pending"
	CompanyOwnershipTransferAccepted CompanyOwnershipTransferStatus = "accepted"
	CompanyOwnershipTransferDeclined CompanyOwnershipTransferStatus = "declined"
	CompanyOwnershipTransferCanceled CompanyOwnershipTransferStatus = "canceled"
	CompanyOwnershipTransferExpired  CompanyOwnershipTransferStatus = "expired"
)

type CompanyOwnershipTransfer struct {
	ID           uuid.UUID
	CompanyUUID  uuid.UUID
	FromUserUUID uuid.UUID
	ToUserUUID   uuid.UUID
	Status       CompanyOwnershipTransferStatus
	Reason       *string
	DecidedAt    *time.Time
	LockVersion  int64
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

type CreateCompanyOwnershipTransferInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	ToUserUUID  uuid.UUID
	Reason      string
}

type DecideCompanyOwnershipTransferInput struct {
	TransferUUID uuid.UUID
	RequestUser  uuid.UUID
	Accept       bool
	LockVersion  int64
}
