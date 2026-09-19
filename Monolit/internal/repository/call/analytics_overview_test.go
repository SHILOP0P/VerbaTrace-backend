//go:build integration

package call

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) TestGetAnalyticsOverviewAggregatesVisibleFilteredCalls() {
	company, manager := s.createCompanyWithManager()
	uploader := s.createUser(uuid.NewString() + "@example.com")
	outsider := s.createUser(uuid.NewString() + "@example.com")
	baseTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	analyzed := testCall(uploader.ID)
	analyzed.Status = models.CallStatusAnalyzed
	analyzed.VisibilityScope = models.CallVisibilityScopeCompany
	analyzed.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	analyzed.DurationSeconds = 60
	analyzed.CreatedAt = baseTime
	_, err := s.repository.CreateCall(s.ctx, analyzed)
	s.Require().NoError(err)
	s.insertFacts(analyzed.ID, baseTime, 90)

	failed := testCall(uploader.ID)
	failed.Status = models.CallStatusFailed
	failed.VisibilityScope = models.CallVisibilityScopeCompany
	failed.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	failed.DurationSeconds = 120
	failed.CreatedAt = baseTime.Add(time.Hour)
	_, err = s.repository.CreateCall(s.ctx, failed)
	s.Require().NoError(err)

	outOfRange := testCall(uploader.ID)
	outOfRange.Status = models.CallStatusNew
	outOfRange.VisibilityScope = models.CallVisibilityScopeCompany
	outOfRange.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	outOfRange.CreatedAt = baseTime.Add(48 * time.Hour)
	_, err = s.repository.CreateCall(s.ctx, outOfRange)
	s.Require().NoError(err)

	from := baseTime.Add(-time.Minute)
	to := baseTime.Add(2 * time.Hour)
	overview, err := s.repository.GetAnalyticsOverview(s.ctx, models.AnalyticsOverviewInput{
		UserID:          manager.ID,
		VisibilityScope: models.CallVisibilityScopeCompany,
		CompanyUUID:     uuid.NullUUID{UUID: company.ID, Valid: true},
		From:            &from,
		To:              &to,
	})
	s.Require().NoError(err)
	s.Require().Equal(2, overview.CallsTotal)
	s.Require().Zero(overview.CallsCreatedToday)
	s.Require().Equal(1, overview.CallsWithTranscription)
	s.Require().Equal(1, overview.CallsAnalyzed)
	s.Require().Equal(1, overview.CallsFailed)
	s.Require().NotNil(overview.AverageDurationSeconds)
	s.Require().Equal(90, *overview.AverageDurationSeconds)
	s.Require().NotNil(overview.AverageQualityScore)
	s.Require().Equal(4.5, *overview.AverageQualityScore)
	s.Require().NotNil(overview.AverageScore)
	s.Require().Equal(90.0, *overview.AverageScore)
	s.Require().Equal(100, overview.ScoreScale)
	s.Require().Equal(1, overview.ScoreDistribution.Excellent)
	s.Require().Empty(overview.TopTopics, "v2 breakdowns have no v3 source")
	s.Require().Len(overview.Charts.CallsByDay, 1)
	s.Require().Len(overview.Charts.AnalyzedByDay, 1)
	s.Require().Len(overview.Charts.ScoreByDay, 1)
	s.Require().Len(overview.Charts.DurationByDay, 1)

	outsiderOverview, err := s.repository.GetAnalyticsOverview(s.ctx, models.AnalyticsOverviewInput{
		UserID: outsider.ID,
		From:   &from,
		To:     &to,
	})
	s.Require().NoError(err)
	s.Require().Zero(outsiderOverview.CallsTotal)
}

