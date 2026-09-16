package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"
)

// RateLimitBlocked answers whether this subject is currently locked out. The
// counters live in Postgres; they move to Redis with the microservices.
func (r *Repository) RateLimitBlocked(ctx context.Context, scope, subject string, now time.Time) (bool, time.Time, error) {
	var blockedUntil sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT blocked_until FROM auth_rate_counters WHERE scope=$1 AND subject=$2`, scope, subject).Scan(&blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, fmt.Errorf("read rate counter: %w", err)
	}
	if blockedUntil.Valid && now.Before(blockedUntil.Time) {
		return true, blockedUntil.Time, nil
	}

	return false, time.Time{}, nil
}

// RegisterRateLimitFailure counts one failed attempt and blocks the subject once
// it crosses the threshold inside the window.
func (r *Repository) RegisterRateLimitFailure(ctx context.Context, rule models.RateLimitRule, subject string, now time.Time) (bool, time.Time, error) {
	query := `
	INSERT INTO auth_rate_counters(scope,subject,window_start,attempts,updated_at)
	VALUES($1,$2,$3,1,$3)
	ON CONFLICT (scope,subject) DO UPDATE
	SET window_start = CASE WHEN auth_rate_counters.window_start <= $4 THEN $3 ELSE auth_rate_counters.window_start END,
	    attempts = CASE WHEN auth_rate_counters.window_start <= $4 THEN 1 ELSE auth_rate_counters.attempts + 1 END,
	    blocked_until = CASE
	        WHEN auth_rate_counters.window_start <= $4 THEN NULL
	        WHEN auth_rate_counters.attempts + 1 >= $5 THEN $6
	        ELSE auth_rate_counters.blocked_until
	    END,
	    updated_at = $3
	RETURNING attempts, blocked_until`

	var attempts int
	var blockedUntil sql.NullTime
	windowStart := now.Add(-rule.Window)
	if err := r.db.QueryRowContext(ctx, query, rule.Scope, subject, now, windowStart, rule.Threshold, now.Add(rule.Block)).Scan(&attempts, &blockedUntil); err != nil {
		return false, time.Time{}, fmt.Errorf("count rate attempt: %w", err)
	}
	if blockedUntil.Valid && now.Before(blockedUntil.Time) {
		return true, blockedUntil.Time, nil
	}

	return false, time.Time{}, nil
}

// ClearRateLimit forgets the failures of a subject after a success.
func (r *Repository) ClearRateLimit(ctx context.Context, scope, subject string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM auth_rate_counters WHERE scope=$1 AND subject=$2`, scope, subject); err != nil {
		return fmt.Errorf("clear rate counter: %w", err)
	}
	return nil
}
