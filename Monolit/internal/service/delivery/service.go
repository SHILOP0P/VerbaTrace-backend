// Package delivery tells people about what happened in their calls: the weekly
// digest and the alert about a failed call. The bell in the app is a real
// channel; mail and Telegram go through a queue whose only sender for now is
// the mock one, which logs the message.
package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Kinds of events a person can subscribe to.
const (
	KindWeeklyDigest      = "weekly_digest"
	KindCriticalCallAlert = "critical_call_alert"
)

// Channels an event can go through.
const (
	ChannelInApp    = "in_app"
	ChannelEmail    = "email"
	ChannelTelegram = "telegram"
)

var (
	ErrInvalidSubscription = errors.New("invalid notification subscription")
	ErrChannelUnavailable  = errors.New("notification channel is not available")
)

type Service struct {
	db        *sql.DB
	log       logger.Logger
	publicURL string
	now       func() time.Time
}

func NewService(db *sql.DB, log logger.Logger, publicURL string) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, log: log, publicURL: strings.TrimRight(publicURL, "/"), now: func() time.Time { return time.Now().UTC() }}
}

// Link is an address in the app for a message outside it.
func (s *Service) Link(path string) string { return s.publicURL + path }

// Event is one thing to tell one person.
type Event struct {
	Kind         string
	User         uuid.UUID
	Notification models.NotificationType
	Title, Body  string
	EntityType   string
	EntityID     uuid.NullUUID
	// Company is the company the event is about; a frozen company sends nothing.
	Company uuid.NullUUID
	// Text is the whole message for channels outside the app: numbers, names of
	// employees and criteria, and links, never quotes of the conversation.
	Text string
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Deliver sends events once per mark. The mark says the event happened, so a
// second run, a retried job or a repeated analysis does nothing. The bell is
// written in the same transaction as the mark.
func (s *Service) Deliver(ctx context.Context, mark string, events []Event) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin delivery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT INTO delivery_marks (dedupe_key) VALUES ($1) ON CONFLICT DO NOTHING`, mark)
	if err != nil {
		return false, fmt.Errorf("mark delivery: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	for _, event := range events {
		channels, err := enabledChannels(ctx, tx, event.User, event.Kind)
		if err != nil {
			return false, err
		}
		for _, channel := range channels {
			if channel == ChannelInApp {
				if err := notify(ctx, tx, event); err != nil {
					return false, err
				}
				continue
			}
			payload := map[string]any{"title": event.Title, "text": event.Text}
			if event.Company.Valid {
				payload["company_uuid"] = event.Company.UUID.String()
			}
			if _, err := Enqueue(ctx, tx, Outbound{User: event.User, Channel: channel, Kind: event.Kind, DedupeKey: mark + ":" + event.User.String() + ":" + channel, Payload: payload}); err != nil {
				return false, err
			}
		}
	}
	return true, tx.Commit()
}

func notify(ctx context.Context, tx execer, event Event) error {
	var entityType any
	var entityID any
	if event.EntityType != "" && event.EntityID.Valid {
		entityType, entityID = event.EntityType, event.EntityID.UUID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notifications (notification_uuid, user_uuid, type, title, body, entity_type, entity_uuid, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())`, uuid.New(), event.User, string(event.Notification), event.Title, event.Body, entityType, entityID); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}
	return nil
}

// enabledChannels are the channels a person wants an event in. The bell is on
// unless switched off; mail and Telegram need a subscription and a confirmed
// address.
func enabledChannels(ctx context.Context, q execer, user uuid.UUID, kind string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT ch.channel
		FROM (VALUES ('in_app'), ('email'), ('telegram')) AS ch(channel)
		LEFT JOIN notification_subscriptions s ON s.user_uuid = $1 AND s.kind = $2 AND s.channel = ch.channel
		WHERE CASE WHEN ch.channel = 'in_app' THEN COALESCE(s.enabled, true)
		           ELSE COALESCE(s.enabled, false) AND EXISTS (
		               SELECT 1 FROM notification_channels c WHERE c.user_uuid = $1 AND c.channel = ch.channel AND c.status = 'verified')
		      END
		ORDER BY ch.channel`, user, kind)
	if err != nil {
		return nil, fmt.Errorf("read subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var channels []string
	for rows.Next() {
		var channel string
		if rows.Scan(&channel) == nil {
			channels = append(channels, channel)
		}
	}
	return channels, rows.Err()
}

// Subscription is one cell of the "event × channel" settings.
type Subscription struct {
	Kind      string `json:"kind"`
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
}

var (
	kinds    = []string{KindWeeklyDigest, KindCriticalCallAlert}
	channels = []string{ChannelInApp, ChannelEmail, ChannelTelegram}
)

// Subscriptions is the whole matrix for a person. Only the app can be switched
// on today: mail and Telegram have no sender yet.
func (s *Service) Subscriptions(ctx context.Context, user uuid.UUID) ([]Subscription, error) {
	stored := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, channel, enabled FROM notification_subscriptions WHERE user_uuid = $1`, user)
	if err != nil {
		return nil, fmt.Errorf("read subscriptions: %w", err)
	}
	for rows.Next() {
		var kind, channel string
		var enabled bool
		if rows.Scan(&kind, &channel, &enabled) == nil {
			stored[kind+"/"+channel] = enabled
		}
	}
	_ = rows.Close()
	out := make([]Subscription, 0, len(kinds)*len(channels))
	for _, kind := range kinds {
		for _, channel := range channels {
			enabled, ok := stored[kind+"/"+channel]
			if !ok {
				enabled = channel == ChannelInApp
			}
			available := channel == ChannelInApp
			out = append(out, Subscription{Kind: kind, Channel: channel, Enabled: enabled && available, Available: available})
		}
	}
	return out, nil
}

// SetSubscriptions stores the cells a person changed.
func (s *Service) SetSubscriptions(ctx context.Context, user uuid.UUID, items []Subscription) ([]Subscription, error) {
	for _, item := range items {
		if !contains(kinds, item.Kind) || !contains(channels, item.Channel) {
			return nil, ErrInvalidSubscription
		}
		if item.Enabled && item.Channel != ChannelInApp {
			return nil, ErrChannelUnavailable
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_subscriptions (user_uuid, kind, channel, enabled) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_uuid, kind, channel) DO UPDATE SET enabled = EXCLUDED.enabled`, user, item.Kind, item.Channel, item.Enabled); err != nil {
			return nil, fmt.Errorf("save subscription: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Subscriptions(ctx, user)
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Outbound is a message for a channel outside the app.
type Outbound struct {
	User      uuid.UUID
	Channel   string
	Kind      string
	DedupeKey string
	Payload   map[string]any
}

// Enqueue puts a message in the queue once per dedupe key.
func Enqueue(ctx context.Context, q execer, message Outbound) (bool, error) {
	payload, err := json.Marshal(message.Payload)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx, `
		INSERT INTO outbound_messages (message_uuid, user_uuid, channel, kind, dedupe_key, payload_json, status)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, 'pending') ON CONFLICT (dedupe_key) DO NOTHING`,
		uuid.New(), message.User, message.Channel, message.Kind, message.DedupeKey, string(payload))
	if err != nil {
		return false, fmt.Errorf("enqueue message: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
