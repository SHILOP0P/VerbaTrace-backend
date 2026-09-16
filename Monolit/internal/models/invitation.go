package models

import (
	"time"

	"github.com/google/uuid"
)

type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "pending"
	InvitationStatusAccepted InvitationStatus = "accepted"
	InvitationStatusDeclined InvitationStatus = "declined"
	InvitationStatusCanceled InvitationStatus = "canceled"
	InvitationStatusExpired  InvitationStatus = "expired"
)

type InvitationApprovalStatus string

const (
	InvitationApprovalNotRequired InvitationApprovalStatus = "not_required"
	InvitationApprovalPending     InvitationApprovalStatus = "pending"
	InvitationApprovalApproved    InvitationApprovalStatus = "approved"
	InvitationApprovalRejected    InvitationApprovalStatus = "rejected"
)

type MembershipInvitation struct {
	ID                     uuid.UUID
	CompanyUUID            uuid.UUID
	DepartmentUUID         uuid.NullUUID
	InvitedUserUUID        uuid.UUID
	InvitedByUserUUID      uuid.UUID
	CompanyRole            CompanyMemberRole
	DepartmentRole         *DepartmentMemberRole
	Status                 InvitationStatus
	ApprovalStatus         InvitationApprovalStatus
	ApprovalDecidedByUUID  uuid.NullUUID
	ApprovalDecidedAt      *time.Time
	ExpiresAt              time.Time
	RespondedAt            *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
	TargetAlreadyEngaged   bool
	TargetCompanyIsCurrent bool
}

// Visible reports whether the invited user may see and answer the invitation.
// An invite waiting for the deputy's approval stays hidden until it is decided.
func (i MembershipInvitation) Visible() bool {
	return i.ApprovalStatus == "" || i.ApprovalStatus == InvitationApprovalNotRequired || i.ApprovalStatus == InvitationApprovalApproved
}

type DecideInvitationApprovalInput struct {
	CompanyUUID    uuid.UUID
	InvitationUUID uuid.UUID
	RequestUser    uuid.UUID
	Approve        bool
}

type ListCompanyInvitationsInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	Status      InvitationStatus
	Since       *time.Time
}

type CreateCompanyInvitationInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	UserUUID    uuid.UUID
	Username    string
	Role        CompanyMemberRole
	// AcknowledgeCurrentMembership is the answer to the "this person already
	// works somewhere" alert shown to the person who invites.
	AcknowledgeCurrentMembership bool
}

type CreateDepartmentInvitationInput struct {
	CompanyUUID                  uuid.UUID
	DepartmentUUID               uuid.UUID
	RequestUser                  uuid.UUID
	UserUUID                     uuid.UUID
	Username                     string
	Role                         DepartmentMemberRole
	AcknowledgeCurrentMembership bool
}

type ListUserInvitationsInput struct {
	UserUUID uuid.UUID
	Status   InvitationStatus
}

type AcceptInvitationInput struct {
	InvitationUUID uuid.UUID
	RequestUser    uuid.UUID
	// ConfirmTransfer is the answer to the "you are leaving your current
	// company" alert. Without it a move between companies is refused.
	ConfirmTransfer bool
}

type AcceptInvitationCommand struct {
	InvitationUUID  uuid.UUID
	ConfirmTransfer bool
	Now             time.Time
}

type DeclineInvitationInput struct {
	InvitationUUID uuid.UUID
	RequestUser    uuid.UUID
}

type CancelInvitationInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.NullUUID
	InvitationUUID uuid.UUID
	RequestUser    uuid.UUID
}
