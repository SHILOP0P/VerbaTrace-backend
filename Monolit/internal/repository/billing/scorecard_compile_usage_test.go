//go:build integration

package billing

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Compiling a scorecard is billed as an analysis but belongs to no call. A
// retried reservation must not book it twice, and the credit history shows the
// compile of one scorecard as one charge, however many times the model was
// asked, instead of losing it among call analyses.
func (s *RepositorySuite) TestScorecardCompileIsReservedOnceAndListedAsOneCharge() {
	userID := s.createUser("scorecard-compile@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	now := time.Now().UTC()
	scorecardID := uuid.New()

	reserve := func(key string, charge int64) {
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
		_, err = s.repository.SettleCredits(s.ctx, models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: charge, ProviderUsageJSON: []byte(`{}`)}, now.Add(time.Second))
		s.Require().NoError(err)
	}
	reserve("scorecard:"+scorecardID.String()+":1:try0", 150)
	reserve("scorecard:"+scorecardID.String()+":1:try1", 120)

	var operations int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM usage_operations WHERE idempotency_key LIKE $1`, "scorecard:"+scorecardID.String()+":%").Scan(&operations))
	s.Require().Equal(2, operations)

	dashboard, err := s.repository.GetCreditDashboard(s.ctx, subscription, now.Add(-time.Hour), now.Add(time.Hour))
	s.Require().NoError(err)
	var compiles []models.CreditWalletEntry
	for _, entry := range dashboard.WalletEntries {
		if entry.Reason == models.ScorecardCompileUsageMode {
			compiles = append(compiles, entry)
		}
	}
	s.Require().Len(compiles, 1, "both attempts are one charge in the credit history")
	s.Require().Equal(scorecardID, compiles[0].TransactionUUID)
	s.Require().EqualValues(-270, compiles[0].Credits)
}
