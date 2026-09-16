package models

import (
	"time"

	"github.com/google/uuid"
)

type NotificationType string

const (
	NotificationTypeInvitation                  NotificationType = "invitation"
	NotificationTypeReportReady                 NotificationType = "report_ready"
	NotificationTypeSubscription                NotificationType = "subscription"
	NotificationTypeProcessingFailed            NotificationType = "processing_failed"
	NotificationTypeActionAssigned              NotificationType = "action_assigned"
	NotificationTypeActionReassigned            NotificationType = "action_reassigned"
	NotificationTypeActionDueChanged            NotificationType = "action_due_changed"
	NotificationTypeActionCancelled             NotificationType = "action_cancelled"
	NotificationTypeActionCompleted             NotificationType = "action_completed"
	NotificationTypeActionTransferRequested     NotificationType = "action_transfer_requested"
	NotificationTypeActionTransferApproved      NotificationType = "action_transfer_approved"
	NotificationTypeActionTransferRejected      NotificationType = "action_transfer_rejected"
	NotificationTypeActionReminder              NotificationType = "action_reminder"
	NotificationTypeActionGraceStarted          NotificationType = "action_grace_started"
	NotificationTypeActionOverdue               NotificationType = "action_overdue"
	NotificationTypeActionAssignmentInvalid     NotificationType = "action_assignment_invalid"
	NotificationTypeSupportAccessRequested      NotificationType = "support_access_requested"
	NotificationTypeSupportAccessDecided        NotificationType = "support_access_decided"
	NotificationTypeActionExternalSyncRequested NotificationType = "action_external_sync_requested"
	NotificationTypeActionExternalSyncDecided   NotificationType = "action_external_sync_decided"
	NotificationTypeInvitationApprovalRequested NotificationType = "invitation_approval_requested"
	NotificationTypeInvitationApprovalDecided   NotificationType = "invitation_approval_decided"
	NotificationTypeDepartmentTransferRequested NotificationType = "department_transfer_requested"
	NotificationTypeDepartmentTransferDecided   NotificationType = "department_transfer_decided"
	NotificationTypeDepartmentMemberMoved       NotificationType = "department_member_moved"
	NotificationTypeCompanyOwnerTransferAsked   NotificationType = "company_owner_transfer_requested"
	NotificationTypeCompanyOwnerTransferDecided NotificationType = "company_owner_transfer_decided"
	NotificationTypeCompanyDeputyAssigned       NotificationType = "company_deputy_assigned"
	NotificationTypeCompanyDeputyRevoked        NotificationType = "company_deputy_revoked"
	NotificationTypeCompanyMemberRemoved        NotificationType = "company_member_removed"
)

type Notification struct {
	ID         uuid.UUID
	UserUUID   uuid.UUID
	Type       NotificationType
	Title      string
	Body       string
	EntityType *string
	EntityUUID uuid.NullUUID
	ReadAt     *time.Time
	CreatedAt  time.Time
}

type CreateNotificationInput struct {
	UserUUID   uuid.UUID
	Type       NotificationType
	Title      string
	Body       string
	EntityType *string
	EntityUUID uuid.NullUUID
	CreatedAt  time.Time
}

type ListNotificationsInput struct {
	UserUUID   uuid.UUID
	UnreadOnly bool
	Limit      int
	Offset     int
}

type ListNotificationsResult struct {
	Notifications []Notification
	UnreadCount   int
	Limit         int
	Offset        int
}
