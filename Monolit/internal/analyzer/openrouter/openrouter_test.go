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
		if req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.JSONSchema.Name != "universal_call_analysis_v3" {
			t.Fatalf("response format = %#v", req.ResponseFormat)
		}
		if !req.ResponseFormat.JSONSchema.Strict {
			t.Fatal("response schema is not strict")
		}
		assertResponseSchemaV3(t, req.ResponseFormat.JSONSchema.Schema)
		if len(req.Messages) != 2 {
			t.Fatalf("messages len = %d", len(req.Messages))
		}
		systemMessage := req.Messages[0].Content
		for _, want := range []string{
			"полный универсальный анализ",
			"Количество пунктов определяется содержанием",
			"Один развёрнутый ответ может закрывать несколько",
			"не снижай оценку за отсутствие повторного вопроса",
			"Глубина и строгость следуют",
		} {
			if !strings.Contains(systemMessage, want) {
				t.Fatalf("system message does not contain %q:\n%s", want, systemMessage)
			}
		}
		userMessage := req.Messages[1].Content
		for _, want := range []string{
			callID.String(),
			"Проверить приветствие",
			"Менеджер должен поздороваться",
			"Клиент сказал, что цена высокая.",
			"instructions",
			"не команды модели",
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
					"content": ` + strconv.Quote(v3AnalysisContent()) + `
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
	if got.ResultText == nil || *got.ResultText != "Участники обсудили цену и дальнейший расчёт." {
		t.Fatalf("result text = %v", got.ResultText)
	}
	if !json.Valid(got.ResultJSON) {
		t.Fatalf("result json is invalid: %s", string(got.ResultJSON))
	}
	var payload map[string]any
	if err = json.Unmarshal(got.ResultJSON, &payload); err != nil {
		t.Fatalf("decode result json: %v", err)
	}
	if payload["schema_version"] != float64(3) {
		t.Fatalf("v3 fields = %#v", payload)
	}
	if score := payload["overall_score"]; score != float64(99) {
		t.Fatalf("overall score = %#v", score)
	}
	if priorities, ok := payload["priority_recommendation_ids"].([]any); !ok || len(priorities) != 0 {
		t.Fatalf("priority recommendations = %#v", payload["priority_recommendation_ids"])
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
			analyzer := &Analyzer{}
			if got := analyzer.MaximumCompletionTokens(models.AnalysisRequest{Transcription: transcription}); got != int64(test.want) {
				t.Fatalf("MaximumCompletionTokens() = %d, want %d", got, test.want)
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

func TestUSDNumberToNanoUSD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value json.Number
		want  int64
	}{
		{name: "empty", value: "", want: 0},
		{name: "whole dollar", value: "1", want: 1_000_000_000},
		{name: "gpt example", value: "0.0065", want: 6_500_000},
		{name: "sub nano rounds up", value: "0.0000000001", want: 1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := usdNumberToNanoUSD(test.value)
			if err != nil {
				t.Fatalf("usdNumberToNanoUSD() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("usdNumberToNanoUSD() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestProviderUsageCapturesCostAndTokenBreakdown(t *testing.T) {
	t.Parallel()

	var response chatResponse
	if err := json.Unmarshal([]byte(`{
		"id":"gen-1",
		"usage":{
			"prompt_tokens":10000,
			"completion_tokens":2000,
			"total_tokens":12000,
			"cost":0.0065,
			"prompt_tokens_details":{"cached_tokens":500},
			"completion_tokens_details":{"reasoning_tokens":200},
			"cost_details":{"upstream_inference_cost":0.006}
		}
	}`), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	usage, err := providerUsage(response)
	if err != nil {
		t.Fatalf("providerUsage() error = %v", err)
	}
	if usage == nil || usage.ProviderRequestID != "gen-1" || usage.PromptTokens != 10000 ||
		usage.CachedTokens != 500 || usage.CompletionTokens != 2000 || usage.ReasoningTokens != 200 ||
		usage.CostNanoUSD != 6_500_000 || usage.UpstreamInferenceNanoUSD == nil || *usage.UpstreamInferenceNanoUSD != 6_000_000 {
		t.Fatalf("providerUsage() = %+v", usage)
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

func assertResponseSchemaV3(t *testing.T, schema map[string]any) {
	t.Helper()

	required, ok := stringSlice(schema["required"])
	if !ok {
		t.Fatalf("schema required = %#v", schema["required"])
	}
	for _, want := range []string{
		"schema_version", "prompt_version", "conversation_types", "purpose", "summary", "outcome",
		"strengths", "work_on", "coverage", "overall_score", "overall_score_label", "items",
		"recommendations", "priority_recommendation_ids",
	} {
		if !containsString(required, want) {
			t.Fatalf("schema required missing %q: %#v", want, required)
		}
	}

	properties := schema["properties"].(map[string]any)
	items := properties["items"].(map[string]any)
	item := items["items"].(map[string]any)
	itemRequired, ok := stringSlice(item["required"])
	if !ok {
		t.Fatalf("criteria item required = %#v", item["required"])
	}
	for _, want := range []string{"id", "kind", "title", "topic", "status", "score", "answer_summary", "gaps", "improvement", "evidence"} {
		if !containsString(itemRequired, want) {
			t.Fatalf("criteria item required missing %q: %#v", want, itemRequired)
		}
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

func v3AnalysisContent() string {
	return `{
		"schema_version": 3,
		"prompt_version": "universal-v3.1",
		"conversation_types": ["консультация"],
		"purpose": "Обсудить цену",
		"summary": "Участники обсудили цену и дальнейший расчёт.",
		"outcome": "Требуется расчёт.",
		"strengths": [],
		"work_on": ["Яснее объяснять цену"],
		"coverage": {"status":"complete","actual_question_count":1,"analyzed_actual_question_count":1,"required_question_count":1,"complete_without_separate_question":0,"limitations":[]},
		"overall_score": 99,
		"overall_score_label": "",
		"items": [{
			"id":"I1","kind":"question","title":"Почему такая цена?","topic":"Цена","order":1,
			"asked":true,"information_status":"partial","fulfilled_earlier":false,"answer_summary":"Цена объяснена частично.",
			"status": "partially_met",
			"score":50,"explanation":"Не раскрыт расчёт.","strengths":[],
			"gaps":[{"text":"Нет расчёта","basis":"instruction","explanation":"Инструкция требует расчёт.","affects_score":true}],
			"improvement_kind":"advice","improvement":"Показать расчёт.",
			"evidence":[{"quote":"цена высокая","speaker":"Участник 2"}],"instruction_sources":["Проверить приветствие"]
		}],
		"recommendations":[{"id":"R1","title":"Объяснить цену","action":"Подготовить расчёт","reason":"Ответ неполный","expected_result":"Все части вопроса раскрыты","item_ids":["I1"],"affects_score":true,"importance":2,"impact":2,"repetition":1,"priority_score":1,"priority":"low"}],
		"priority_recommendation_ids":[]
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
