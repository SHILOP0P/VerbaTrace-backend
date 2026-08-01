package analytics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"
)

const (
	aggregateRepresentativeCallLimit = 80
	aggregateMetricLimit             = 16
	aggregateSampleUUIDLimit         = 8
	aggregateAttentionCallLimit      = 12
)

func buildAggregateAnalysisRequest(input models.CreateDeepAnalysisInput, sources []models.AggregateAnalysisSourceCall, total int) models.AggregateAnalysisRequest {
	dataset := buildAggregateSourceDataset(sources, total)
	representatives := selectRepresentativeAggregateCalls(sources, dataset)
	dataset.SourceSummary.RepresentativeCalls = len(representatives)

	return models.AggregateAnalysisRequest{
		Scope:            input.Scope,
		PeriodFrom:       input.PeriodFrom,
		PeriodTo:         input.PeriodTo,
		SourceCallsCount: total,
		Sources:          representatives,
		Metrics: models.AggregateAnalysisSourceMetrics{
			IncludedCalls:       len(sources),
			TotalCalls:          total,
			AggregatedCalls:     len(sources),
			RepresentativeCalls: len(representatives),
			SourceSetHash:       dataset.SourceSummary.SourceSetHash,
		},
		Dataset: dataset,
	}
}

func buildAggregateSourceDataset(sources []models.AggregateAnalysisSourceCall, total int) models.AggregateAnalysisSourceDataset {
	if total == 0 {
		total = len(sources)
	}

	scoreSummary := aggregateScoreSummary(sources)
	issueCoverage := aggregateFrequencies(sources, func(source models.AggregateAnalysisSourceCall) []aggregateMetricValue {
		values := stringValues(source.IssueCodes)
		out := make([]aggregateMetricValue, 0, len(values))
		for _, value := range values {
			code := normalizeAggregateCode(value)
			if code == "" {
				continue
			}
			out = append(out, aggregateMetricValue{Code: code, Title: humanizeAggregateCode(code)})
		}
		return out
	}, total)

	return models.AggregateAnalysisSourceDataset{
		SourceSummary: models.AggregateAnalysisSourceSummary{
			AnalyzedCalls:        total,
			IncludedInStatistics: len(sources),
			AllAnalyzedCallsUsed: len(sources) == total,
			SourceSetHash:        aggregateSourceSetHash(sources),
		},
		ScoreSummary:     scoreSummary,
		IssueCoverage:    issueCoverage,
		WeakCriteria:     aggregateWeakCriteria(sources, total),
		BusinessOutcomes: aggregateBusinessOutcomes(sources, total),
		LostReasons:      aggregateLostReasons(sources, total),
		CustomerObjections: aggregateFrequencies(sources, func(source models.AggregateAnalysisSourceCall) []aggregateMetricValue {
			return titledMetricValues(stringValues(source.CustomerObjections))
		}, total),
		Risks: aggregateFrequencies(sources, func(source models.AggregateAnalysisSourceCall) []aggregateMetricValue {
			return titledMetricValues(stringValues(source.Risks))
		}, total),
		Topics: aggregateFrequencies(sources, func(source models.AggregateAnalysisSourceCall) []aggregateMetricValue {
			return titledMetricValues(stringValues(source.Topics))
		}, total),
		NextStepSummary: aggregateNextStepSummary(sources, total),
		AttentionCalls:  aggregateAttentionCalls(sources),
		StrongCalls:     aggregateStrongCalls(sources),
	}
}

func enrichAggregateAnalysisResult(result models.AnalysisResult, request models.AggregateAnalysisRequest) models.AnalysisResult {
	if len(result.ResultJSON) == 0 {
		return result
	}

	var payload map[string]any
	if err := json.Unmarshal(result.ResultJSON, &payload); err != nil {
		return result
	}

	payload["aggregate_schema_version"] = float64(1)
	payload["source_summary"] = request.Dataset.SourceSummary
	payload["aggregate_statistics"] = map[string]any{
		"score_summary":       request.Dataset.ScoreSummary,
		"issue_coverage":      request.Dataset.IssueCoverage,
		"weak_criteria":       request.Dataset.WeakCriteria,
		"business_outcomes":   request.Dataset.BusinessOutcomes,
		"lost_reasons":        request.Dataset.LostReasons,
		"customer_objections": request.Dataset.CustomerObjections,
		"risks":               request.Dataset.Risks,
		"topics":              request.Dataset.Topics,
		"next_step_summary":   request.Dataset.NextStepSummary,
		"attention_calls":     request.Dataset.AttentionCalls,
		"strong_calls":        request.Dataset.StrongCalls,
	}
	payload["coverage_note"] = "Статистические блоки рассчитаны backend по всем доступным готовым анализам звонков за выбранный период."
	normalizeAggregateRecurringIssues(payload, request.Dataset)

	raw, err := json.Marshal(payload)
	if err != nil {
		return result
	}

	text := aggregateResultText(payload, result.ResultText)
	return models.AnalysisResult{ResultJSON: raw, ResultText: &text, Model: result.Model}
}

