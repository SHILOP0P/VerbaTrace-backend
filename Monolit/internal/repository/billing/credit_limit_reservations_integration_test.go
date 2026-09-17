//go:build integration

package billing

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// A limit that only counts what has already been billed is no limit at all:
// operations that are still running are money already committed, and leaving
// them out let a handful of parallel calls walk past the cap together.
func (s *RepositorySuite) TestCreditLimitCountsReservationsNotOnlySettledOperations() {
	ownerID := s.createUser("reservation-limit-owner@example.com")
	companyID := s.createCompany(ownerID)

	subscription, err := s.repository.UpsertSubscription(s.ctx, models.UpsertSubscriptionInput{
		PlanCode: models.PlanCodeBusinessPro,
		UserUUID: uuid.NullUUID{UUID: ownerID, Valid: true},
		Status:   models.SubscriptionStatusActive,
		StartsAt: time.Now().UTC().Add(-time.Hour),
	})
	s.Require().NoError(err)

	limit := int64(100)
	s.Require().NoError(s.repository.SetCompanyCreditLimit(s.ctx, models.SetCreditLimitInput{
		CompanyUUID:  companyID,
		UserUUID:     ownerID,
		LimitCredits: &limit,
	}))

	now := time.Now().UTC()
	reserve := func(key string, maximum int64) error {
		_, reserveErr := s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
			OperationUUID:  uuid.New(),
			CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
			OperationType:  "analysis",
			Environment:    "production",
			Provider:       "openrouter",
			Model:          "openai/gpt-5-mini",
			IdempotencyKey: key,
			MaximumCharge:  maximum,
		}, now)
		return reserveErr
	}

	// The first reservation fits inside the cap and is never settled.
	s.Require().NoError(reserve("first", 100))

	// The second one has to see the first as spent even though nothing was
	// charged yet.
	s.Require().ErrorIs(reserve("second", 1), models.ErrCompanyCreditLimitExceeded)
}

// The cap is hard: an operation that would overshoot it is refused before it
// starts. Comparing only what was already spent let one expensive call overshoot
// the cap by any amount, because nothing ever refused that call.
func (s *RepositorySuite) TestCreditLimitRefusesAnOperationThatWouldOvershootIt() {
	ownerID := s.createUser("hard-limit-owner@example.com")
	companyID := s.createCompany(ownerID)

	subscription, err := s.repository.UpsertSubscription(s.ctx, models.UpsertSubscriptionInput{
		PlanCode: models.PlanCodeBusinessPro,
		UserUUID: uuid.NullUUID{UUID: ownerID, Valid: true},
		Status:   models.SubscriptionStatusActive,
		StartsAt: time.Now().UTC().Add(-time.Hour),
	})
	s.Require().NoError(err)

	limit := int64(1000)
	s.Require().NoError(s.repository.SetCompanyCreditLimit(s.ctx, models.SetCreditLimitInput{
		CompanyUUID:  companyID,
		UserUUID:     ownerID,
		LimitCredits: &limit,
	}))

	now := time.Now().UTC()
	reserve := func(key string, maximum int64) error {
		_, reserveErr := s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
			OperationUUID:  uuid.New(),
			CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
			OperationType:  "analysis",
			Environment:    "production",
			Provider:       "openrouter",
			Model:          "openai/gpt-5-mini",
			IdempotencyKey: key,
			MaximumCharge:  maximum,
		}, now)
		return reserveErr
	}

	// 999 of 1000 committed: the cap is not exhausted yet.
	s.Require().NoError(reserve("almost-full", 999))

	// An operation worth 50 000 no longer fits, and used to be allowed because
	// only 999 had been spent so far.
	s.Require().ErrorIs(reserve("expensive", 50_000), models.ErrCompanyCreditLimitExceeded)

	// What still fits is still allowed.
	s.Require().NoError(reserve("last-credit", 1))
}

// The window follows the owner's plan, so spending under a previous window does
// not count against the current one even when both fall in the same month.
func (s *RepositorySuite) TestCreditLimitWindowFollowsTheSubscription() {
	ownerID := s.createUser("period-limit-owner@example.com")
	companyID := s.createCompany(ownerID)

	// A plan bought 31 days ago: its second window opened yesterday.
	start := time.Now().UTC().Add(-31 * 24 * time.Hour)
	subscription, err := s.repository.UpsertSubscription(s.ctx, models.UpsertSubscriptionInput{
		PlanCode: models.PlanCodeBusinessPro,
		UserUUID: uuid.NullUUID{UUID: ownerID, Valid: true},
		Status:   models.SubscriptionStatusActive,
		StartsAt: start,
	})
	s.Require().NoError(err)

	limit := int64(10)
	s.Require().NoError(s.repository.SetCompanyCreditLimit(s.ctx, models.SetCreditLimitInput{
		CompanyUUID:  companyID,
		UserUUID:     ownerID,
		LimitCredits: &limit,
	}))

	// Spending recorded inside the first window, which has since closed.
	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID:  uuid.New(),
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		OperationType:  "analysis",
		Environment:    "production",
		Provider:       "openrouter",
		Model:          "openai/gpt-5-mini",
		IdempotencyKey: "previous-window",
		MaximumCharge:  10,
	}, start.Add(20*24*time.Hour))
	s.Require().NoError(err)

	// The current window is empty, so the cap is available again.
	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID:  uuid.New(),
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		OperationType:  "analysis",
		Environment:    "production",
		Provider:       "openrouter",
		Model:          "openai/gpt-5-mini",
		IdempotencyKey: "current-window",
		MaximumCharge:  10,
	}, time.Now().UTC())
	s.Require().NoError(err)
}
