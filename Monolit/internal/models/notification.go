package models

import (
	"time"

	"github.com/google/uuid"
)

type NotificationType string

const (
	NotificationTypeInvitation              NotificationType = "invitation"
	NotificationTypeReportReady             NotificationType = "report_ready"
	NotificationTypeSubscription            NotificationType = "subscription"
	NotificationTypeProcessingFailed        NotificationType = "processing_failed"
	NotificationTypeActionAssigned          NotificationType = "action_assigned"
	NotificationTypeActionReassigned        NotificationType = "action_reassigned"
	NotificationTypeActionDueChanged        NotificationType = "action_due_changed"
	NotificationTypeActionCancelled         NotificationType = "action_cancelled"
	NotificationTypeActionCompleted         NotificationType = "action_completed"
	NotificationTypeActionTransferRequested NotificationType = "action_transfer_requested"
	NotificationTypeActionTransferApproved  NotificationType = "action_transfer_approved"
	NotificationTypeActionTransferRejected  NotificationType = "action_transfer_rejected"
	NotificationTypeActionReminder          NotificationType = "action_reminder"
	NotificationTypeActionGraceStarted      NotificationType = "action_grace_started"
	NotificationTypeActionOverdue           NotificationType = "action_overdue"
	NotificationTypeActionAssignmentInvalid NotificationType = "action_assignment_invalid"
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