func reusableAggregateAnalysisMatchesSourceSet(analysis models.AggregateAnalysis, request models.AggregateAnalysisRequest) bool {
	if analysis.SourceCallsCount != request.SourceCallsCount {
		return false
	}
	switch analysis.Status {
	case models.AggregateAnalysisStatusPending, models.AggregateAnalysisStatusProcessing:
		return true
	case models.AggregateAnalysisStatusDone:
	default:
		return false
	}

	var payload map[string]any
	if err := json.Unmarshal(analysis.ResultJSON, &payload); err != nil {
		return false
	}
	sourceSummary := mapValue(payload["source_summary"])
	return strings.TrimSpace(stringFromAny(sourceSummary["source_set_hash"])) == request.Dataset.SourceSummary.SourceSetHash
}

type aggregateMetricValue struct {
	Code  string
	Title string
}

type aggregateFrequencyAccumulator struct {
	code      string
	title     string
	count     int
	callUUIDs []string
}

func aggregateFrequencies(sources []models.AggregateAnalysisSourceCall, valuesFor func(models.AggregateAnalysisSourceCall) []aggregateMetricValue, total int) []models.AggregateAnalysisFrequency {
	if total == 0 {
		total = len(sources)
	}
	accumulators := map[string]*aggregateFrequencyAccumulator{}
	for _, source := range sources {
		seen := map[string]struct{}{}
		for _, value := range valuesFor(source) {
			code := normalizeAggregateCode(value.Code)
			if code == "" {
				continue
			}
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			acc := accumulators[code]
			if acc == nil {
				acc = &aggregateFrequencyAccumulator{code: code, title: strings.TrimSpace(value.Title)}
				if acc.title == "" {
					acc.title = humanizeAggregateCode(code)
				}
				accumulators[code] = acc
			}
			acc.count++
			if len(acc.callUUIDs) < aggregateSampleUUIDLimit {
				acc.callUUIDs = append(acc.callUUIDs, source.CallUUID.String())
			}
		}
	}

	items := make([]models.AggregateAnalysisFrequency, 0, len(accumulators))
	for _, acc := range accumulators {
		items = append(items, models.AggregateAnalysisFrequency{
			Code: acc.code, Title: acc.title, Count: acc.count, Share: aggregateShare(acc.count, total), SampleCallUUIDs: acc.callUUIDs,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Code < items[j].Code
		}
		return items[i].Count > items[j].Count
	})
	return limitAggregateFrequencies(items, aggregateMetricLimit)
}

func aggregateBusinessOutcomes(sources []models.AggregateAnalysisSourceCall, total int) []models.AggregateAnalysisFrequency {
	return aggregateFrequencies(sources, func(source models.AggregateAnalysisSourceCall) []aggregateMetricValue {
		record := mapValue(source.BusinessOutcome)
		status := normalizeAggregateCode(stringFromAny(record["status"]))
		if status == "" {
			status = "unclear"
		}
		return []aggregateMetricValue{{Code: status, Title: humanizeAggregateCode(status)}}
	}, total)
}

func aggregateLostReasons(sources []models.AggregateAnalysisSourceCall, total int) []models.AggregateAnalysisFrequency {
	return aggregateFrequencies(sources, func(source models.AggregateAnalysisSourceCall) []aggregateMetricValue {
		record := mapValue(source.BusinessOutcome)
		reason := normalizeAggregateCode(stringFromAny(record["lost_reason"]))
		if reason == "" || reason == "not_applicable" {
			return nil
		}
		return []aggregateMetricValue{{Code: reason, Title: humanizeAggregateCode(reason)}}
	}, total)
}

type aggregateCriterionAccumulator struct {
	code               string
	title              string
	applicableCalls    int
	weakCalls          int
	missedCalls        int
	partiallyMetCalls  int
	unclearCalls       int
	pointsShareSum     float64
	pointsShareSamples int
	callUUIDs          []string
}

