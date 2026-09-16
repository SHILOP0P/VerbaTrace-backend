package mock

import (
	"context"
	"encoding/json"

	"verbatrace/monolit/internal/models"
)

type Analyzer struct {
	model string
}

func New(model string) *Analyzer {
	return &Analyzer{model: model}
}

func (a *Analyzer) Provider() string {
	return "mock"
}

func (a *Analyzer) MaximumCompletionTokens(models.AnalysisRequest) int64 {
	return 512
}

func (a *Analyzer) Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error) {
	select {
	case <-ctx.Done():
		return models.AnalysisResult{}, ctx.Err()
	default:
	}

	payload := map[string]any{
		"schema_version":    2,
		"summary":           "Тестовый анализ звонка выполнен.",
		"topics":            []string{"Тестовый анализ"},
		"dialogue_tone":     map[string]any{"overall": "Нейтральный", "manager": "Вежливый", "client": "Спокойный", "evidence_quotes": []string{}},
		"client_questions":  []any{},
		"question_coverage": map[string]any{"status": "unclear", "summary": "Вопросы клиента в mock-анализе не оцениваются.", "unanswered_questions": []string{}},
		"manager_quality":   map[string]any{"strengths": []string{"Менеджер поддерживал структуру разговора."}, "issues": []string{}, "recommendations": []string{"Проверить результат на реальной модели анализа."}},
		"call_outcome":      "Тестовый результат анализа.",
		"score":             75,
		"score_scale":       100,
		"score_breakdown":   map[string]any{"points_awarded": 15, "points_possible": 20, "applicable_criteria_count": 2, "total_criteria_count": 2},
		"criteria_results": []map[string]any{
			{"code": "greeting", "title": "Приветствие", "status": "met", "points_awarded": 10, "points_max": 10, "evidence_quotes": []string{}, "issue": "", "recommendation": ""},
			{"code": "needs_discovery", "title": "Выявление потребности", "status": "partially_met", "points_awarded": 5, "points_max": 10, "evidence_quotes": []string{}, "issue": "Mock-анализ не проверяет реальную глубину вопросов.", "recommendation": "Запустить анализ через реального провайдера."},
		},
		"customer_objections": []string{},
		"risks":               []string{},
		"next_steps":          []string{"Проверить результат на реальной модели анализа."},
		"next_step":           "Проверить результат на реальной модели анализа.",
		"next_step_quality":   map[string]any{"has_next_step": true, "specific": true, "has_deadline": false, "has_responsible_person": false},
		"business_outcome":    map[string]any{"status": "unclear", "summary": "Mock-анализ не определяет бизнес-итог.", "lost_reason": "not_applicable"},
		"customer_signals":    map[string]any{"intent": "unclear", "urgency": "unclear", "budget_discussed": false, "decision_maker_present": false},
		"issue_codes":         []string{},
		"evidence_quotes":     []string{},
		"confidence":          "low",
		"call_uuid":           request.CallUUID.String(),
		"transcription_size":  len(request.Transcription),
		"instruction_count":   len(request.Instructions),
	}

	resultJSON, err := json.Marshal(payload)
	if err != nil {
		return models.AnalysisResult{}, err
	}

	resultText := "Тестовый анализ звонка: расшифровка и инструкции приняты."
	model := stringPtr(a.model)
	if a.model == "" {
		model = nil
	}

	return models.AnalysisResult{
		ResultJSON: resultJSON,
		ResultText: &resultText,
		Model:      model,
		Usage:      &models.ProviderUsage{PromptTokens: int64((len(request.Transcription) + 2) / 3), CompletionTokens: 512, TotalTokens: int64((len(request.Transcription)+2)/3) + 512, CostNanoUSD: 1_000_000},
	}, nil
}

func stringPtr(value string) *string {
	return &value
}
