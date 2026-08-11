package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := New("", "google/gemini-2.5-flash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRequiresModel(t *testing.T) {
	_, err := New("sk-or-v1-test", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewDoesNotImposeHTTPTimeout(t *testing.T) {
	analyzer, err := New("sk-or-v1-test", "google/gemini-2.5-flash")
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	if analyzer.client.Timeout != 0 {
		t.Fatalf("timeout = %s, want no client deadline", analyzer.client.Timeout)
	}
}

func TestAnalyzeSendsTranscriptionAndInstructions(t *testing.T) {
	callID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != chatPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, chatPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-or-v1-test" {
			t.Fatalf("authorization = %q", got)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "google/gemini-2.5-flash" {
			t.Fatalf("model = %q", req.Model)
		}
		if req.MaxCompletionTokens != 12288 {
			t.Fatalf("max completion tokens = %d", req.MaxCompletionTokens)
		}
		if req.Reasoning.Effort != "minimal" || !req.Reasoning.Exclude {
			t.Fatalf("reasoning = %+v", req.Reasoning)
		}
		if !req.Provider.RequireParameters {
			t.Fatalf("provider routing = %+v", req.Provider)
		}
		if req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.JSONSchema.Name != "call_analysis" {
			t.Fatalf("response format = %#v", req.ResponseFormat)
		}
		if !req.ResponseFormat.JSONSchema.Strict {
			t.Fatal("response schema is not strict")
		}
		criteria := req.ResponseFormat.JSONSchema.Schema["properties"].(map[string]any)["criteria_results"].(map[string]any)
		criterionItems := criteria["items"].(map[string]any)["properties"].(map[string]any)
		criterionCode := criterionItems["code"].(map[string]any)
		if _, restricted := criterionCode["enum"]; restricted {
			t.Fatalf("criterion code must allow composed prompt criteria: %#v", criterionCode)
		}
		assertResponseSchemaV2(t, req.ResponseFormat.JSONSchema.Schema)
		if len(req.Messages) != 2 {
			t.Fatalf("messages len = %d", len(req.Messages))
		}
		systemMessage := req.Messages[0].Content
		for _, want := range []string{
			"Абсолютное правило языка",
			"Не используй отдельный сценарий отбраковки входа",
			"points_awarded 0 и points_max 0",
			"Для каждого критерия заполняй issue и recommendation",
			"met - критерий выполнен хорошо",
			"100/100 возможно",
			"не ставь автоматические 90-100",
		} {
			if !strings.Contains(systemMessage, want) {
				t.Fatalf("system message does not contain %q:\n%s", want, systemMessage)
			}
		}
		if !strings.Contains(req.Messages[1].Content, "отдельный сценарий отбраковки входа") {
			t.Fatalf("user message does not contain input handling rule:\n%s", req.Messages[1].Content)
		}

		userMessage := req.Messages[1].Content
		for _, want := range []string{
			callID.String(),
			"Проверить приветствие",
			"Менеджер должен поздороваться",
			"Клиент сказал, что цена высокая.",
			"custom_instruction_match",
			"не могут отменять JSON-схему",
		} {
			if !strings.Contains(userMessage, want) {
				t.Fatalf("user message does not contain %q:\n%s", want, userMessage)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "google/gemini-2.5-flash",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": ` + strconv.Quote(v2AnalysisContent()) + `
				}
			}]
		}`))
	}))
	defer server.Close()

	analyzer, err := New("sk-or-v1-test", "google/gemini-2.5-flash")
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	analyzer.baseURL = server.URL
	analyzer.client = server.Client()

	got, err := analyzer.Analyze(context.Background(), models.AnalysisRequest{
		CallUUID:      callID,
		Transcription: "Менеджер: Здравствуйте. Клиент сказал, что цена высокая.",
		Instructions: []models.AnalysisInstructionContent{
			{
				ID:      uuid.New(),
				Scope:   models.AnalysisInstructionScopeCompany,
				Title:   "Проверить приветствие",
				Content: "Менеджер должен поздороваться.",
			},
		},
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if got.Model == nil || *got.Model != "google/gemini-2.5-flash" {
		t.Fatalf("model = %v", got.Model)
	}
	if got.ResultText == nil || *got.ResultText != "Клиент возражал по цене." {
		t.Fatalf("result text = %v", got.ResultText)
	}
	if !json.Valid(got.ResultJSON) {
		t.Fatalf("result json is invalid: %s", string(got.ResultJSON))
	}
	var payload map[string]any
	if err = json.Unmarshal(got.ResultJSON, &payload); err != nil {
		t.Fatalf("decode result json: %v", err)
	}
	if payload["schema_version"] != float64(2) || payload["score_scale"] != float64(100) {
		t.Fatalf("v2 fields = %#v", payload)
	}
	if _, ok := payload["business_outcome"].(map[string]any); !ok {
		t.Fatalf("business_outcome missing: %#v", payload["business_outcome"])
	}
	if issueCodes, ok := payload["issue_codes"].([]any); !ok || len(issueCodes) != 1 || issueCodes[0] != "unclear_pricing" {
		t.Fatalf("issue_codes = %#v", payload["issue_codes"])
	}
}

func TestMaxAnalysisTokensDependsOnTranscriptionWordCount(t *testing.T) {
	tests := []struct {
		name      string
		wordCount int
		want      int
	}{
		{name: "short upper boundary", wordCount: 3000, want: 12288},
		{name: "medium lower boundary", wordCount: 3001, want: 24576},
		{name: "medium upper boundary", wordCount: 8000, want: 24576},
		{name: "long lower boundary", wordCount: 8001, want: 32768},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transcription := strings.Repeat("слово ", test.wordCount)
			if got := maxAnalysisTokens(transcription); got != test.want {
				t.Fatalf("maxAnalysisTokens() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestAnalyzeAggregateSendsDatasetAndDeepSchema(t *testing.T) {
	callID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.JSONSchema.Name != "aggregate_analysis" {
			t.Fatalf("response format = %#v", req.ResponseFormat)
		}
		if req.MaxCompletionTokens != 32768 {
			t.Fatalf("max completion tokens = %d", req.MaxCompletionTokens)
		}
		required, ok := stringSlice(req.ResponseFormat.JSONSchema.Schema["required"])
		if !ok {
			t.Fatalf("aggregate required = %#v", req.ResponseFormat.JSONSchema.Schema["required"])
		}
		for _, want := range []string{"executive_summary", "systemic_issues", "single_call_observations", "weak_criteria", "detailed_report"} {
			if !containsString(required, want) {
				t.Fatalf("aggregate schema required missing %q: %#v", want, required)
			}
		}
		if !strings.Contains(req.Messages[0].Content, "Representative_calls не являются полной базой анализа") {
			t.Fatalf("aggregate system prompt does not explain representative calls:\n%s", req.Messages[0].Content)
		}
		if !strings.Contains(req.Messages[1].Content, `"source_calls_count":150`) ||
			!strings.Contains(req.Messages[1].Content, `"source_set_hash":"hash"`) ||
			!strings.Contains(req.Messages[1].Content, callID.String()) {
			t.Fatalf("aggregate user prompt does not contain dataset/request:\n%s", req.Messages[1].Content)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "google/gemini-2.5-flash",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": ` + strconv.Quote(aggregateAnalysisContent()) + `
				}
			}]
		}`))
	}))
	defer server.Close()

	analyzer, err := New("sk-or-v1-test", "google/gemini-2.5-flash")
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	analyzer.baseURL = server.URL
	analyzer.client = server.Client()

	got, err := analyzer.AnalyzeAggregate(context.Background(), models.AggregateAnalysisRequest{
		SourceCallsCount: 150,
		Sources:          []models.AggregateAnalysisSourceCall{{CallUUID: callID, Title: "Call"}},
		Metrics: models.AggregateAnalysisSourceMetrics{
			IncludedCalls: 150, TotalCalls: 150, AggregatedCalls: 150, RepresentativeCalls: 1, SourceSetHash: "hash",
		},
		Dataset: models.AggregateAnalysisSourceDataset{
			SourceSummary: models.AggregateAnalysisSourceSummary{AnalyzedCalls: 150, IncludedInStatistics: 150, AllAnalyzedCallsUsed: true, SourceSetHash: "hash"},
			IssueCoverage: []models.AggregateAnalysisFrequency{{
				Code: "unclear_pricing", Title: "Неясная цена", Count: 42, Share: 0.28, SampleCallUUIDs: []string{callID.String()},
			}},
		},
	})
	if err != nil {
		t.Fatalf("analyze aggregate: %v", err)
	}
	if got.ResultText == nil || *got.ResultText != "Качество проседает из-за цены." {
		t.Fatalf("result text = %v", got.ResultText)
	}
}