func aggregateWeakCriteria(sources []models.AggregateAnalysisSourceCall, total int) []models.AggregateAnalysisCriterionMetric {
	if total == 0 {
		total = len(sources)
	}
	accumulators := map[string]*aggregateCriterionAccumulator{}
	for _, source := range sources {
		for _, record := range recordList(source.CriteriaResults) {
			code := normalizeAggregateCode(stringFromAny(record["code"]))
			if code == "" {
				code = normalizeAggregateCode(stringFromAny(record["title"]))
			}
			if code == "" {
				continue
			}
			status := normalizeAggregateCode(stringFromAny(record["status"]))
			if status == "not_applicable" {
				continue
			}
			acc := accumulators[code]
			if acc == nil {
				acc = &aggregateCriterionAccumulator{code: code, title: strings.TrimSpace(stringFromAny(record["title"]))}
				if acc.title == "" {
					acc.title = humanizeAggregateCode(code)
				}
				accumulators[code] = acc
			}
			acc.applicableCalls++
			pointsMax, maxOK := floatFromAny(record["points_max"])
			pointsAwarded, awardedOK := floatFromAny(record["points_awarded"])
			if maxOK && awardedOK && pointsMax > 0 {
				acc.pointsShareSum += clampAggregateRatio(pointsAwarded / pointsMax)
				acc.pointsShareSamples++
			}
			switch status {
			case "missed":
				acc.missedCalls++
				acc.weakCalls++
			case "partially_met":
				acc.partiallyMetCalls++
				acc.weakCalls++
			case "unclear":
				acc.unclearCalls++
				acc.weakCalls++
			}
			if (status == "missed" || status == "partially_met" || status == "unclear") && len(acc.callUUIDs) < aggregateSampleUUIDLimit {
				acc.callUUIDs = append(acc.callUUIDs, source.CallUUID.String())
			}
		}
	}

	items := make([]models.AggregateAnalysisCriterionMetric, 0, len(accumulators))
	for _, acc := range accumulators {
		if acc.weakCalls == 0 {
			continue
		}
		var average *float64
		if acc.pointsShareSamples > 0 {
			value := roundAggregateRatio(acc.pointsShareSum / float64(acc.pointsShareSamples))
			average = &value
		}
		items = append(items, models.AggregateAnalysisCriterionMetric{
			Code: acc.code, Title: acc.title, ApplicableCalls: acc.applicableCalls, WeakCalls: acc.weakCalls,
			WeakShare: aggregateShare(acc.weakCalls, total), AveragePointsShare: average, MissedCalls: acc.missedCalls,
			PartiallyMetCalls: acc.partiallyMetCalls, UnclearCalls: acc.unclearCalls, SampleCallUUIDs: acc.callUUIDs,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].WeakCalls == items[j].WeakCalls {
			return items[i].Code < items[j].Code
		}
		return items[i].WeakCalls > items[j].WeakCalls
	})
	if len(items) > aggregateMetricLimit {
		return items[:aggregateMetricLimit]
	}
	return items
}

func aggregateScoreSummary(sources []models.AggregateAnalysisSourceCall) models.AggregateAnalysisScoreSummary {
	var sum float64
	var minScore, maxScore *float64
	out := models.AggregateAnalysisScoreSummary{}
	for _, source := range sources {
		if source.Score == nil {
			continue
		}
		score := clampAggregateScore(*source.Score)
		out.CallsWithScore++
		sum += score
		if minScore == nil || score < *minScore {
			value := score
			minScore = &value
		}
		if maxScore == nil || score > *maxScore {
			value := score
			maxScore = &value
		}
		switch {
		case score < 50:
			out.LowCount++
		case score < 80:
			out.MediumCount++
		default:
			out.HighCount++
		}
	}
	if out.CallsWithScore > 0 {
		average := math.Round(sum/float64(out.CallsWithScore)*100) / 100
		out.Average = &average
		out.Min = minScore
		out.Max = maxScore
	}
	return out
}

func aggregateNextStepSummary(sources []models.AggregateAnalysisSourceCall, total int) models.AggregateAnalysisNextStepSummary {
	if total == 0 {
		total = len(sources)
	}
	out := models.AggregateAnalysisNextStepSummary{}
	for _, source := range sources {
		record := mapValue(source.NextStepQuality)
		hasNextStep := boolFromAny(record["has_next_step"])
		specific := boolFromAny(record["specific"])
		if hasNextStep {
			out.CallsWithNextStep++
		} else {
			out.CallsMissingNextStep++
		}
		if specific {
			out.CallsWithSpecificNextStep++
		} else {
			out.CallsMissingSpecificStep++
		}
	}
	out.MissingNextStepShare = aggregateShare(out.CallsMissingNextStep, total)
	out.MissingSpecificStepShare = aggregateShare(out.CallsMissingSpecificStep, total)
	return out
}

