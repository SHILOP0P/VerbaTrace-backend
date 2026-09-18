package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) GetAnalyticsOverview(ctx context.Context, input model.AnalyticsOverviewInput) (model.AnalyticsOverview, error) {
	where, args := buildListFilters(model.ListCallsInput{
		UserID:          input.UserID,
		VisibilityScope: input.VisibilityScope,
		CompanyUUID:     input.CompanyUUID,
		DepartmentUUID:  input.DepartmentUUID,
		From:            input.From,
		To:              input.To,
		FolderUUID:      input.FolderUUID,
	})

	query := fmt.Sprintf(`
	SELECT COUNT(*)::int,
	       COUNT(*) FILTER (WHERE c.created_at >= date_trunc('day', NOW() AT TIME ZONE 'UTC'))::int,
	       COUNT(*) FILTER (WHERE c.status = 'new')::int,
	       COUNT(*) FILTER (WHERE c.status = 'processing')::int,
	       COUNT(*) FILTER (WHERE c.status = 'transcribed')::int,
	       COUNT(*) FILTER (WHERE c.status IN ('transcribed', 'analyzed'))::int,
	       COUNT(*) FILTER (WHERE c.status = 'analyzed')::int,
	       COUNT(*) FILTER (WHERE c.status = 'failed')::int,
	       AVG(c.duration_seconds)::float8
	FROM calls c
	WHERE %s
	`, where)

	var overview model.AnalyticsOverview
	var averageDuration sql.NullFloat64
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&overview.CallsTotal,
		&overview.CallsCreatedToday,
		&overview.CallsNew,
		&overview.CallsProcessing,
		&overview.CallsTranscribed,
		&overview.CallsWithTranscription,
		&overview.CallsAnalyzed,
		&overview.CallsFailed,
		&averageDuration,
	)
	if err != nil {
		return model.AnalyticsOverview{}, fmt.Errorf("get analytics overview: %w", err)
	}

	if averageDuration.Valid {
		rounded := int(math.Round(averageDuration.Float64))
		overview.AverageDurationSeconds = &rounded
	}
	overview.QualityScoreScale = 5
	overview.ScoreScale = 100
	overview.TopTopics = []model.AnalyticsTopicCount{}

	if err := r.fillAnalysisAggregates(ctx, &overview, where, args); err != nil {
		return model.AnalyticsOverview{}, err
	}

	return overview, nil
}

