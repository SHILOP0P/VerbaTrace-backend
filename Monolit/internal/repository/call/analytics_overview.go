package call

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	model "verbatrace/monolit/internal/models"
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

type analyticsCallRow struct {
	CallUUID        string
	Status          model.CallStatus
	DurationSeconds int
	CreatedAt       time.Time
	ResultJSON      sql.NullString
}

type analyticsAccumulator struct {
	callsByDay    map[string]int
	analyzedByDay map[string]int
	durationByDay map[string][]int
	qualityByDay  map[string][]float64
	scoreByDay    map[string][]float64
	risksByDay    map[string]int
	topicCounts   map[string]int
	criteria      map[string]*criterionAccumulator
	issueCodes    map[string]int
	outcomes      map[string]int

	scores            []float64
	scoreDistribution model.AnalyticsScoreDistribution
	risksCount        int
	recsCount         int
	analysisSeen      bool
	nextStep          model.AnalyticsNextStepSummary
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

func (r *Repository) fillAnalysisAggregates(ctx context.Context, overview *model.AnalyticsOverview, where string, args []any) error {
	query := fmt.Sprintf(`
	SELECT c.call_uuid::text,
	       c.status,
	       c.duration_seconds,
	       c.created_at,
	       effective_call_analysis_json(ca.analysis_uuid,ca.result_json)::text
	FROM calls c
	LEFT JOIN call_analyses ca
	  ON ca.call_uuid = c.call_uuid
	 AND ca.status = 'done'
	WHERE %s
	`, where)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("get analytics details: %w", err)
	}
	defer func() { _ = rows.Close() }()

	acc := analyticsAccumulator{
		callsByDay:    map[string]int{},
		analyzedByDay: map[string]int{},
		durationByDay: map[string][]int{},
		qualityByDay:  map[string][]float64{},
		scoreByDay:    map[string][]float64{},
		risksByDay:    map[string]int{},
		topicCounts:   map[string]int{},
		criteria:      map[string]*criterionAccumulator{},
		issueCodes:    map[string]int{},
		outcomes:      map[string]int{},
	}

	for rows.Next() {
		var row analyticsCallRow
		if err := rows.Scan(&row.CallUUID, &row.Status, &row.DurationSeconds, &row.CreatedAt, &row.ResultJSON); err != nil {
			return fmt.Errorf("scan analytics details: %w", err)
		}
		acc.addCall(row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan analytics details: %w", err)
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
	if !row.ResultJSON.Valid || strings.TrimSpace(row.ResultJSON.String) == "" {
		return
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(row.ResultJSON.String), &payload); err != nil {
		return
	}
	a.analysisSeen = true

	if row.Status == model.CallStatusAnalyzed {
		if score, ok := extractScore(payload); ok {
			a.scores = append(a.scores, score)
			a.scoreByDay[day] = append(a.scoreByDay[day], score)
			a.qualityByDay[day] = append(a.qualityByDay[day], score/20)
			a.addScoreDistribution(score)
		}
		a.addCriteria(row.CallUUID, payload)
		a.addIssueCodes(payload["issue_codes"])
		a.addBusinessOutcome(payload["business_outcome"])
		a.addNextStepQuality(payload)
	}

	risks := countListValues(payload["risks"]) +
		countListValues(payload["customer_objections"]) +
		countNestedListValues(payload, "manager_quality", "issues")
	a.risksCount += risks
	a.risksByDay[day] += risks

	a.recsCount += countNestedListValues(payload, "manager_quality", "recommendations") +
		countListValues(payload["next_steps"]) +
		countListValues(payload["recommendations"])

	addTopics(a.topicCounts, payload["topics"])
	addTopics(a.topicCounts, payload["top_topics"])
}

func (a *analyticsAccumulator) apply(overview *model.AnalyticsOverview) {
	overview.Charts = model.AnalyticsCharts{
		CallsByDay:    countMapToPoints(a.callsByDay),
		AnalyzedByDay: countMapToPoints(a.analyzedByDay),
		QualityByDay:  averageFloatMapToQualityPoints(a.qualityByDay),
		ScoreByDay:    averageFloatMapToScorePoints(a.scoreByDay),
		DurationByDay: averageIntMapToDurationPoints(a.durationByDay),
		RisksByDay:    countMapToPoints(a.risksByDay),
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
	overview.TopIssueCodes = codeCountMapToCounts(a.issueCodes, 10)
	overview.BusinessOutcomes = statusCountMapToCounts(a.outcomes)
	overview.NextStepSummary = a.nextStep
	if a.analysisSeen {
		risks := a.risksCount
		recs := a.recsCount
		overview.RisksCount = &risks
		overview.RecommendationsCount = &recs
	}
	overview.TopTopics = topicMapToCounts(a.topicCounts, 10)
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

func extractScore(payload map[string]any) (float64, bool) {
	if score, ok := numberValue(payload["score"]); ok && score >= 0 {
		scale, scaleOK := numberValue(payload["score_scale"])
		if scaleOK && scale > 0 {
			return clampScore(score / scale * 100), true
		}
	}
	for _, key := range []string{"quality_score", "overall_score", "manager_score", "score"} {
		score, ok := numberValue(payload[key])
		if !ok || score < 0 {
			continue
		}
		if score > 5 {
			return clampScore(score), true
		}
		return clampScore(score * 20), true
	}
	return 0, false
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

func (a *analyticsAccumulator) addCriteria(callKey string, payload map[string]any) {
	items, ok := payload["criteria_results"].([]any)
	if !ok {
		return
	}
	for _, item := range items {
		criterion, ok := item.(map[string]any)
		if !ok {
			continue
		}
		code, _ := criterion["code"].(string)
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		status, _ := criterion["status"].(string)
		status = strings.TrimSpace(status)
		if !isAllowedCriterionStatus(status) {
			continue
		}
		acc := a.criteria[code]
		if acc == nil {
			acc = &criterionAccumulator{code: code, calls: map[string]struct{}{}}
			a.criteria[code] = acc
		}
		if title, ok := criterion["title"].(string); ok && strings.TrimSpace(title) != "" {
			acc.title = strings.TrimSpace(title)
		}
		acc.calls[callKey] = struct{}{}
		acc.addStatus(status)
		if status == "not_applicable" {
			continue
		}
		if score, ok := criterionScore(criterion, status); ok {
			acc.scores = append(acc.scores, score)
		}
	}
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

func criterionScore(criterion map[string]any, status string) (float64, bool) {
	pointsMax, maxOK := numberValue(criterion["points_max"])
	pointsAwarded, awardedOK := numberValue(criterion["points_awarded"])
	if maxOK && awardedOK && pointsMax > 0 {
		return clampScore(pointsAwarded / pointsMax * 100), true
	}
	switch status {
	case "met":
		return 100, true
	case "partially_met":
		return 50, true
	case "missed", "unclear":
		return 0, true
	default:
		return 0, false
	}
}

func isAllowedCriterionStatus(status string) bool {
	switch status {
	case "met", "partially_met", "missed", "unclear", "not_applicable":
		return true
	default:
		return false
	}
}

func (a *analyticsAccumulator) addIssueCodes(value any) {
	items, ok := value.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		code, ok := item.(string)
		if !ok {
			continue
		}
		code = normalizeIssueCode(code)
		if code != "" {
			a.issueCodes[code]++
		}
	}
}

func normalizeIssueCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "_")
	code = strings.Join(strings.Fields(code), "_")
	return code
}

func (a *analyticsAccumulator) addBusinessOutcome(value any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	status, _ := object["status"].(string)
	status = strings.TrimSpace(status)
	if !isAllowedBusinessOutcome(status) {
		status = "unclear"
	}
	a.outcomes[status]++
}

func isAllowedBusinessOutcome(status string) bool {
	switch status {
	case "success", "follow_up_needed", "no_decision", "lost", "support_resolved", "unclear":
		return true
	default:
		return false
	}
}

func (a *analyticsAccumulator) addNextStepQuality(payload map[string]any) {
	object, ok := payload["next_step_quality"].(map[string]any)
	if ok {
		hasNext := boolValue(object["has_next_step"])
		if hasNext {
			a.nextStep.WithNextStep++
		} else {
			a.nextStep.Missing++
		}
		if boolValue(object["specific"]) {
			a.nextStep.Specific++
		}
		if boolValue(object["has_deadline"]) {
			a.nextStep.WithDeadline++
		}
		if boolValue(object["has_responsible_person"]) {
			a.nextStep.WithResponsiblePerson++
		}
		return
	}
	if hasFallbackNextStep(payload) {
		a.nextStep.WithNextStep++
	} else {
		a.nextStep.Missing++
	}
}

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func hasFallbackNextStep(payload map[string]any) bool {
	if nextStep, ok := payload["next_step"].(string); ok && strings.TrimSpace(nextStep) != "" {
		return true
	}
	return countListValues(payload["next_steps"]) > 0
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

func codeCountMapToCounts(values map[string]int, limit int) []model.AnalyticsCodeCount {
	items := make([]model.AnalyticsCodeCount, 0, len(values))
	for code, count := range values {
		items = append(items, model.AnalyticsCodeCount{Code: code, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Code < items[j].Code
		}
		return items[i].Count > items[j].Count
	})
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func statusCountMapToCounts(values map[string]int) []model.AnalyticsStatusCount {
	keys := sortedKeys(values)
	items := make([]model.AnalyticsStatusCount, 0, len(keys))
	for _, status := range keys {
		items = append(items, model.AnalyticsStatusCount{Status: status, Count: values[status]})
	}
	return items
}

func numberValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func countNestedListValues(payload map[string]any, objectKey string, listKey string) int {
	object, ok := payload[objectKey].(map[string]any)
	if !ok {
		return 0
	}
	return countListValues(object[listKey])
}

func countListValues(value any) int {
	switch v := value.(type) {
	case []any:
		return len(v)
	case []string:
		return len(v)
	default:
		return 0
	}
}

func addTopics(counts map[string]int, value any) {
	switch topics := value.(type) {
	case []any:
		for _, item := range topics {
			switch topic := item.(type) {
			case string:
				addTopic(counts, topic)
			case map[string]any:
				if title, ok := topic["title"].(string); ok {
					addTopic(counts, title)
				}
			}
		}
	case []string:
		for _, topic := range topics {
			addTopic(counts, topic)
		}
	}
}

func addTopic(counts map[string]int, topic string) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return
	}
	counts[topic]++
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

func topicMapToCounts(values map[string]int, limit int) []model.AnalyticsTopicCount {
	topics := make([]model.AnalyticsTopicCount, 0, len(values))
	for title, count := range values {
		topics = append(topics, model.AnalyticsTopicCount{Title: title, Count: count})
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].Count == topics[j].Count {
			return topics[i].Title < topics[j].Title
		}
		return topics[i].Count > topics[j].Count
	})
	if limit > 0 && len(topics) > limit {
		return topics[:limit]
	}
	return topics
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