func aggregateAttentionCalls(sources []models.AggregateAnalysisSourceCall) []models.AggregateAnalysisCallEvidence {
	candidates := append([]models.AggregateAnalysisSourceCall(nil), sources...)
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := aggregateAttentionWeight(candidates[i]), aggregateAttentionWeight(candidates[j])
		if left == right {
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		return left > right
	})
	return aggregateCallEvidence(candidates, aggregateAttentionCallLimit, func(source models.AggregateAnalysisSourceCall) bool {
		return aggregateAttentionWeight(source) > 0
	})
}

func aggregateStrongCalls(sources []models.AggregateAnalysisSourceCall) []models.AggregateAnalysisCallEvidence {
	candidates := append([]models.AggregateAnalysisSourceCall(nil), sources...)
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := scoreOrDefault(candidates[i], -1), scoreOrDefault(candidates[j], -1)
		if left == right {
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		return left > right
	})
	return aggregateCallEvidence(candidates, aggregateSampleUUIDLimit, func(source models.AggregateAnalysisSourceCall) bool {
		return source.Score != nil && *source.Score >= 85
	})
}

func aggregateCallEvidence(sources []models.AggregateAnalysisSourceCall, limit int, include func(models.AggregateAnalysisSourceCall) bool) []models.AggregateAnalysisCallEvidence {
	out := make([]models.AggregateAnalysisCallEvidence, 0, limit)
	for _, source := range sources {
		if !include(source) {
			continue
		}
		out = append(out, models.AggregateAnalysisCallEvidence{
			CallUUID: source.CallUUID, CreatedAt: source.CreatedAt, Title: source.Title, Score: source.Score,
			Summary: source.Summary, IssueCodes: normalizedIssueCodes(source.IssueCodes),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func aggregateAttentionWeight(source models.AggregateAnalysisSourceCall) int {
	weight := len(normalizedIssueCodes(source.IssueCodes))
	if source.Score != nil {
		switch {
		case *source.Score < 50:
			weight += 4
		case *source.Score < 70:
			weight += 2
		}
	}
	record := mapValue(source.NextStepQuality)
	if !boolFromAny(record["has_next_step"]) {
		weight += 2
	}
	return weight
}

func selectRepresentativeAggregateCalls(sources []models.AggregateAnalysisSourceCall, dataset models.AggregateAnalysisSourceDataset) []models.AggregateAnalysisSourceCall {
	if len(sources) <= aggregateRepresentativeCallLimit {
		return sources
	}

	byID := make(map[string]models.AggregateAnalysisSourceCall, len(sources))
	for _, source := range sources {
		byID[source.CallUUID.String()] = source
	}

	selected := make([]models.AggregateAnalysisSourceCall, 0, aggregateRepresentativeCallLimit)
	seen := map[string]struct{}{}
	add := func(source models.AggregateAnalysisSourceCall) {
		if len(selected) >= aggregateRepresentativeCallLimit {
			return
		}
		key := source.CallUUID.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		selected = append(selected, source)
	}

	for i := 0; i < len(sources) && i < 20; i++ {
		add(sources[i])
	}

	attention := append([]models.AggregateAnalysisSourceCall(nil), sources...)
	sort.SliceStable(attention, func(i, j int) bool {
		left, right := aggregateAttentionWeight(attention[i]), aggregateAttentionWeight(attention[j])
		if left == right {
			return scoreOrDefault(attention[i], 101) < scoreOrDefault(attention[j], 101)
		}
		return left > right
	})
	for i := 0; i < len(attention) && i < 30; i++ {
		add(attention[i])
	}

	strong := append([]models.AggregateAnalysisSourceCall(nil), sources...)
	sort.SliceStable(strong, func(i, j int) bool {
		return scoreOrDefault(strong[i], -1) > scoreOrDefault(strong[j], -1)
	})
	for i := 0; i < len(strong) && i < 15; i++ {
		add(strong[i])
	}

	for _, issue := range dataset.IssueCoverage {
		for _, id := range issue.SampleCallUUIDs {
			if source, ok := byID[id]; ok {
				add(source)
			}
		}
	}
	for _, source := range sources {
		add(source)
		if len(selected) >= aggregateRepresentativeCallLimit {
			break
		}
	}
	return selected
}

func normalizeAggregateRecurringIssues(payload map[string]any, dataset models.AggregateAnalysisSourceDataset) {
	coverageByCode := map[string]models.AggregateAnalysisFrequency{}
	for _, item := range dataset.IssueCoverage {
		coverageByCode[item.Code] = item
	}

	existing := recordList(payload["recurring_issues"])
	kept := make([]any, 0, len(existing))
	singles := recordList(payload["single_call_observations"])
	seenRecurring := map[string]struct{}{}

	for _, item := range existing {
		code := normalizeAggregateCode(stringFromAny(item["code"]))
		count := int(math.Round(numberFromAny(item["count"])))
		if coverage, ok := coverageByCode[code]; ok {
			count = coverage.Count
			item["count"] = float64(coverage.Count)
			item["affected_share"] = coverage.Share
			item["sample_call_uuids"] = coverage.SampleCallUUIDs
		}
		if count < 2 {
			item["reason"] = "Проблема встречается менее чем в двух звонках, поэтому не считается повторяющейся."
			singles = append(singles, item)
			continue
		}
		if code != "" {
			seenRecurring[code] = struct{}{}
		}
		kept = append(kept, item)
	}

	for _, coverage := range dataset.IssueCoverage {
		if coverage.Count < 2 {
			singles = append(singles, map[string]any{
				"code": coverage.Code, "title": coverage.Title, "count": float64(coverage.Count),
				"affected_share": coverage.Share, "sample_call_uuids": coverage.SampleCallUUIDs,
				"reason": "Единичный сигнал из одного звонка.",
			})
			continue
		}
		if _, ok := seenRecurring[coverage.Code]; ok {
			continue
		}
		kept = append(kept, map[string]any{
			"code": coverage.Code, "title": coverage.Title, "count": float64(coverage.Count),
			"recommendation": "Проверить звонки из выборки и закрепить корректирующее действие для повторяющегося паттерна.",
			"affected_share": coverage.Share, "sample_call_uuids": coverage.SampleCallUUIDs,
		})
	}

	payload["recurring_issues"] = kept
	if len(singles) > 0 {
		payload["single_call_observations"] = singles
	}
}

func aggregateResultText(payload map[string]any, fallback *string) string {
	for _, key := range []string{"summary", "executive_summary", "overall_assessment"} {
		if value := strings.TrimSpace(stringFromAny(payload[key])); value != "" {
			return value
		}
	}
	if fallback != nil && strings.TrimSpace(*fallback) != "" {
		return strings.TrimSpace(*fallback)
	}
	return "Глубокий анализ сформирован."
}

func titledMetricValues(values []string) []aggregateMetricValue {
	out := make([]aggregateMetricValue, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value)
		code := normalizeAggregateCode(title)
		if code == "" {
			continue
		}
		out = append(out, aggregateMetricValue{Code: code, Title: title})
	}
	return out
}

func normalizedIssueCodes(value any) []string {
	values := stringValues(value)
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		code := normalizeAggregateCode(value)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func limitAggregateFrequencies(items []models.AggregateAnalysisFrequency, limit int) []models.AggregateAnalysisFrequency {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func aggregateSourceSetHash(sources []models.AggregateAnalysisSourceCall) string {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.CallUUID.String())
	}
	sort.Strings(ids)
	hash := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(hash[:])
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			return []string{text}
		}
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, stringValues(item)...)
		}
		return out
	case map[string]any:
		for _, key := range []string{"text", "title", "summary", "objection", "risk", "topic", "value", "code", "reason"} {
			if text := strings.TrimSpace(stringFromAny(typed[key])); text != "" {
				return []string{text}
			}
		}
	}
	return nil
}

