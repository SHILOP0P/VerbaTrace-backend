package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const (
	defaultNotificationLimit = 20
	notificationColumns      = `notification_uuid, user_uuid, type, title, body, entity_type, entity_uuid, read_at, created_at`
)

func (r *Repository) Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error) {
	query := `
	INSERT INTO notifications (
		notification_uuid, user_uuid, type, title, body, entity_type, entity_uuid, created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING ` + notificationColumns

	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	row := r.db.QueryRowContext(ctx, query, uuid.New(), input.UserUUID, input.Type, input.Title, input.Body, input.EntityType, input.EntityUUID, createdAt)
	notification, err := scanNotification(row)
	if err != nil {
		return models.Notification{}, fmt.Errorf("create notification: %w", err)
	}
	return notification, nil
}

func (r *Repository) List(ctx context.Context, input models.ListNotificationsInput) (models.ListNotificationsResult, error) {
	if input.Limit <= 0 {
		input.Limit = defaultNotificationLimit
	}

	where := "user_uuid = $1"
	if input.UnreadOnly {
		where += " AND read_at IS NULL"
	}

	query := fmt.Sprintf(`
	SELECT `+notificationColumns+`, COUNT(*) OVER() AS unread_count
	FROM notifications
	WHERE %s
	ORDER BY created_at DESC
	LIMIT $2 OFFSET $3
	`, where)

	rows, err := r.db.QueryContext(ctx, query, input.UserUUID, input.Limit, input.Offset)
	if err != nil {
		return models.ListNotificationsResult{}, fmt.Errorf("list notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()

	notifications := make([]models.Notification, 0)
	unreadCount := 0
	for rows.Next() {
		notification, rowUnreadCount, err := scanNotificationWithUnreadCount(rows)
		if err != nil {
			return models.ListNotificationsResult{}, fmt.Errorf("list notifications: %w", err)
		}
		notifications = append(notifications, notification)
		unreadCount = rowUnreadCount
	}
	if err := rows.Err(); err != nil {
		return models.ListNotificationsResult{}, fmt.Errorf("list notifications: %w", err)
	}

	if len(notifications) == 0 {
		unreadCount, err = r.CountUnread(ctx, input.UserUUID)
		if err != nil {
			return models.ListNotificationsResult{}, err
		}
	} else if !input.UnreadOnly {
		unreadCount, err = r.CountUnread(ctx, input.UserUUID)
		if err != nil {
			return models.ListNotificationsResult{}, err
		}
	}

	return models.ListNotificationsResult{
		Notifications: notifications,
		UnreadCount:   unreadCount,
		Limit:         input.Limit,
		Offset:        input.Offset,
	}, nil
}

func (r *Repository) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_uuid = $1 AND read_at IS NULL`, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

func (r *Repository) MarkRead(ctx context.Context, id uuid.UUID, userID uuid.UUID, readAt time.Time) (models.Notification, error) {
	query := `
	UPDATE notifications
	SET read_at = COALESCE(read_at, $3)
	WHERE notification_uuid = $1
	  AND user_uuid = $2
	RETURNING ` + notificationColumns

	notification, err := scanNotification(r.db.QueryRowContext(ctx, query, id, userID, readAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Notification{}, models.ErrNotificationNotFound
		}
		return models.Notification{}, fmt.Errorf("mark notification read: %w", err)
	}
	return notification, nil
}

func (r *Repository) MarkUnread(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Notification, error) {
	query := `UPDATE notifications SET read_at = NULL WHERE notification_uuid = $1 AND user_uuid = $2 RETURNING ` + notificationColumns
	notification, err := scanNotification(r.db.QueryRowContext(ctx, query, id, userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Notification{}, models.ErrNotificationNotFound
		}
		return models.Notification{}, fmt.Errorf("mark notification unread: %w", err)
	}
	return notification, nil
}

func (r *Repository) MarkAllRead(ctx context.Context, userID uuid.UUID, readAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
	UPDATE notifications
	SET read_at = $2
	WHERE user_uuid = $1
	  AND read_at IS NULL
	`, userID, readAt)
	if err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNotification(row scanner) (models.Notification, error) {
	var notification models.Notification
	if err := row.Scan(
		&notification.ID,
		&notification.UserUUID,
		&notification.Type,
		&notification.Title,
		&notification.Body,
		&notification.EntityType,
		&notification.EntityUUID,
		&notification.ReadAt,
		&notification.CreatedAt,
	); err != nil {
		return models.Notification{}, err
	}
	return notification, nil
}

func scanNotificationWithUnreadCount(row scanner) (models.Notification, int, error) {
	var notification models.Notification
	var unreadCount int
	if err := row.Scan(
		&notification.ID,
		&notification.UserUUID,
		&notification.Type,
		&notification.Title,
		&notification.Body,
		&notification.EntityType,
		&notification.EntityUUID,
		&notification.ReadAt,
		&notification.CreatedAt,
		&unreadCount,
	); err != nil {
		return models.Notification{}, 0, err
	}
	return notification, unreadCount, nil
}
