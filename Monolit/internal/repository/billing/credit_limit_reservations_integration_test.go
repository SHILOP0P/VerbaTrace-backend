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
