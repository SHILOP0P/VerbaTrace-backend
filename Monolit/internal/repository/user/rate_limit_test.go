//go:build integration

package user

import (
	"time"

	"verbatrace/monolit/internal/models"
)

// Five wrong passwords in a quarter of an hour lock the account for the same
// quarter of an hour; a successful login forgets the failures.
func (s *RepositorySuite) TestLoginRateLimitBlocksAndClears() {
	rule := models.LoginAccountRateLimit
	subject := "rate-limit@example.com"
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	for attempt := 1; attempt < rule.Threshold; attempt++ {
		blocked, _, err := s.repository.RegisterRateLimitFailure(s.ctx, rule, subject, now)
		s.Require().NoError(err)
		s.Require().False(blocked)
	}

	blocked, until, err := s.repository.RegisterRateLimitFailure(s.ctx, rule, subject, now)
	s.Require().NoError(err)
	s.Require().True(blocked)
	s.Require().Equal(now.Add(rule.Block), until.UTC())

	blocked, _, err = s.repository.RateLimitBlocked(s.ctx, rule.Scope, subject, now.Add(time.Minute))
	s.Require().NoError(err)
	s.Require().True(blocked)

	// The block expires on its own.
	blocked, _, err = s.repository.RateLimitBlocked(s.ctx, rule.Scope, subject, now.Add(rule.Block+time.Minute))
	s.Require().NoError(err)
	s.Require().False(blocked)

	s.Require().NoError(s.repository.ClearRateLimit(s.ctx, rule.Scope, subject))
	blocked, _, err = s.repository.RateLimitBlocked(s.ctx, rule.Scope, subject, now)
	s.Require().NoError(err)
	s.Require().False(blocked)
}

// Attempts spread beyond the window never add up to a block.
func (s *RepositorySuite) TestLoginRateLimitWindowSlides() {
	rule := models.LoginAccountRateLimit
	subject := "slow-guesser@example.com"
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	for attempt := range rule.Threshold + 2 {
		blocked, _, err := s.repository.RegisterRateLimitFailure(s.ctx, rule, subject, now.Add(time.Duration(attempt)*(rule.Window+time.Minute)))
		s.Require().NoError(err)
		s.Require().False(blocked)
	}
}
