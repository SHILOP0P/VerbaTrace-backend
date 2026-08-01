package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func NotificationsModelToAPI(result models.ListNotificationsResult) dto.NotificationsResponse {
	items := make([]dto.NotificationResponse, len(result.Notifications))
	for i, notification := range result.Notifications {
		items[i] = NotificationModelToAPI(notification)
	}
	return dto.NotificationsResponse{
		Notifications: items,
		UnreadCount:   result.UnreadCount,
	}
}

func NotificationModelToAPI(notification models.Notification) dto.NotificationResponse {
	var entityUUID *string
	if notification.EntityUUID.Valid {
		value := notification.EntityUUID.UUID.String()
		entityUUID = &value
	}
	var readAt *string
	if notification.ReadAt != nil {
		value := notification.ReadAt.UTC().Format(time.RFC3339)
		readAt = &value
	}
	return dto.NotificationResponse{
		ID:         notification.ID.String(),
		Type:       string(notification.Type),
		Title:      notification.Title,
		Body:       notification.Body,
		EntityType: notification.EntityType,
		EntityUUID: entityUUID,
		ReadAt:     readAt,
		CreatedAt:  notification.CreatedAt.UTC().Format(time.RFC3339),
	}
}