func TestAnalyzeWrapsNonJSONResponse(t *testing.T) {
	resultJSON, resultText, err := normalizeAnalysisContent("Обычный текстовый ответ")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if resultText != "Обычный текстовый ответ" {
		t.Fatalf("result text = %q", resultText)
	}

	var payload map[string]any
	if err = json.Unmarshal(resultJSON, &payload); err != nil {
		t.Fatalf("decode result json: %v", err)
	}
	if payload["raw_response"] != "Обычный текстовый ответ" {
		t.Fatalf("raw response = %v", payload["raw_response"])
	}
	for _, key := range []string{
		"schema_version",
		"score_scale",
		"score_breakdown",
		"business_outcome",
		"customer_signals",
		"next_step_quality",
		"issue_codes",
	} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("%s key is missing from fallback payload: %#v", key, payload)
		}
	}
	if payload["score"] != float64(0) || payload["confidence"] != "low" {
		t.Fatalf("fallback score/confidence = %#v/%#v", payload["score"], payload["confidence"])
	}
}

func TestNormalizeAnalysisContentRejectsIncompleteStructuredJSON(t *testing.T) {
	_, _, err := normalizeAnalysisContent(`{"schema_version":2,"summary":"unfinished`)
	if err == nil {
		t.Fatal("expected incomplete structured JSON to be rejected")
	}
	if !strings.Contains(err.Error(), "incomplete structured JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeReportsProviderTruncationDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"openai/gpt-5-mini",
			"choices":[{
				"finish_reason":"length",
				"native_finish_reason":"max_output_tokens",
				"message":{"role":"assistant","content":"{\"schema_version\":2,\"criteria_results\":["}
			}],
			"usage":{"completion_tokens":12288,"reasoning_tokens":1200}
		}`))
	}))
	defer server.Close()

	analyzer, err := New("sk-or-v1-test", "openai/gpt-5-mini")
	if err != nil {
		t.Fatal(err)
	}
	analyzer.baseURL = server.URL
	analyzer.client = server.Client()

	_, err = analyzer.Analyze(context.Background(), models.AnalysisRequest{CallUUID: uuid.New(), Transcription: "тестовая расшифровка"})
	if err == nil {
		t.Fatal("expected truncated response error")
	}
	for _, want := range []string{"incomplete structured JSON", "finish_reason=length", "native_finish_reason=max_output_tokens", "completion_tokens=12288", "reasoning_tokens=1200"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestUserPromptMarksMissingInstructionsNotApplicable(t *testing.T) {
	got := userPrompt(uuid.NewString(), "Менеджер: Здравствуйте.", nil)
	for _, want := range []string{
		"Персонализация не задана",
		"Анализируй разговор универсально",
		"Загруженные инструкции не выбраны",
		"custom_instruction_match верни со status not_applicable",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("user prompt does not contain %q:\n%s", want, got)
		}
	}
}

func TestSummaryFromJSON(t *testing.T) {
	got := summaryFromJSON(json.RawMessage(`{"summary":" Короткое резюме. "}`), "fallback")
	if got != "Короткое резюме." {
		t.Fatalf("summary = %q", got)
	}

	got = summaryFromJSON(json.RawMessage(`{"summary":" "}`), "fallback")
	if got != "fallback" {
		t.Fatalf("fallback summary = %q", got)
	}
}

func TestAnalyzeReturnsOpenRouterError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient credits","code":402}}`))
	}))
	defer server.Close()

	analyzer, err := New("sk-or-v1-test", "google/gemini-2.5-flash")
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	analyzer.baseURL = server.URL
	analyzer.client = server.Client()

	_, err = analyzer.Analyze(context.Background(), models.AnalysisRequest{
		CallUUID:      uuid.New(),
		Transcription: "Тестовая транскрипция.",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 402") || !strings.Contains(err.Error(), "insufficient credits") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnalyzeRejectsEmptyTranscription(t *testing.T) {
	analyzer, err := New("sk-or-v1-test", "google/gemini-2.5-flash")
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}

	_, err = analyzer.Analyze(context.Background(), models.AnalysisRequest{
		CallUUID:      uuid.New(),
		Transcription: "   ",
	})
	if !errors.Is(err, models.ErrInvalidAnalysisInput) {
		t.Fatalf("error = %v, want invalid analysis input", err)
	}
}

