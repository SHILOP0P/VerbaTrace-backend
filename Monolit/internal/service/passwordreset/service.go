// Package passwordreset lets a person who forgot their password set a new one
// through a link sent to the account's address. The link is a one-time token
// kept only as a hash; asking for a new one voids the old ones.
package passwordreset

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"verbatrace/monolit/internal/auth/password"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/delivery"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const tokenTTL = 30 * time.Minute

// The counters are mild: they stop a flood of letters to one address or from
// one place without hindering a person who asked twice.
var (
	AccountRateLimit = models.RateLimitRule{Scope: "reset_account", Threshold: 3, Window: time.Hour, Block: time.Hour}
	IPRateLimit      = models.RateLimitRule{Scope: "reset_ip", Threshold: 10, Window: time.Hour, Block: time.Hour}
)

var ErrInvalidToken = errors.New("password reset token is invalid or expired")

// RateLimiter counts requests per address and per network address.
type RateLimiter interface {
	RateLimitBlocked(ctx context.Context, scope, subject string, now time.Time) (bool, time.Time, error)
	RegisterRateLimitFailure(ctx context.Context, rule models.RateLimitRule, subject string, now time.Time) (bool, time.Time, error)
}

type Service struct {
	db        *sql.DB
	limiter   RateLimiter
	pepper    string
	publicURL string
	enabled   bool
	log       logger.Logger
	now       func() time.Time
}

// NewService builds the reset. enabled is false while letters only reach the
// mock sender: the page is hidden then, since nobody would get the link.
func NewService(db *sql.DB, limiter RateLimiter, pepper, publicURL string, enabled bool, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, limiter: limiter, pepper: pepper, publicURL: strings.TrimRight(publicURL, "/"), enabled: enabled, log: log, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Enabled() bool { return s.enabled }

// Request sends a reset link to the account with this address. It answers the
// same whether the address exists or the counters are exhausted, so the page
// cannot be used to learn who has an account.
func (s *Service) Request(ctx context.Context, email, ip string) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return
	}
	if s.blocked(ctx, AccountRateLimit, email) || s.blocked(ctx, IPRateLimit, ip) {
		return
	}
	s.count(ctx, AccountRateLimit, email)
	s.count(ctx, IPRateLimit, ip)
	if err := s.request(ctx, email, ip); err != nil {
		s.log.Warn(ctx, "password reset not requested", zap.Error(err))
	}
}

func (s *Service) request(ctx context.Context, email, ip string) error {
	var user uuid.UUID
	var address string
	err := s.db.QueryRowContext(ctx, `SELECT user_uuid, email FROM users WHERE lower(email) = $1`, email).Scan(&user, &address)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := hashToken(token)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// A new request voids the links sent before it.
	if _, err := tx.ExecContext(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE user_uuid = $1 AND used_at IS NULL`, user); err != nil {
		return fmt.Errorf("void previous reset tokens: %w", err)
	}
	// The address the request came from is kept beside the token, so a reset the
	// owner did not ask for can be traced.
	if _, err := tx.ExecContext(ctx, `INSERT INTO password_reset_tokens (token_hash, user_uuid, expires_at, requested_ip) VALUES ($1, $2, $3, NULLIF($4, ''))`, hash, user, s.now().Add(tokenTTL), ip); err != nil {
		return fmt.Errorf("store reset token: %w", err)
	}
	link := s.publicURL + "/reset-password?token=" + url.QueryEscape(token)
	// The letter carries the token, so its payload is wiped once it is sent.
	if _, err := delivery.Enqueue(ctx, tx, delivery.Outbound{User: user, Channel: delivery.ChannelEmail, Kind: "password_reset", DedupeKey: "reset:" + hash,
		Payload: map[string]any{
			"address": address, "sensitive": true, "title": "Восстановление пароля VerbaTrace",
			"text": "Вы запросили смену пароля. Ссылка действует 30 минут: " + link + ". Если вы этого не делали, просто удалите письмо.",
		}}); err != nil {
		return err
	}
	return tx.Commit()
}

// Confirm sets the new password by a token from the letter. The token works
// once; every session of the account ends, as after a password change.
func (s *Service) Confirm(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < 8 {
		return models.ErrInvalidUserInput
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidToken
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var user uuid.UUID
	var expires time.Time
	var used sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT user_uuid, expires_at, used_at FROM password_reset_tokens WHERE token_hash = $1 FOR UPDATE`, hashToken(token)).Scan(&user, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (used.Valid || !expires.After(s.now()))) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	hash, err := password.Hash(newPassword, s.pepper)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = $2 WHERE user_uuid = $1`, user, hash); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE token_hash = $1`, hashToken(token)); err != nil {
		return err
	}
	// Access tokens die with their sessions: the version bump makes the ones
	// already issued fail at once.
	if _, err := tx.ExecContext(ctx, `
		UPDATE refresh_sessions SET revoked_at = now(), revoked_reason = 'password_reset', access_version = access_version + 1
		WHERE user_uuid = $1 AND revoked_at IS NULL`, user); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notifications (notification_uuid, user_uuid, type, title, body, created_at)
		VALUES ($1, $2, 'password_changed', 'Пароль изменён', 'Если это были не вы, смените пароль и завершите другие сессии', now())`, uuid.New(), user); err != nil {
		return fmt.Errorf("notify password change: %w", err)
	}
	return tx.Commit()
}

func (s *Service) blocked(ctx context.Context, rule models.RateLimitRule, subject string) bool {
	if s.limiter == nil || strings.TrimSpace(subject) == "" {
		return false
	}
	blocked, _, err := s.limiter.RateLimitBlocked(ctx, rule.Scope, subject, s.now())
	return err == nil && blocked
}

func (s *Service) count(ctx context.Context, rule models.RateLimitRule, subject string) {
	if s.limiter == nil || strings.TrimSpace(subject) == "" {
		return
	}
	_, _, _ = s.limiter.RegisterRateLimitFailure(ctx, rule, subject, s.now())
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
