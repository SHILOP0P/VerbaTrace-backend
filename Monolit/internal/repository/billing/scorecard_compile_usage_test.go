//go:build integration

package billing

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Compiling a scorecard is billed as an analysis but belongs to no call. A
// retried reservation must not book it twice, and the customer must see it in
// the usage history on its own line instead of losing it among call analyses.
func (s *RepositorySuite) TestScorecardCompileIsReservedOnceAndListedOnItsOwn() {
	userID := s.createUser("scorecard-compile@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	now := time.Now().UTC()

	key := "scorecard:" + uuid.NewString() + ":1:c1"
	operationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
	input := models.ReserveCreditsInput{
		OperationUUID: operationID, OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", Mode: models.ScorecardCompileUsageMode,
		IdempotencyKey: key, MaximumCharge: 400,
	}
	first, err := s.repository.ReserveCredits(s.ctx, subscription, input, now)
	s.Require().NoError(err)
	again, err := s.repository.ReserveCredits(s.ctx, subscription, input, now)
	s.Require().NoError(err)
	s.Require().Equal(first, again)
	_, err = s.repository.SettleCredits(s.ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: 150, ProviderUsageJSON: []byte(`{}`)}, now.Add(time.Second))
	s.Require().NoError(err)

	var operations int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM usage_operations WHERE idempotency_key = $1`, key).Scan(&operations))
	s.Require().Equal(1, operations)

	dashboard, err := s.repository.GetCreditDashboard(s.ctx, subscription, now.Add(-time.Hour), now.Add(time.Hour))
	s.Require().NoError(err)
	var compile *models.CreditWalletEntry
	for i := range dashboard.WalletEntries {
		if dashboard.WalletEntries[i].TransactionUUID == operationID {
			compile = &dashboard.WalletEntries[i]
		}
	}
	s.Require().NotNil(compile, "the compile is listed in the usage history")
	s.Require().Equal(models.ScorecardCompileUsageMode, compile.Reason)
	s.Require().EqualValues(-150, compile.Credits)
}
