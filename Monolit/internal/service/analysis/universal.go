package analysis

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"
)

func isUniversalAnalysis(payload map[string]any) bool {
	version, ok := payload["schema_version"].(float64)
	return ok && version == 3
}

func normalizeUniversalAnalysisResult(result models.AnalysisResult, payload map[string]any) (models.AnalysisResult, error) {
	// Stored results and analyses resumed across a deploy carry the version
	// they were started with, so every version still in the database is read.
	switch stringField(payload, "prompt_version") {
	case "universal-v3.1", "universal-v3.2":
	default:
		return models.AnalysisResult{}, errors.New("unsupported universal analysis prompt version")
	}
	summary := stringField(payload, "summary")
	if summary == "" {
		return models.AnalysisResult{}, errors.New("universal analysis summary is empty")
	}
	coverage, ok := payload["coverage"].(map[string]any)
	if !ok {
		return models.AnalysisResult{}, errors.New("universal analysis coverage is missing")
	}
	total := nonNegativeInt(coverage["actual_question_count"])
	analyzed := nonNegativeInt(coverage["analyzed_actual_question_count"])
	if analyzed > total {
		return models.AnalysisResult{}, errors.New("analyzed question count exceeds actual question count")
	}
	items, ok := payload["items"].([]any)
	if !ok {
		return models.AnalysisResult{}, errors.New("universal analysis items are missing")
	}
	seenItems := map[string]bool{}
	questionItems := 0
	weighted, weights := 0.0, 0.0
	for index, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return models.AnalysisResult{}, fmt.Errorf("universal analysis item %d is invalid", index)
		}
		id := strings.TrimSpace(stringField(item, "id"))
		if id == "" || seenItems[id] {
			return models.AnalysisResult{}, fmt.Errorf("universal analysis item %d has invalid id", index)
		}
		seenItems[id] = true
		if stringField(item, "kind") == "question" {
			questionItems++
		}
		status := stringField(item, "status")
		weight := boundedInt(item["weight"], 1, 3, 1)
		item["weight"] = weight
		if excludedUniversalStatus(status) {
			item["score"] = nil
			continue
		}
		score, hasScore := universalStatusScore(status)
		if !hasScore {
			return models.AnalysisResult{}, fmt.Errorf("universal analysis item %s has invalid status", id)
		}
		item["score"] = score
		if contributes, ok := item["contributes_to_overall"].(bool); ok && !contributes {
			continue
		}
		weighted += float64(score * weight)
		weights += float64(weight)
	}
	if questionItems < analyzed || (coverage["status"] == "complete" && analyzed != total) {
		coverage["status"] = "partial"
		if questionItems < analyzed {
			coverage["analyzed_actual_question_count"] = questionItems
		}
		limitations := stringArray(coverage["limitations"])
		limitations = append(limitations, "Счётчик вопросов модели не совпал с числом карточек; итог помечен как неполный")
		coverage["limitations"] = stringsToAny(limitations)
	}
	if coverage["status"] == "complete" && questionItems != total {
		return models.AnalysisResult{}, errors.New("incomplete_coverage: question inventory differs from final items")
	}
	if coverage["status"] == "partial" {
		payload["overall_score"] = nil
		payload["overall_score_label"] = "Анализ неполный"
	} else if weights == 0 {
		payload["overall_score"] = nil
		payload["overall_score_label"] = "Недостаточно оснований для оценки"
	} else {
		score := int(math.Floor(weighted/weights + 0.5))
		payload["overall_score"] = score
		payload["overall_score_label"] = fmt.Sprintf("%d / 100", score)
	}
	if err := normalizeUniversalRecommendations(payload, seenItems); err != nil {
		return models.AnalysisResult{}, err
	}
	payload["schema_version"] = 3
	payload["score"] = payload["overall_score"]
	payload["score_scale"] = 100
	encoded, err := json.Marshal(payload)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("encode universal analysis: %w", err)
	}
	result.ResultJSON = encoded
	result.ResultText = &summary
	return result, nil
}

func stringsToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func normalizeUniversalRecommendations(payload map[string]any, itemIDs map[string]bool) error {
	raw, ok := payload["recommendations"].([]any)
	if !ok {
		return errors.New("universal analysis recommendations are missing")
	}
	type ranked struct {
		id                          string
		score, importance, position int
	}
	ranking := make([]ranked, 0, len(raw))
	seen := map[string]bool{}
	for index, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("recommendation %d is invalid", index)
		}
		id := stringField(item, "id")
		if id == "" || seen[id] {
			return fmt.Errorf("recommendation %d has invalid id", index)
		}
		seen[id] = true
		for _, linked := range stringArray(item["item_ids"]) {
			if !itemIDs[linked] {
				return fmt.Errorf("recommendation %s references unknown item %s", id, linked)
			}
		}
		importance := boundedInt(item["importance"], 1, 3, 1)
		impact, hasImpact := optionalBoundedInt(item["impact"], 1, 3)
		repetition := boundedInt(item["repetition"], 1, 3, 1)
		priority := "unresolved"
		priorityScore := -1
		if hasImpact {
			priorityScore = int(math.Floor(100*float64(importance+2*impact+repetition-4)/8 + 0.5))
			switch {
			case priorityScore >= 70:
				priority = "high"
			case priorityScore >= 35:
				priority = "medium"
			default:
				priority = "low"
			}
			if affects, _ := item["affects_score"].(bool); !affects && priority == "high" {
				priority, priorityScore = "low", min(priorityScore, 34)
			}
			item["priority_score"] = priorityScore
		} else {
			item["priority_score"] = nil
		}
		item["importance"], item["impact"], item["repetition"], item["priority"] = importance, func() any {
			if hasImpact {
				return impact
			}
			return nil
		}(), repetition, priority
		ranking = append(ranking, ranked{id: id, score: priorityScore, importance: importance, position: index})
	}
	sort.SliceStable(ranking, func(i, j int) bool {
		if ranking[i].score != ranking[j].score {
			return ranking[i].score > ranking[j].score
		}
		if ranking[i].importance != ranking[j].importance {
			return ranking[i].importance > ranking[j].importance
		}
		return ranking[i].position < ranking[j].position
	})
	priorities := make([]any, 0, 3)
	for _, value := range ranking {
		if value.score < 0 || len(priorities) == 3 {
			continue
		}
		priorities = append(priorities, value.id)
	}
	payload["priority_recommendation_ids"] = priorities
	return nil
}

func excludedUniversalStatus(status string) bool {
	switch status {
	case "not_applicable", "unclear", "conflict", "not_assessed":
		return true
	default:
		return false
	}
}

func universalStatusScore(status string) (int, bool) {
	switch status {
	case "met":
		return 100, true
	case "mostly_met":
		return 75, true
	case "partially_met":
		return 50, true
	case "minimally_met":
		return 25, true
	case "missed":
		return 0, true
	default:
		return 0, false
	}
}

func nonNegativeInt(value any) int { return boundedInt(value, 0, math.MaxInt, 0) }
func boundedInt(value any, low, high, fallback int) int {
	v, ok := optionalBoundedInt(value, low, high)
	if !ok {
		return fallback
	}
	return v
}
func optionalBoundedInt(value any, low, high int) (int, bool) {
	v, ok := value.(float64)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v < float64(low) || v > float64(high) {
		return 0, false
	}
	return int(v), true
}
func stringArray(value any) []string {
	raw, _ := value.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}