// DepartmentCompany answers uuid.Nil for a department that does not exist, so
// an unknown filter is treated like no company filter and simply finds nothing.
func (r *Repository) DepartmentCompany(ctx context.Context, departmentID uuid.UUID) (uuid.UUID, error) {
	var companyID uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT company_uuid FROM departments WHERE department_uuid=$1`, departmentID).Scan(&companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get department company: %w", err)
	}
	return companyID, nil
}

// analyticsCallRow is one call of the overview with the score its facts carry.
type analyticsCallRow struct {
	Status          model.CallStatus
	DurationSeconds int
	CreatedAt       time.Time
	Score           sql.NullFloat64
}

type analyticsAccumulator struct {
	callsByDay    map[string]int
	analyzedByDay map[string]int
	durationByDay map[string][]int
	qualityByDay  map[string][]float64
	scoreByDay    map[string][]float64
	criteria      map[string]*criterionAccumulator

	scores            []float64
	scoreDistribution model.AnalyticsScoreDistribution
}

type criterionAccumulator struct {
	code          string
	title         string
	scores        []float64
	met           int
	partiallyMet  int
	missed        int
	unclear       int
	notApplicable int
	calls         map[string]struct{}
}

// fillAnalysisAggregates reads scores from the analytics facts, the same
// numbers the analytics pages show, and never parses result_json. The v2
// breakdowns without a v3 counterpart (issue codes, outcomes, next steps,
// topics, risks) stay empty until the page stops asking for them.
func (r *Repository) fillAnalysisAggregates(ctx context.Context, overview *model.AnalyticsOverview, where string, args []any) error {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
	SELECT c.status, c.duration_seconds, c.created_at,
	       CASE WHEN c.status = 'analyzed' AND NOT COALESCE(f.is_internal, false) THEN f.overall_score END::float8
	FROM calls c
	LEFT JOIN analytics_call_facts f ON f.call_uuid = c.call_uuid
	WHERE %s
	`, where), args...)
	if err != nil {
		return fmt.Errorf("get analytics details: %w", err)
	}
	acc := analyticsAccumulator{
		callsByDay:    map[string]int{},
		analyzedByDay: map[string]int{},
		durationByDay: map[string][]int{},
		qualityByDay:  map[string][]float64{},
		scoreByDay:    map[string][]float64{},
		criteria:      map[string]*criterionAccumulator{},
	}
	for rows.Next() {
		var row analyticsCallRow
		if err := rows.Scan(&row.Status, &row.DurationSeconds, &row.CreatedAt, &row.Score); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan analytics details: %w", err)
		}
		acc.addCall(row)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan analytics details: %w", err)
	}

	criteria, err := r.db.QueryContext(ctx, fmt.Sprintf(`
	SELECT c.call_uuid::text, cf.criterion_key::text, COALESCE(t.title, ''), cf.score::float8,
	       CASE
	           WHEN cf.human_decision = 'not_applicable' OR (cf.human_score IS NULL AND cf.ai_status = 'not_applicable') THEN 'not_applicable'
	           WHEN cf.score IS NULL THEN 'unclear'
	           WHEN cf.score >= 63 THEN 'met'
	           WHEN cf.score >= 13 THEN 'partially_met'
	           ELSE 'missed'
	       END
	FROM calls c
	JOIN analytics_call_facts f ON f.call_uuid = c.call_uuid AND NOT f.is_internal
	JOIN analytics_criterion_facts cf ON cf.call_uuid = f.call_uuid
	LEFT JOIN LATERAL (
	    SELECT sc.title FROM instruction_scorecard_criteria sc JOIN instruction_scorecards s ON s.scorecard_uuid = sc.scorecard_uuid
	    WHERE sc.criterion_key = cf.criterion_key OR sc.criterion_key IN (SELECT alias_key FROM criterion_key_aliases WHERE canonical_key = cf.criterion_key)
	    ORDER BY s.is_current DESC, s.created_at DESC LIMIT 1
	) t ON true
	WHERE c.status = 'analyzed' AND %s
	`, where), args...)
	if err != nil {
		return fmt.Errorf("get analytics criteria: %w", err)
	}
	defer func() { _ = criteria.Close() }()
	for criteria.Next() {
		var callID, key, title, status string
		var score sql.NullFloat64
		if err := criteria.Scan(&callID, &key, &title, &score, &status); err != nil {
			return fmt.Errorf("scan analytics criteria: %w", err)
		}
		c := acc.criteria[key]
		if c == nil {
			c = &criterionAccumulator{code: key, title: title, calls: map[string]struct{}{}}
			acc.criteria[key] = c
		}
		c.calls[callID] = struct{}{}
		c.addStatus(status)
		if score.Valid {
			c.scores = append(c.scores, score.Float64)
		}
	}
	if err := criteria.Err(); err != nil {
		return fmt.Errorf("scan analytics criteria: %w", err)
	}

	acc.apply(overview)
	return nil
}

func (a *analyticsAccumulator) addCall(row analyticsCallRow) {
	day := row.CreatedAt.UTC().Format("2006-01-02")
	a.callsByDay[day]++
	if row.Status == model.CallStatusAnalyzed {
		a.analyzedByDay[day]++
	}
	if row.DurationSeconds > 0 {
		a.durationByDay[day] = append(a.durationByDay[day], row.DurationSeconds)
	}
	if row.Score.Valid {
		score := clampScore(row.Score.Float64)
		a.scores = append(a.scores, score)
		a.scoreByDay[day] = append(a.scoreByDay[day], score)
		a.qualityByDay[day] = append(a.qualityByDay[day], score/20)
		a.addScoreDistribution(score)
	}
}

func (a *analyticsAccumulator) apply(overview *model.AnalyticsOverview) {
	overview.Charts = model.AnalyticsCharts{
		CallsByDay:    countMapToPoints(a.callsByDay),
		AnalyzedByDay: countMapToPoints(a.analyzedByDay),
		QualityByDay:  averageFloatMapToQualityPoints(a.qualityByDay),
		ScoreByDay:    averageFloatMapToScorePoints(a.scoreByDay),
		DurationByDay: averageIntMapToDurationPoints(a.durationByDay),
		RisksByDay:    []model.AnalyticsCountPoint{},
	}
	if len(a.scores) > 0 {
		averageScore := roundFloat(averageFloat(a.scores), 1)
		averageQuality := roundFloat(averageScore/20, 1)
		overview.AverageScore = &averageScore
		overview.AverageQualityScore = &averageQuality
	}
	overview.ScoreDistribution = a.scoreDistribution
	overview.CriteriaSummary = criteriaSummary(a.criteria)
	overview.TopWeakCriteria = topWeakCriteria(a.criteria, 5)
	overview.TopIssueCodes = []model.AnalyticsCodeCount{}
	overview.BusinessOutcomes = []model.AnalyticsStatusCount{}
	overview.NextStepSummary = model.AnalyticsNextStepSummary{}
	overview.TopTopics = []model.AnalyticsTopicCount{}
}