// The overview reads scores and weak criteria from the facts, the same numbers
// the analytics pages show, whatever schema the analysis had.
func (s *RepositorySuite) TestGetAnalyticsOverviewReadsCriteriaFromFacts() {
	company, manager := s.createCompanyWithManager()
	uploader := s.createUser(uuid.NewString() + "@example.com")
	baseTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	budget, nextStep, greeting := uuid.New(), uuid.New(), uuid.New()

	calls := make([]uuid.UUID, 2)
	for i := range calls {
		call := testCall(uploader.ID)
		call.Status = models.CallStatusAnalyzed
		call.VisibilityScope = models.CallVisibilityScopeCompany
		call.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
		call.CreatedAt = baseTime.Add(time.Duration(i) * time.Hour)
		_, err := s.repository.CreateCall(s.ctx, call)
		s.Require().NoError(err)
		calls[i] = call.ID
	}
	s.insertFacts(calls[0], baseTime, 80)
	s.insertCriterionFact(calls[0], budget, "missed", 0)
	s.insertCriterionFact(calls[0], nextStep, "missed", 0)
	s.insertCriterionFact(calls[0], greeting, "not_applicable", -1)
	s.insertFacts(calls[1], baseTime.Add(time.Hour), 60)
	s.insertCriterionFact(calls[1], budget, "partially_met", 50)
	s.insertCriterionFact(calls[1], nextStep, "missed", 0)

	overview, err := s.repository.GetAnalyticsOverview(s.ctx, models.AnalyticsOverviewInput{
		UserID:          manager.ID,
		VisibilityScope: models.CallVisibilityScopeCompany,
		CompanyUUID:     uuid.NullUUID{UUID: company.ID, Valid: true},
	})
	s.Require().NoError(err)
	s.Require().NotNil(overview.AverageScore)
	s.Require().Equal(70.0, *overview.AverageScore)
	s.Require().Equal(1, overview.ScoreDistribution.Weak)
	s.Require().Equal(1, overview.ScoreDistribution.Good)

	s.Require().Len(overview.CriteriaSummary, 3)
	needs := findCriterionSummary(overview.CriteriaSummary, budget.String())
	s.Require().NotNil(needs)
	s.Require().Equal(25.0, *needs.AverageScore)
	s.Require().Equal(1, needs.Missed)
	s.Require().Equal(1, needs.PartiallyMet)
	s.Require().Equal(2, needs.CallsCount)
	na := findCriterionSummary(overview.CriteriaSummary, greeting.String())
	s.Require().NotNil(na)
	s.Require().Nil(na.AverageScore)
	s.Require().Equal(1, na.NotApplicable)
	s.Require().Len(overview.TopWeakCriteria, 2)
	s.Require().Equal(nextStep.String(), overview.TopWeakCriteria[0].Code)
	s.Require().Empty(overview.TopIssueCodes)
	s.Require().Empty(overview.BusinessOutcomes)
}

func findCriterionSummary(items []models.AnalyticsCriterionSummary, code string) *models.AnalyticsCriterionSummary {
	for i := range items {
		if items[i].Code == code {
			return &items[i]
		}
	}
	return nil
}

func (s *RepositorySuite) insertFacts(callID uuid.UUID, at time.Time, overall int) {
	s.T().Helper()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO analytics_call_facts (call_uuid, analysis_uuid, occurred_at, schema_version, scorecard_mode, ai_overall_score, coverage_status)
		VALUES ($1, $2, $3, 3, 'fixed', $4, 'complete')`, callID, uuid.New(), at, overall)
	s.Require().NoError(err)
}

// insertCriterionFact stores a criterion result; a negative score is none.
func (s *RepositorySuite) insertCriterionFact(callID, key uuid.UUID, status string, score int) {
	s.T().Helper()
	var value any
	if score >= 0 {
		value = score
	}
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO analytics_criterion_facts (call_uuid, criterion_key, instruction_uuid, scorecard_uuid, item_id, ai_status, ai_score, weight, is_critical)
		VALUES ($1, $2, $3, $4, 'r1', $5, $6, 1, false)`, callID, key, uuid.New(), uuid.New(), status, value)
	s.Require().NoError(err)
}
