package auth

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
)

// rateLimiter is implemented by the user repository. It stays a separate
// interface so the auth service keeps working in tests that never touch it.
type rateLimiter interface {
	RateLimitBlocked(ctx context.Context, scope, subject string, now time.Time) (bool, time.Time, error)
	RegisterRateLimitFailure(ctx context.Context, rule models.RateLimitRule, subject string, now time.Time) (bool, time.Time, error)
	ClearRateLimit(ctx context.Context, scope, subject string) error
}

// ensureNotBlocked refuses an attempt while the subject is locked out. It never
// says which counter tripped: that would tell an attacker what to change.
func (s *Service) ensureNotBlocked(ctx context.Context, rule models.RateLimitRule, subject string) error {
	limiter, ok := s.userRepository.(rateLimiter)
	if !ok || strings.TrimSpace(subject) == "" {
		return nil
	}
	blocked, _, err := limiter.RateLimitBlocked(ctx, rule.Scope, subject, s.now().UTC())
	if err != nil {
		return err
	}
	if blocked {
		return models.ErrTooManyAttempts
	}

	return nil
}

func (s *Service) registerFailure(ctx context.Context, rule models.RateLimitRule, subject string) {
	limiter, ok := s.userRepository.(rateLimiter)
	if !ok || strings.TrimSpace(subject) == "" {
		return
	}
	_, _, _ = limiter.RegisterRateLimitFailure(ctx, rule, subject, s.now().UTC())
}

func (s *Service) clearFailures(ctx context.Context, rule models.RateLimitRule, subject string) {
	limiter, ok := s.userRepository.(rateLimiter)
	if !ok || strings.TrimSpace(subject) == "" {
		return
	}
	_ = limiter.ClearRateLimit(ctx, rule.Scope, subject)
}

func rateSubject(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