func (a *analyticsAccumulator) addScoreDistribution(score float64) {
	switch {
	case score < 50:
		a.scoreDistribution.Critical++
	case score < 65:
		a.scoreDistribution.Weak++
	case score < 80:
		a.scoreDistribution.Normal++
	case score < 90:
		a.scoreDistribution.Good++
	default:
		a.scoreDistribution.Excellent++
	}
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return roundFloat(score, 1)
}

func (a *criterionAccumulator) addStatus(status string) {
	switch status {
	case "met":
		a.met++
	case "partially_met":
		a.partiallyMet++
	case "missed":
		a.missed++
	case "unclear":
		a.unclear++
	case "not_applicable":
		a.notApplicable++
	}
}

func criteriaSummary(values map[string]*criterionAccumulator) []model.AnalyticsCriterionSummary {
	keys := sortedKeys(values)
	items := make([]model.AnalyticsCriterionSummary, 0, len(keys))
	for _, key := range keys {
		acc := values[key]
		items = append(items, model.AnalyticsCriterionSummary{
			Code:          acc.code,
			Title:         acc.title,
			AverageScore:  averageScorePtr(acc.scores),
			Met:           acc.met,
			PartiallyMet:  acc.partiallyMet,
			Missed:        acc.missed,
			Unclear:       acc.unclear,
			NotApplicable: acc.notApplicable,
			CallsCount:    len(acc.calls),
		})
	}
	return items
}

func topWeakCriteria(values map[string]*criterionAccumulator, limit int) []model.AnalyticsWeakCriterion {
	items := make([]model.AnalyticsWeakCriterion, 0, len(values))
	for _, acc := range values {
		avg := averageScorePtr(acc.scores)
		if avg == nil {
			continue
		}
		items = append(items, model.AnalyticsWeakCriterion{
			Code:              acc.code,
			Title:             acc.title,
			AverageScore:      avg,
			MissedCount:       acc.missed,
			PartiallyMetCount: acc.partiallyMet,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := *items[i].AverageScore, *items[j].AverageScore
		if left != right {
			return left < right
		}
		if items[i].MissedCount != items[j].MissedCount {
			return items[i].MissedCount > items[j].MissedCount
		}
		if items[i].PartiallyMetCount != items[j].PartiallyMetCount {
			return items[i].PartiallyMetCount > items[j].PartiallyMetCount
		}
		return items[i].Code < items[j].Code
	})
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func averageScorePtr(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	average := roundFloat(averageFloat(values), 1)
	return &average
}

func countMapToPoints(values map[string]int) []model.AnalyticsCountPoint {
	dates := sortedKeys(values)
	points := make([]model.AnalyticsCountPoint, 0, len(dates))
	for _, date := range dates {
		points = append(points, model.AnalyticsCountPoint{Date: date, Count: values[date]})
	}
	return points
}

func averageFloatMapToQualityPoints(values map[string][]float64) []model.AnalyticsQualityPoint {
	dates := sortedKeys(values)
	points := make([]model.AnalyticsQualityPoint, 0, len(dates))
	for _, date := range dates {
		points = append(points, model.AnalyticsQualityPoint{
			Date:                date,
			AverageQualityScore: roundFloat(averageFloat(values[date]), 1),
		})
	}
	return points
}

func averageFloatMapToScorePoints(values map[string][]float64) []model.AnalyticsScorePoint {
	dates := sortedKeys(values)
	points := make([]model.AnalyticsScorePoint, 0, len(dates))
	for _, date := range dates {
		points = append(points, model.AnalyticsScorePoint{
			Date:         date,
			AverageScore: roundFloat(averageFloat(values[date]), 1),
		})
	}
	return points
}

func averageIntMapToDurationPoints(values map[string][]int) []model.AnalyticsDurationPoint {
	dates := sortedKeys(values)
	points := make([]model.AnalyticsDurationPoint, 0, len(dates))
	for _, date := range dates {
		points = append(points, model.AnalyticsDurationPoint{
			Date:                   date,
			AverageDurationSeconds: averageInt(values[date]),
		})
	}
	return points
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func averageFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func averageInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	var sum int
	for _, value := range values {
		sum += value
	}
	return int(math.Round(float64(sum) / float64(len(values))))
}

func roundFloat(value float64, precision int) float64 {
	scale := math.Pow(10, float64(precision))
	return math.Round(value*scale) / scale
}
