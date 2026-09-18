package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Message is what a sender gets from the queue.
type Message struct {
	ID      uuid.UUID
	User    uuid.UUID
	Channel string
	Kind    string
	// Address is where the channel delivers: a confirmed address of the
	// person, or the one the message itself names (a password reset goes to
	// the account's address).
	Address string
	Payload json.RawMessage
}

// Sender delivers one message through a channel outside the app.
type Sender interface {
	Name() string
	Send(ctx context.Context, message Message) error
}

// MockSender stands in for mail and Telegram until they are connected: it logs
// that a message would have gone and counts it as sent.
type MockSender struct{ log logger.Logger }

func NewMockSender(log logger.Logger) *MockSender {
	if log == nil {
		log = logger.NewNop()
	}
	return &MockSender{log: log}
}

func (m *MockSender) Name() string { return "mock" }

func (m *MockSender) Send(ctx context.Context, message Message) error {
	m.log.Info(ctx, "outbound message (mock sender)", zap.String("message_id", message.ID.String()),
		zap.String("channel", message.Channel), zap.String("kind", message.Kind), zap.Int("payload_bytes", len(message.Payload)))
	return nil
}

// NewSender picks the sender named by NOTIFY_SENDER. Only the mock exists.
func NewSender(name string, log logger.Logger) (Sender, error) {
	switch name {
	case "", "mock":
		return NewMockSender(log), nil
	}
	return nil, fmt.Errorf("unknown notification sender %q", name)
}

const (
	maxAttempts  = 5
	leaseTime    = 5 * time.Minute
	firstBackoff = time.Minute
)

var errCompanyFrozen = errors.New("company is frozen")

// Worker sends the queue: messages are leased so two replicas never send the
// same one, retried with a growing delay and given up after five attempts.
type Worker struct {
	db       *sql.DB
	sender   Sender
	log      logger.Logger
	interval time.Duration
	batch    int
}

func NewWorker(db *sql.DB, sender Sender, log logger.Logger, interval time.Duration, batch int) *Worker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if batch <= 0 {
		batch = 50
	}
	if log == nil {
		log = logger.NewNop()
	}
	return &Worker{db: db, sender: sender, log: log, interval: interval, batch: batch}
}

func (w *Worker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			w.RunOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

// RunOnce sends what is due now and reports how many messages it took.
func (w *Worker) RunOnce(ctx context.Context) int {
	rows, err := w.db.QueryContext(ctx, `
		UPDATE outbound_messages m SET status = 'sending', attempts = m.attempts + 1, lease_until = now() + make_interval(secs => $2)
		WHERE m.message_uuid IN (
			SELECT message_uuid FROM outbound_messages
			WHERE (status IN ('pending','failed') AND available_at <= now()) OR (status = 'sending' AND lease_until < now())
			ORDER BY available_at, message_uuid
			FOR UPDATE SKIP LOCKED LIMIT $1)
		RETURNING m.message_uuid, m.user_uuid, m.channel, m.kind, m.payload_json::text, m.attempts,
		          COALESCE((SELECT c.address FROM notification_channels c WHERE c.user_uuid = m.user_uuid AND c.channel = m.channel AND c.status = 'verified'), m.payload_json->>'address', ''),
		          COALESCE((SELECT co.lifecycle_state <> 'active' FROM companies co WHERE co.company_uuid::text = m.payload_json->>'company_uuid'), false)`,
		w.batch, leaseTime.Seconds())
	if err != nil {
		w.log.Warn(ctx, "outbound queue claim failed", zap.Error(err))
		return 0
	}
	type claimed struct {
		message  Message
		attempts int
		frozen   bool
	}
	var batch []claimed
	for rows.Next() {
		var c claimed
		var payload string
		if err := rows.Scan(&c.message.ID, &c.message.User, &c.message.Channel, &c.message.Kind, &payload, &c.attempts, &c.message.Address, &c.frozen); err == nil {
			c.message.Payload = json.RawMessage(payload)
			batch = append(batch, c)
		}
	}
	_ = rows.Close()
	for _, c := range batch {
		err := errCompanyFrozen
		if !c.frozen {
			err = w.sender.Send(ctx, c.message)
		}
		w.finish(ctx, c.message.ID, c.attempts, err)
	}
	return len(batch)
}

func (w *Worker) finish(ctx context.Context, id uuid.UUID, attempts int, sendErr error) {
	var err error
	switch {
	case sendErr == nil:
		_, err = w.db.ExecContext(ctx, `UPDATE outbound_messages SET status = 'sent', sent_at = now(), lease_until = NULL, last_error = NULL WHERE message_uuid = $1`, id)
	case errors.Is(sendErr, errCompanyFrozen) || attempts >= maxAttempts:
		// A frozen company sends nothing, and it would be stale when unfrozen.
		_, err = w.db.ExecContext(ctx, `UPDATE outbound_messages SET status = 'dead', lease_until = NULL, last_error = $2 WHERE message_uuid = $1`, id, sendErr.Error())
	default:
		delay := firstBackoff << (attempts - 1)
		_, err = w.db.ExecContext(ctx, `UPDATE outbound_messages SET status = 'failed', lease_until = NULL, last_error = $2, available_at = now() + make_interval(secs => $3) WHERE message_uuid = $1`,
			id, sendErr.Error(), delay.Seconds())
	}
	if err != nil {
		w.log.Warn(ctx, "outbound message not updated", zap.String("message_id", id.String()), zap.Error(err))
	}
}