func recordList(value any) []map[string]any {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if record, ok := value.(map[string]any); ok {
			out = append(out, record)
		}
	}
	return out
}

func mapValue(value any) map[string]any {
	if record, ok := value.(map[string]any); ok {
		return record
	}
	return map[string]any{}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(strings.Trim(stringifyAggregateValue(typed), `"`))
	}
}

func stringifyAggregateValue(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func floatFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func numberFromAny(value any) float64 {
	if number, ok := floatFromAny(value); ok {
		return number
	}
	return 0
}

func boolFromAny(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func normalizeAggregateCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.Join(strings.Fields(value), "_")
	value = strings.Trim(value, "_")
	if value == "not_applicable" || value == "none" || value == "n_a" || value == "не_указано" {
		return ""
	}
	return value
}

func humanizeAggregateCode(code string) string {
	code = strings.TrimSpace(strings.ReplaceAll(code, "_", " "))
	if code == "" {
		return ""
	}
	return strings.ToUpper(code[:1]) + code[1:]
}

func aggregateShare(count int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return roundAggregateRatio(float64(count) / float64(total))
}

func roundAggregateRatio(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func clampAggregateRatio(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func clampAggregateScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func scoreOrDefault(source models.AggregateAnalysisSourceCall, fallback float64) float64 {
	if source.Score == nil {
		return fallback
	}
	return *source.Score
}
