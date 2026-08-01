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
	s.insertDoneAnalysis(analyzed.ID, `{
		"quality_score": 4.5,
		"topics": ["Интеграция", "Договор"],
		"risks": ["Нет бюджета"],
		"manager_quality": {
			"issues": ["Перебивал"],
			"recommendations": ["Уточнять сроки"]
		},
		"next_steps": ["Отправить КП"]
	}`)

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
	s.Require().NotEmpty(overview.TopTopics)
	s.Require().NotNil(overview.RisksCount)
	s.Require().Equal(2, *overview.RisksCount)
	s.Require().NotNil(overview.RecommendationsCount)
	s.Require().Equal(2, *overview.RecommendationsCount)
	s.Require().Len(overview.Charts.CallsByDay, 1)
	s.Require().Len(overview.Charts.AnalyzedByDay, 1)
	s.Require().Len(overview.Charts.QualityByDay, 1)
	s.Require().Len(overview.Charts.ScoreByDay, 1)
	s.Require().Len(overview.Charts.DurationByDay, 1)
	s.Require().Len(overview.Charts.RisksByDay, 1)

	outsiderOverview, err := s.repository.GetAnalyticsOverview(s.ctx, models.AnalyticsOverviewInput{
		UserID: outsider.ID,
		From:   &from,
		To:     &to,
	})
	s.Require().NoError(err)
	s.Require().Zero(outsiderOverview.CallsTotal)
}

func (s *RepositorySuite) TestGetAnalyticsOverviewAggregatesV2AnalysisMetrics() {
	company, manager := s.createCompanyWithManager()
	uploader := s.createUser(uuid.NewString() + "@example.com")
	baseTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	first := testCall(uploader.ID)
	first.Status = models.CallStatusAnalyzed
	first.VisibilityScope = models.CallVisibilityScopeCompany
	first.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	first.CreatedAt = baseTime
	_, err := s.repository.CreateCall(s.ctx, first)
	s.Require().NoError(err)
	s.insertDoneAnalysis(first.ID, `{
		"score": 80,
		"score_scale": 100,
		"criteria_results": [
			{"code": "needs_discovery", "title": "Выявление потребности", "status": "missed", "points_awarded": 0, "points_max": 2},
			{"code": "next_step", "title": "Следующий шаг", "status": "missed", "points_awarded": 0, "points_max": 2},
			{"code": "greeting", "title": "Приветствие", "status": "not_applicable", "points_awarded": 0, "points_max": 1}
		],
		"issue_codes": ["weak_next_step", " ", "price-risk"],
		"business_outcome": {"status": "follow_up_needed"},
		"next_step_quality": {
			"has_next_step": true,
			"specific": true,
			"has_deadline": false,
			"has_responsible_person": true
		}
	}`)

	second := testCall(uploader.ID)
	second.Status = models.CallStatusAnalyzed
	second.VisibilityScope = models.CallVisibilityScopeCompany
	second.CompanyUUID = uuid.NullUUID{UUID: company.ID, Valid: true}
	second.CreatedAt = baseTime.Add(time.Hour)
	_, err = s.repository.CreateCall(s.ctx, second)
	s.Require().NoError(err)
	s.insertDoneAnalysis(second.ID, `{
		"score": 60,
		"score_scale": 100,
		"criteria_results": [
			{"code": "needs_discovery", "title": "Выявление потребности", "status": "partially_met"},
			{"code": "next_step", "title": "Следующий шаг", "status": "missed"}
		],
		"issue_codes": ["weak_next_step", "a_issue"],
		"business_outcome": {"status": "unknown_status"},
		"next_step": "Позвонить завтра"
	}`)

	overview, err := s.repository.GetAnalyticsOverview(s.ctx, models.AnalyticsOverviewInput{
		UserID:          manager.ID,
		VisibilityScope: models.CallVisibilityScopeCompany,
		CompanyUUID:     uuid.NullUUID{UUID: company.ID, Valid: true},
	})
	s.Require().NoError(err)
	s.Require().NotNil(overview.AverageScore)
	s.Require().Equal(70.0, *overview.AverageScore)
	s.Require().NotNil(overview.AverageQualityScore)
	s.Require().Equal(3.5, *overview.AverageQualityScore)
	s.Require().Equal(1, overview.ScoreDistribution.Weak)
	s.Require().Equal(1, overview.ScoreDistribution.Good)

	s.Require().Len(overview.CriteriaSummary, 3)
	needs := findCriterionSummary(overview.CriteriaSummary, "needs_discovery")
	s.Require().NotNil(needs)
	s.Require().NotNil(needs.AverageScore)
	s.Require().Equal(25.0, *needs.AverageScore)
	s.Require().Equal(1, needs.Missed)
	s.Require().Equal(1, needs.PartiallyMet)
	s.Require().Equal(2, needs.CallsCount)
	greeting := findCriterionSummary(overview.CriteriaSummary, "greeting")
	s.Require().NotNil(greeting)
	s.Require().Nil(greeting.AverageScore)
	s.Require().Equal(1, greeting.NotApplicable)
	s.Require().Len(overview.TopWeakCriteria, 2)
	s.Require().Equal("next_step", overview.TopWeakCriteria[0].Code)

	s.Require().Equal([]models.AnalyticsCodeCount{
		{Code: "weak_next_step", Count: 2},
		{Code: "a_issue", Count: 1},
		{Code: "price_risk", Count: 1},
	}, overview.TopIssueCodes)
	s.Require().Equal([]models.AnalyticsStatusCount{
		{Status: "follow_up_needed", Count: 1},
		{Status: "unclear", Count: 1},
	}, overview.BusinessOutcomes)
	s.Require().Equal(2, overview.NextStepSummary.WithNextStep)
	s.Require().Equal(1, overview.NextStepSummary.Specific)
	s.Require().Equal(0, overview.NextStepSummary.WithDeadline)
	s.Require().Equal(1, overview.NextStepSummary.WithResponsiblePerson)
	s.Require().Equal(0, overview.NextStepSummary.Missing)
	s.Require().Len(overview.Charts.ScoreByDay, 1)
	s.Require().Equal(70.0, overview.Charts.ScoreByDay[0].AverageScore)
}

func findCriterionSummary(items []models.AnalyticsCriterionSummary, code string) *models.AnalyticsCriterionSummary {
	for i := range items {
		if items[i].Code == code {
			return &items[i]
		}
	}
	return nil
}

func (s *RepositorySuite) insertDoneAnalysis(callID uuid.UUID, resultJSON string) {
	s.T().Helper()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO call_analyses (
			analysis_uuid,
			call_uuid,
			status,
			provider,
			result_json,
			result_text,
			created_at,
			updated_at
		) VALUES ($1, $2, 'done', 'test', $3::jsonb, 'summary', NOW(), NOW())
	`, uuid.New(), callID, resultJSON)
	s.Require().NoError(err)
}