func assertResponseSchemaV2(t *testing.T, schema map[string]any) {
	t.Helper()

	required, ok := stringSlice(schema["required"])
	if !ok {
		t.Fatalf("schema required = %#v", schema["required"])
	}
	for _, want := range []string{
		"summary",
		"topics",
		"dialogue_tone",
		"client_questions",
		"question_coverage",
		"manager_quality",
		"call_outcome",
		"score",
		"criteria_results",
		"customer_objections",
		"risks",
		"next_steps",
		"next_step",
		"evidence_quotes",
		"confidence",
		"schema_version",
		"score_scale",
		"score_breakdown",
		"business_outcome",
		"customer_signals",
		"next_step_quality",
		"issue_codes",
	} {
		if !containsString(required, want) {
			t.Fatalf("schema required missing %q: %#v", want, required)
		}
	}

	properties := schema["properties"].(map[string]any)
	criteria := properties["criteria_results"].(map[string]any)
	item := criteria["items"].(map[string]any)
	itemRequired, ok := stringSlice(item["required"])
	if !ok {
		t.Fatalf("criteria item required = %#v", item["required"])
	}
	for _, want := range []string{"code", "title", "topic", "status", "points_awarded", "points_max", "score", "quote", "evidence_quotes", "issue", "explanation", "recommendation"} {
		if !containsString(itemRequired, want) {
			t.Fatalf("criteria item required missing %q: %#v", want, itemRequired)
		}
	}
	itemProperties := item["properties"].(map[string]any)
	if _, ok := itemProperties["instruction_title"]; ok {
		t.Fatalf("criteria schema still contains legacy instruction_title: %#v", itemProperties)
	}
	if _, ok := itemProperties["result"]; ok {
		t.Fatalf("criteria schema still contains legacy result: %#v", itemProperties)
	}

	businessOutcome := properties["business_outcome"].(map[string]any)
	businessProps := businessOutcome["properties"].(map[string]any)
	status := businessProps["status"].(map[string]any)
	statusEnum, ok := stringSlice(status["enum"])
	if !ok {
		t.Fatalf("business_outcome.status enum = %#v", status["enum"])
	}
	if containsString(statusEnum, "not_call") {
		t.Fatalf("business_outcome.status enum must not contain not_call: %#v", status["enum"])
	}

	issueCodes := properties["issue_codes"].(map[string]any)
	if _, ok := issueCodes["enum"]; ok {
		t.Fatalf("issue_codes must not have enum: %#v", issueCodes)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func v2AnalysisContent() string {
	return `{
		"schema_version": 2,
		"summary": "Клиент возражал по цене.",
		"topics": ["Цена"],
		"dialogue_tone": {
			"overall": "Деловой",
			"manager": "Вежливый",
			"client": "Осторожный",
			"evidence_quotes": ["цена высокая"]
		},
		"client_questions": [],
		"question_coverage": {
			"status": "no_questions",
			"summary": "Клиент не задавал вопросов.",
			"unanswered_questions": []
		},
		"manager_quality": {
			"strengths": ["Менеджер поздоровался."],
			"issues": ["Цена объяснена недостаточно ясно."],
			"recommendations": ["Подготовить расчет."]
		},
		"call_outcome": "Нужно отправить расчет.",
		"score": 80,
		"score_scale": 100,
		"score_breakdown": {
			"points_awarded": 8,
			"points_possible": 10,
			"applicable_criteria_count": 1,
			"total_criteria_count": 2
		},
		"criteria_results": [{
			"code": "pricing_clarity",
			"title": "Ясность цены",
			"status": "partially_met",
			"points_awarded": 5,
			"points_max": 10,
			"evidence_quotes": ["цена высокая"],
			"issue": "Цена вызвала возражение.",
			"recommendation": "Отправить расчет."
		}],
		"customer_objections": ["Цена высокая"],
		"risks": [],
		"next_steps": ["Отправить расчет"],
		"next_step": "Отправить расчет",
		"next_step_quality": {
			"has_next_step": true,
			"specific": true,
			"has_deadline": false,
			"has_responsible_person": false
		},
		"business_outcome": {
			"status": "follow_up_needed",
			"summary": "Клиент ждет расчет.",
			"lost_reason": "not_applicable"
		},
		"customer_signals": {
			"intent": "medium",
			"urgency": "low",
			"budget_discussed": true,
			"decision_maker_present": false
		},
		"issue_codes": ["unclear_pricing"],
		"evidence_quotes": ["цена высокая"],
		"confidence": "high"
	}`
}

func aggregateAnalysisContent() string {
	return `{
		"summary": "Качество проседает из-за цены.",
		"executive_summary": "За период заметен повторяющийся риск по объяснению цены.",
		"overall_assessment": "Нужна системная работа с ценностью предложения и следующими шагами.",
		"key_findings": [{
			"title": "Неясное объяснение цены",
			"description": "В части звонков цена вызывает возражение, а менеджер не раскрывает ценность достаточно подробно.",
			"severity": "high",
			"evidence_call_uuids": ["00000000-0000-0000-0000-000000000001"],
			"affected_calls_count": 42,
			"affected_share": 0.28
		}],
		"recurring_issues": [{
			"code": "unclear_pricing",
			"title": "Неясная цена",
			"count": 42,
			"recommendation": "Подготовить аргументы ценности и варианты скидок.",
			"affected_share": 0.28,
			"sample_call_uuids": ["00000000-0000-0000-0000-000000000001"]
		}],
		"systemic_issues": [{
			"code": "unclear_pricing",
			"title": "Неясная цена",
			"description": "Возражение по цене повторяется и влияет на исход разговора.",
			"affected_calls_count": 42,
			"affected_share": 0.28,
			"severity": "high",
			"evidence_call_uuids": ["00000000-0000-0000-0000-000000000001"],
			"recommendation": "Дать менеджерам сценарий объяснения ценности.",
			"business_impact": "Снижение конверсии при обсуждении стоимости."
		}],
		"single_call_observations": [],
		"weak_criteria": [{
			"code": "pricing_clarity",
			"title": "Ясность цены",
			"affected_calls_count": 42,
			"affected_share": 0.28,
			"explanation": "Критерий проседает в звонках с ценовым возражением.",
			"recommendation": "Усилить объяснение цены.",
			"evidence_call_uuids": ["00000000-0000-0000-0000-000000000001"]
		}],
		"client_objections": [{
			"code": "price_high",
			"title": "Цена высокая",
			"affected_calls_count": 42,
			"affected_share": 0.28,
			"explanation": "Клиенты сомневаются в цене.",
			"recommendation": "Показывать ценность до обсуждения стоимости.",
			"evidence_call_uuids": ["00000000-0000-0000-0000-000000000001"]
		}],
		"loss_and_risk_patterns": [{
			"code": "client_may_leave",
			"title": "Риск ухода клиента",
			"description": "Клиент может выбрать конкурента при слабом объяснении цены.",
			"affected_calls_count": 42,
			"affected_share": 0.28,
			"severity": "medium",
			"evidence_call_uuids": ["00000000-0000-0000-0000-000000000001"],
			"recommendation": "Закрепить следующий шаг и ценность.",
			"business_impact": "Потеря части сделок."
		}],
		"strengths": ["Менеджеры сохраняют деловой тон."],
		"risks": ["Клиенты могут уйти к конкурентам."],
		"priority_actions": [{
			"title": "Обновить сценарий объяснения цены",
			"priority": "high",
			"expected_effect": "Снизить число ценовых возражений."
		}],
		"manager_recommendations": ["Показывать ценность до цены."],
		"confidence": "high",
		"detailed_report": {
			"methodology": "Вывод сделан по backend dataset за период.",
			"quality_overview": "Главный риск связан с ценой.",
			"issue_analysis": "Проблема повторяется в значимой доле звонков.",
			"customer_loss_analysis": "Цена может приводить к потере клиента.",
			"training_plan": "Обучить менеджеров объяснять ценность.",
			"data_limitations": "Использованы сохраненные per-call analyses, не полные транскрипции."
		}
	}`
}
