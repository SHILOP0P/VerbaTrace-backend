package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/models"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	chatPath       = "/chat/completions"
	providerName   = "openrouter"

	shortTranscriptionWordLimit = 3000
	longTranscriptionWordLimit  = 8000
	shortAnalysisMaxTokens      = 12288
	mediumAnalysisMaxTokens     = 24576
	longAnalysisMaxTokens       = 32768
)

type Analyzer struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []message      `json:"messages"`
	ResponseFormat      responseFormat `json:"response_format"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Reasoning           reasoning      `json:"reasoning"`
	Provider            provider       `json:"provider,omitempty"`
}

type provider struct {
	RequireParameters bool `json:"require_parameters"`
}

type reasoning struct {
	Effort  string `json:"effort"`
	Exclude bool   `json:"exclude"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message            message `json:"message"`
		FinishReason       string  `json:"finish_reason"`
		NativeFinishReason string  `json:"native_finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64       `json:"prompt_tokens"`
		CompletionTokens int64       `json:"completion_tokens"`
		TotalTokens      int64       `json:"total_tokens"`
		Cost             json.Number `json:"cost"`
		PromptDetails    struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			ReasoningTokens int64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
		CostDetails struct {
			UpstreamInferenceCost json.Number `json:"upstream_inference_cost"`
		} `json:"cost_details"`
		// Kept for compatibility with older OpenRouter response fixtures.
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"usage"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func New(apiKey string, model string) (*Analyzer, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("openrouter analyzer api key is required")
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("openrouter analyzer model is required")
	}

	return &Analyzer{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}, nil
}

func (a *Analyzer) Provider() string {
	return providerName
}

func (a *Analyzer) MaximumCompletionTokens(request models.AnalysisRequest) int64 {
	return int64(maxAnalysisTokens(request.Transcription))
}

func (a *Analyzer) Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error) {
	transcription := strings.TrimSpace(request.Transcription)
	if transcription == "" {
		return models.AnalysisResult{}, models.ErrInvalidAnalysisInput
	}

	payload := chatRequest{
		Model: a.model,
		Messages: []message{
			{
				Role:    "system",
				Content: systemPrompt(),
			},
			{
				Role:    "user",
				Content: userPromptWithPrivacy(request.CallUUID.String(), transcription, request.Instructions, request.Personalization, request.Redaction),
			},
		},
		ResponseFormat: callAnalysisResponseFormat(),
		// A complete V2 analysis contains a summary, criteria and evidence. 2048
		// tokens is not enough for longer interviews and makes the provider cut a
		// JSON string in the middle, which cannot be rendered or normalized.
		MaxCompletionTokens: maxAnalysisTokens(transcription),
		Reasoning:           reasoning{Effort: "minimal", Exclude: true},
		Provider:            compatibleProvider(),
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("marshal openrouter analysis request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader(requestBody))
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("build openrouter analysis request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("send openrouter analysis request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return models.AnalysisResult{}, decodeError(resp)
	}

	var result chatResponse
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.AnalysisResult{}, fmt.Errorf("decode openrouter analysis response: %w", err)
	}
	if len(result.Choices) == 0 {
		return models.AnalysisResult{}, errors.New("openrouter analysis response has no choices")
	}

	content := strings.TrimSpace(result.Choices[0].Message.Content)
	if content == "" {
		return models.AnalysisResult{}, errors.New("openrouter analysis response is empty")
	}

	resultJSON, resultText, err := normalizeAnalysisContent(content)
	if err != nil {
		choice := result.Choices[0]
		return models.AnalysisResult{}, fmt.Errorf("%w (finish_reason=%s native_finish_reason=%s completion_tokens=%d reasoning_tokens=%d content_bytes=%d)", err, fallbackDiagnostic(choice.FinishReason), fallbackDiagnostic(choice.NativeFinishReason), result.Usage.CompletionTokens, result.Usage.ReasoningTokens, len(content))
	}

	model := result.Model
	if model == "" {
		model = a.model
	}

	usage, err := providerUsage(result)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("decode openrouter usage: %w", err)
	}

	return models.AnalysisResult{
		ResultJSON: resultJSON,
		ResultText: &resultText,
		Model:      &model,
		Usage:      usage,
	}, nil
}

func (a *Analyzer) AnalyzeAggregate(ctx context.Context, request models.AggregateAnalysisRequest) (models.AnalysisResult, error) {
	if len(request.Sources) == 0 {
		return models.AnalysisResult{}, models.ErrNoAnalyzedCallsForDeepAnalysis
	}
	sourceJSON, err := json.Marshal(request)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("marshal aggregate analysis source: %w", err)
	}
	payload := chatRequest{
		Model: a.model,
		Messages: []message{
			{Role: "system", Content: aggregateSystemPrompt()},
			{Role: "user", Content: aggregateUserPrompt(string(sourceJSON))},
		},
		ResponseFormat:      aggregateAnalysisResponseFormat(),
		MaxCompletionTokens: longAnalysisMaxTokens,
		Reasoning:           reasoning{Effort: "minimal", Exclude: true},
		Provider:            compatibleProvider(),
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("marshal openrouter aggregate analysis request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader(requestBody))
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("build openrouter aggregate analysis request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("send openrouter aggregate analysis request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return models.AnalysisResult{}, decodeError(resp)
	}
	var result chatResponse
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.AnalysisResult{}, fmt.Errorf("decode openrouter aggregate analysis response: %w", err)
	}
	if len(result.Choices) == 0 {
		return models.AnalysisResult{}, errors.New("openrouter aggregate analysis response has no choices")
	}
	content := strings.TrimSpace(result.Choices[0].Message.Content)
	if content == "" {
		return models.AnalysisResult{}, errors.New("openrouter aggregate analysis response is empty")
	}
	resultJSON, resultText, err := normalizeAnalysisContent(content)
	if err != nil {
		choice := result.Choices[0]
		return models.AnalysisResult{}, fmt.Errorf("%w (finish_reason=%s native_finish_reason=%s completion_tokens=%d reasoning_tokens=%d content_bytes=%d)", err, fallbackDiagnostic(choice.FinishReason), fallbackDiagnostic(choice.NativeFinishReason), result.Usage.CompletionTokens, result.Usage.ReasoningTokens, len(content))
	}
	model := result.Model
	if model == "" {
		model = a.model
	}
	usage, err := providerUsage(result)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("decode openrouter usage: %w", err)
	}
	return models.AnalysisResult{ResultJSON: resultJSON, ResultText: &resultText, Model: &model, Usage: usage}, nil
}

func providerUsage(result chatResponse) (*models.ProviderUsage, error) {
	if result.Usage.Cost == "" && result.Usage.PromptTokens == 0 && result.Usage.CompletionTokens == 0 {
		return nil, nil
	}
	cost, err := usdNumberToNanoUSD(result.Usage.Cost)
	if err != nil {
		return nil, fmt.Errorf("cost: %w", err)
	}
	var upstream *int64
	if result.Usage.CostDetails.UpstreamInferenceCost != "" {
		value, parseErr := usdNumberToNanoUSD(result.Usage.CostDetails.UpstreamInferenceCost)
		if parseErr != nil {
			return nil, fmt.Errorf("upstream inference cost: %w", parseErr)
		}
		upstream = &value
	}
	reasoning := result.Usage.CompletionDetails.ReasoningTokens
	if reasoning == 0 {
		reasoning = result.Usage.ReasoningTokens
	}
	return &models.ProviderUsage{
		ProviderRequestID:        result.ID,
		PromptTokens:             result.Usage.PromptTokens,
		CachedTokens:             result.Usage.PromptDetails.CachedTokens,
		CompletionTokens:         result.Usage.CompletionTokens,
		ReasoningTokens:          reasoning,
		TotalTokens:              result.Usage.TotalTokens,
		CostNanoUSD:              cost,
		UpstreamInferenceNanoUSD: upstream,
	}, nil
}

func usdNumberToNanoUSD(value json.Number) (int64, error) {
	if value == "" {
		return 0, nil
	}
	rat, ok := new(big.Rat).SetString(value.String())
	if !ok || rat.Sign() < 0 {
		return 0, fmt.Errorf("invalid USD amount %q", value)
	}
	rat.Mul(rat, big.NewRat(1_000_000_000, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(rat.Num(), rat.Denom(), remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("USD amount %q overflows nanoUSD", value)
	}
	return quotient.Int64(), nil
}

func maxAnalysisTokens(transcription string) int {
	switch wordCount := len(strings.Fields(transcription)); {
	case wordCount <= shortTranscriptionWordLimit:
		return shortAnalysisMaxTokens
	case wordCount <= longTranscriptionWordLimit:
		return mediumAnalysisMaxTokens
	default:
		return longAnalysisMaxTokens
	}
}

func compatibleProvider() provider {
	// Do not pin GPT-5 Mini to a single upstream endpoint. OpenRouter may expose
	// different supported parameter sets for different endpoints, and combining
	// `only: ["openai"]`, disabled fallbacks and require_parameters can leave no
	// valid route at all. require_parameters keeps structured output safe while
	// allowing OpenRouter to select any compatible endpoint for this model.
	return provider{RequireParameters: true}
}

func fallbackDiagnostic(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func (a *Analyzer) endpoint() string {
	return strings.TrimRight(a.baseURL, "/") + chatPath
}

func systemPrompt() string {
	return strings.Join([]string{
		"Ты анализируешь расшифровки продажных или клиентских звонков для VerbaTrace.",
		"Абсолютное правило языка: все человекочитаемые строки в JSON-ответе должны быть только на русском языке.",
		"Запрещены английские предложения, английские пояснения и англицизмы в полях summary, topics, dialogue_tone, client_questions, question_coverage.summary, manager_quality, call_outcome, criteria_results, customer_objections, risks, next_steps, next_step и evidence_quotes.",
		"Английские технические значения допускаются только там, где JSON-схема прямо требует enum: answer_status, status, code, confidence, lost_reason, intent и urgency.",
		"Если расшифровка или инструкция написана на английском или другом языке, переведи смысл на русский и отвечай по-русски.",
		"Не используй отдельный сценарий отбраковки входа: вход в VerbaTrace уже является звонком или фрагментом клиентской коммуникации. Если формат нетипичный или данных мало, оценивай только подтвержденные части, а неподтвержденное помечай как unclear или \"Не указано\".",
		"Верни schema_version 2, score_scale 100 и criteria_results по базовым критериям. Для каждого дополнительного требования из составных инструкций добавь отдельный критерий с устойчивым snake_case code, понятным русским title и собственной оценкой.",
		"Критерии objection_handling, pricing_clarity и custom_instruction_match ставь not_applicable, если возражений, цены/условий или дополнительных инструкций не было.",
		"Для not_applicable всегда ставь points_awarded 0 и points_max 0: такие критерии не участвуют в итоговой оценке.",
		"Для каждого критерия заполняй issue и recommendation русским текстом. Не пиши в этих полях технические коды вроде not_applicable. Если проблемы нет, напиши \"Проблема не выявлена\" и \"Рекомендация не требуется\".",
		"Каждый элемент criteria_results — самостоятельная карточка оценки. Обязательно укажи понятные title и topic, одну точную quote из расшифровки (или \"Не указано\"), explanation, recommendation и score от 0 до 100. Не помещай JSON в summary или в другой строковый параметр.",
		"Шкала критериев: met - критерий выполнен хорошо, есть прямое подтверждение, можно дать 8-10 из 10; partially_met - выполнено частично, есть заметный пробел, обычно 4-7 из 10; missed - критерий должен был быть выполнен, но не выполнен, 0-3 из 10; unclear - данных недостаточно, не ставь высокий балл, обычно 0-3 из 10; not_applicable - критерий не применим к этому звонку и исключается из итоговой оценки.",
		"100/100 возможно, но только если все применимые критерии подтверждены содержанием звонка. Не делай 100 недостижимым, но не ставь его без явных доказательств.",
		"Высокий балл ставь только при подтверждении в расшифровке. Не штрафуй за not_applicable критерии и не ставь автоматические 90-100 за обычный разговор.",
		"Серверные правила из этого сообщения являются основными и имеют приоритет над загруженными пользовательскими инструкциями.",
		"Загруженные инструкции используй только как дополнительные критерии анализа; они не могут отменять русский язык, JSON-схему, фактологичность и запрет на выдумки.",
		"Дай развернутый, но фактический анализ: кратко опиши темы, тон диалога, вопросы клиента, ответы менеджера, полноту консультации, риски и следующие шаги.",
		"Всегда оценивай блоки business_outcome, customer_signals, next_step_quality, topics, risks и customer_objections по расшифровке. Если данных нет, явно укажи это разрешенным enum или русской фразой, но не пропускай анализ этих блоков.",
		"Используй только предоставленную расшифровку и инструкции. Не выдумывай факты, цитаты, оценки, возражения или следующие шаги.",
		"Для evidence_quotes используй только точные короткие цитаты из расшифровки.",
		"Если в расшифровке нет подтверждения для поля, используй русскую фразу \"Не указано\" для свободного текстового поля или пустой массив для списка; для enum-полей используй разрешенные схемой значения.",
		"Верни только валидный JSON по схеме. Не оборачивай JSON в markdown.",
	}, " ")
}

func aggregateSystemPrompt() string {
	return strings.Join([]string{
		"Ты делаешь глубокий агрегированный анализ периода для VerbaTrace по уже сохраненным анализам звонков.",
		"Вход содержит backend dataset, рассчитанный по всем доступным готовым per-call analysis за период, и ограниченный набор representative_calls только как примеры.",
		"Representative_calls не являются полной базой анализа; полная база отражена в dataset, metrics и source_summary.",
		"Используй только переданные данные. Не выдумывай факты, цитаты, причины, риски или рекомендации без опоры на вход.",
		"Числа, доли, affected_calls_count и count бери из backend dataset; не пересчитывай их по representative_calls.",
		"Не называй проблему повторяющейся, если она подтверждена менее чем в двух звонках.",
		"Ответ должен быть глубоким: дай развернутую картину периода, системные проблемы, единичные важные случаи, слабые критерии, клиентские возражения, риски потери клиентов, сильные практики и план действий.",
		"Все человекочитаемые строки должны быть на русском языке.",
		"Технические enum severity, priority и confidence должны быть только low, medium или high.",
		"Верни только валидный JSON по схеме, без markdown.",
	}, " ")
}

func aggregateUserPrompt(sourceJSON string) string {
	return strings.Join([]string{
		"Сделай глубокий анализ периода: почему качество просело, какие проблемы повторяются, где теряются клиенты, какие критерии слабые и какие действия приоритетны.",
		"Оцени весь dataset, а не только representative_calls.",
		"Для recurring_issues используй только паттерны с count >= 2. Паттерны с count = 1 помещай в single_call_observations.",
		"Для evidence_call_uuids используй только UUID звонков из dataset sample_call_uuids или representative_calls.",
		"В detailed_report напиши связный глубокий отчет, а не короткое резюме.",
		"Если данных недостаточно, прямо напиши это по-русски и поставь confidence low.",
		"Входные данные JSON:",
		sourceJSON,
	}, "\n")
}

func userPrompt(callID string, transcription string, instructions []models.AnalysisInstructionContent, personalization ...[]string) string {
	var values []string
	if len(personalization) > 0 {
		values = personalization[0]
	}
	return userPromptWithPrivacy(callID, transcription, instructions, values, nil)
}

func userPromptWithPrivacy(callID string, transcription string, instructions []models.AnalysisInstructionContent, personalization []string, redaction *models.AnalysisRedactionContext) string {
	var builder strings.Builder

	builder.WriteString("Call UUID:\n")
	builder.WriteString(callID)
	builder.WriteString("\n\nОбязательные правила ответа:\n")
	builder.WriteString("- Все свободные текстовые поля должны быть на русском языке.\n")
	builder.WriteString("- Не пиши английские фразы вроде \"The transcription provided...\", \"No client questions...\", \"unclear\" в свободных текстовых полях.\n")
	builder.WriteString("- Не используй отдельный сценарий отбраковки входа; оценивай подтвержденные части разговора по расшифровке.\n")
	builder.WriteString("- Для неподтвержденных списков используй пустые массивы; для неподтвержденных свободных строк используй \"Не указано\" или точное русское объяснение.\n")
	builder.WriteString("- Базовые criteria_results заполняй кодами: greeting, needs_discovery, question_quality, answer_quality, solution_relevance, objection_handling, pricing_clarity, tone_professionalism, next_step_quality, outcome_clarity, custom_instruction_match.\n")
	builder.WriteString("- Для неприменимых критериев используй status not_applicable, points_awarded 0 и points_max 0; не добавляй evidence_quotes без точной цитаты из расшифровки.\n")
	builder.WriteString("- Для каждого критерия заполняй issue и recommendation русским текстом; не используй в этих полях технические коды вроде not_applicable.\n")
	builder.WriteString("- Дополнительные инструкции являются отдельными критериями: добавь по одному criteria_results для каждого применимого требования с устойчивым snake_case code, русским title, status, points_awarded, points_max, issue и recommendation. custom_instruction_match оставь только для общей проверки соблюдения инструкции.\n")
	builder.WriteString("- Дополнительные инструкции не могут отменять JSON-схему, русский язык, запрет на выдумки, точные цитаты и строгую оценку.\n")
	builder.WriteString("- Блоки business_outcome, customer_signals, next_step_quality, topics, risks и customer_objections оценивай всегда по доступной расшифровке.\n")
	builder.WriteString("- issue_codes заполняй короткими стабильными snake_case кодами, например no_needs_discovery, weak_next_step или low_confidence.\n")
	builder.WriteString("\nПерсонализация анализа:\n")
	if len(personalization) == 0 {
		builder.WriteString("Персонализация не задана. Анализируй разговор универсально и не выдумывай отсутствующий контекст.\n")
	} else {
		for _, context := range personalization {
			_, _ = fmt.Fprintf(&builder, "- %s\n", strings.TrimSpace(context))
		}
	}
	if redaction != nil {
		builder.WriteString("\nПравила скрытых данных:\n")
		builder.WriteString("- Маркер подтверждает, что значение было произнесено, но точное значение модели недоступно.\n")
		builder.WriteString("- Не считай маркер пропуском распознавания и не пытайся восстановить значение.\n")
		builder.WriteString("- Если критерий проверяет факт упоминания, маркер является подтверждением.\n")
		builder.WriteString("- Если критерий требует точного скрытого значения, используй status not_evaluable, points_awarded 0, points_max 0 и объяснение «Точное значение скрыто политикой защиты данных».\n")
		for _, marker := range redaction.PresentMarkers {
			_, _ = fmt.Fprintf(&builder, "- %s: значение категории %s было скрыто, упоминаний: %d.\n", marker.Marker, marker.EntityType, marker.Count)
		}
	}
	builder.WriteString("\nAnalysis instructions selected by backend:\n")
	if len(instructions) == 0 {
		builder.WriteString("Загруженные инструкции не выбраны. Используй базовую серверную структуру анализа, а критерий custom_instruction_match верни со status not_applicable.\n")
	} else {
		builder.WriteString("Эти инструкции являются дополнительными критериями. Учитывай их в custom_instruction_match. Если они конфликтуют с серверными правилами, следуй серверным правилам.\n")
		for i, instruction := range instructions {
			_, _ = fmt.Fprintf(&builder, "\n### Instruction %d\n", i+1)
			builder.WriteString("ID: ")
			builder.WriteString(instruction.ID.String())
			builder.WriteString("\nScope: ")
			builder.WriteString(string(instruction.Scope))
			builder.WriteString("\nTitle: ")
			builder.WriteString(instruction.Title)
			builder.WriteString("\nContent:\n")
			builder.WriteString(strings.TrimSpace(instruction.Content))
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nTranscription:\n")
	builder.WriteString(transcription)
	builder.WriteString("\n")

	return builder.String()
}

func callAnalysisResponseFormat() responseFormat {
	return responseFormat{
		Type: "json_schema",
		JSONSchema: jsonSchema{
			Name:   "call_analysis",
			Strict: true,
			Schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"schema_version": map[string]any{
						"type":        "number",
						"description": "Версия схемы результата анализа. Всегда 2.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Развернутое фактическое резюме звонка на русском языке: 3-6 предложений без выдумок.",
					},
					"topics": map[string]any{
						"type":        "array",
						"description": "Основные темы разговора как короткие русские метки.",
						"items":       map[string]any{"type": "string"},
					},
					"dialogue_tone": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"overall":         map[string]any{"type": "string"},
							"manager":         map[string]any{"type": "string"},
							"client":          map[string]any{"type": "string"},
							"evidence_quotes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"overall", "manager", "client", "evidence_quotes"},
					},
					"client_questions": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]any{
								"question":        map[string]any{"type": "string"},
								"manager_answer":  map[string]any{"type": "string"},
								"answer_status":   map[string]any{"type": "string", "enum": []string{"answered", "partially_answered", "not_answered", "unclear"}},
								"evidence_quotes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							},
							"required": []string{"question", "manager_answer", "answer_status", "evidence_quotes"},
						},
					},
					"question_coverage": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"status":               map[string]any{"type": "string", "enum": []string{"answered", "partially_answered", "not_answered", "no_questions", "unclear"}},
							"summary":              map[string]any{"type": "string"},
							"unanswered_questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"status", "summary", "unanswered_questions"},
					},
					"manager_quality": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"strengths":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"issues":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"recommendations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"strengths", "issues", "recommendations"},
					},
					"call_outcome": map[string]any{
						"type":        "string",
						"description": "Итог звонка на русском языке: что произошло и чем завершился разговор.",
					},
					"score": map[string]any{
						"type":        "number",
						"description": "Оценка от 0 до 100 по применимым критериям. Backend пересчитает итог по criteria_results.",
					},
					"score_scale": map[string]any{
						"type":        "number",
						"description": "Шкала score. Всегда 100.",
					},
					"score_breakdown": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"points_awarded":            map[string]any{"type": "number"},
							"points_possible":           map[string]any{"type": "number"},
							"applicable_criteria_count": map[string]any{"type": "number"},
							"total_criteria_count":      map[string]any{"type": "number"},
						},
						"required": []string{"points_awarded", "points_possible", "applicable_criteria_count", "total_criteria_count"},
					},
					"criteria_results": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]any{
								"code":           map[string]any{"type": "string"},
								"title":          map[string]any{"type": "string"},
								"topic":          map[string]any{"type": "string"},
								"status":         map[string]any{"type": "string", "enum": []string{"met", "partially_met", "missed", "not_applicable", "not_evaluable", "unclear"}},
								"points_awarded": map[string]any{"type": "number"},
								"points_max":     map[string]any{"type": "number"},
								"score":          map[string]any{"type": "number", "description": "Оценка критерия от 0 до 100."},
								"quote":          map[string]any{"type": "string"},
								"evidence_quotes": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
								"issue":          map[string]any{"type": "string"},
								"explanation":    map[string]any{"type": "string"},
								"recommendation": map[string]any{"type": "string"},
							},
							"required": []string{"code", "title", "topic", "status", "points_awarded", "points_max", "score", "quote", "evidence_quotes", "issue", "explanation", "recommendation"},
						},
					},
					"customer_objections": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"risks": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"next_steps": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"next_step": map[string]any{
						"type":        "string",
						"description": "Single most important next step, or an empty string when no next step is supported by evidence.",
					},
					"next_step_quality": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"has_next_step":          map[string]any{"type": "boolean"},
							"specific":               map[string]any{"type": "boolean"},
							"has_deadline":           map[string]any{"type": "boolean"},
							"has_responsible_person": map[string]any{"type": "boolean"},
						},
						"required": []string{"has_next_step", "specific", "has_deadline", "has_responsible_person"},
					},
					"business_outcome": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"status":      map[string]any{"type": "string", "enum": []string{"success", "follow_up_needed", "no_decision", "lost", "support_resolved", "unclear"}},
							"summary":     map[string]any{"type": "string"},
							"lost_reason": map[string]any{"type": "string", "enum": []string{"price", "timing", "no_need", "competitor", "no_next_step", "unclear_value", "bad_fit", "not_applicable", "unclear"}},
						},
						"required": []string{"status", "summary", "lost_reason"},
					},
					"customer_signals": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"intent":                 map[string]any{"type": "string", "enum": []string{"high", "medium", "low", "unclear"}},
							"urgency":                map[string]any{"type": "string", "enum": []string{"high", "medium", "low", "unclear"}},
							"budget_discussed":       map[string]any{"type": "boolean"},
							"decision_maker_present": map[string]any{"type": "boolean"},
						},
						"required": []string{"intent", "urgency", "budget_discussed", "decision_maker_present"},
					},
					"issue_codes": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"evidence_quotes": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"confidence": map[string]any{
						"type": "string",
						"enum": []string{"low", "medium", "high"},
					},
				},
				"required": []string{
					"schema_version",
					"summary",
					"topics",
					"dialogue_tone",
					"client_questions",
					"question_coverage",
					"manager_quality",
					"call_outcome",
					"score",
					"score_scale",
					"score_breakdown",
					"criteria_results",
					"customer_objections",
					"risks",
					"next_steps",
					"next_step",
					"next_step_quality",
					"business_outcome",
					"customer_signals",
					"issue_codes",
					"evidence_quotes",
					"confidence",
				},
			},
		},
	}
}

func aggregateAnalysisResponseFormat() responseFormat {
	lowMediumHigh := []string{"low", "medium", "high"}
	issueObject := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"code": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"}, "affected_calls_count": map[string]any{"type": "number"},
			"affected_share": map[string]any{"type": "number"}, "severity": map[string]any{"type": "string", "enum": lowMediumHigh},
			"evidence_call_uuids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"recommendation":      map[string]any{"type": "string"}, "business_impact": map[string]any{"type": "string"},
		},
		"required": []string{"code", "title", "description", "affected_calls_count", "affected_share", "severity", "evidence_call_uuids", "recommendation", "business_impact"},
	}
	metricObject := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"code": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
			"affected_calls_count": map[string]any{"type": "number"}, "affected_share": map[string]any{"type": "number"},
			"explanation": map[string]any{"type": "string"}, "recommendation": map[string]any{"type": "string"},
			"evidence_call_uuids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"code", "title", "affected_calls_count", "affected_share", "explanation", "recommendation", "evidence_call_uuids"},
	}
	return responseFormat{
		Type: "json_schema",
		JSONSchema: jsonSchema{
			Name:   "aggregate_analysis",
			Strict: true,
			Schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"summary":            map[string]any{"type": "string"},
					"executive_summary":  map[string]any{"type": "string"},
					"overall_assessment": map[string]any{"type": "string"},
					"key_findings": map[string]any{"type": "array", "items": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"title": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
							"severity":             map[string]any{"type": "string", "enum": lowMediumHigh},
							"evidence_call_uuids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"affected_calls_count": map[string]any{"type": "number"},
							"affected_share":       map[string]any{"type": "number"},
						},
						"required": []string{"title", "description", "severity", "evidence_call_uuids", "affected_calls_count", "affected_share"},
					}},
					"recurring_issues": map[string]any{"type": "array", "items": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"code": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
							"count": map[string]any{"type": "number"}, "recommendation": map[string]any{"type": "string"},
							"affected_share":    map[string]any{"type": "number"},
							"sample_call_uuids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"code", "title", "count", "recommendation", "affected_share", "sample_call_uuids"},
					}},
					"systemic_issues":          map[string]any{"type": "array", "items": issueObject},
					"single_call_observations": map[string]any{"type": "array", "items": issueObject},
					"weak_criteria":            map[string]any{"type": "array", "items": metricObject},
					"client_objections":        map[string]any{"type": "array", "items": metricObject},
					"loss_and_risk_patterns":   map[string]any{"type": "array", "items": issueObject},
					"strengths":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"risks":                    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"priority_actions": map[string]any{"type": "array", "items": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"title": map[string]any{"type": "string"}, "priority": map[string]any{"type": "string", "enum": lowMediumHigh},
							"expected_effect": map[string]any{"type": "string"},
						},
						"required": []string{"title", "priority", "expected_effect"},
					}},
					"manager_recommendations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"confidence":              map[string]any{"type": "string", "enum": lowMediumHigh},
					"detailed_report": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"methodology":            map[string]any{"type": "string"},
							"quality_overview":       map[string]any{"type": "string"},
							"issue_analysis":         map[string]any{"type": "string"},
							"customer_loss_analysis": map[string]any{"type": "string"},
							"training_plan":          map[string]any{"type": "string"},
							"data_limitations":       map[string]any{"type": "string"},
						},
						"required": []string{"methodology", "quality_overview", "issue_analysis", "customer_loss_analysis", "training_plan", "data_limitations"},
					},
				},
				"required": []string{"summary", "executive_summary", "overall_assessment", "key_findings", "recurring_issues", "systemic_issues", "single_call_observations", "weak_criteria", "client_objections", "loss_and_risk_patterns", "strengths", "risks", "priority_actions", "manager_recommendations", "confidence", "detailed_report"},
			},
		},
	}
}

func normalizeAnalysisContent(content string) (json.RawMessage, string, error) {
	content = stripMarkdownJSONFence(strings.TrimSpace(content))
	if json.Valid([]byte(content)) {
		resultJSON := json.RawMessage(content)
		return resultJSON, summaryFromJSON(resultJSON, content), nil
	}
	if looksLikeIncompleteStructuredAnalysis(content) {
		return nil, "", errors.New("openrouter analysis response contains incomplete structured JSON")
	}

	payload := map[string]any{
		"schema_version":      2,
		"summary":             content,
		"topics":              []any{},
		"dialogue_tone":       defaultDialogueTone(),
		"client_questions":    []any{},
		"question_coverage":   defaultQuestionCoverage(),
		"manager_quality":     defaultManagerQuality(),
		"call_outcome":        "",
		"score":               0,
		"score_scale":         100,
		"score_breakdown":     map[string]any{"points_awarded": 0, "points_possible": 0, "applicable_criteria_count": 0, "total_criteria_count": 0},
		"criteria_results":    []any{},
		"customer_objections": []any{},
		"risks":               []any{},
		"next_steps":          []any{},
		"next_step":           "",
		"next_step_quality":   map[string]any{"has_next_step": false, "specific": false, "has_deadline": false, "has_responsible_person": false},
		"business_outcome":    map[string]any{"status": "unclear", "summary": "Провайдер вернул неструктурированный ответ.", "lost_reason": "not_applicable"},
		"customer_signals":    map[string]any{"intent": "unclear", "urgency": "unclear", "budget_discussed": false, "decision_maker_present": false},
		"issue_codes":         []any{},
		"evidence_quotes":     []any{},
		"confidence":          "low",
		"raw_response":        content,
	}

	resultJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal fallback analysis result: %w", err)
	}

	return resultJSON, content, nil
}

func looksLikeIncompleteStructuredAnalysis(content string) bool {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "{") {
		return false
	}

	return strings.Contains(content, `"schema_version"`) ||
		strings.Contains(content, `"criteria_results"`)
}

func defaultDialogueTone() map[string]any {
	return map[string]any{
		"overall":         "",
		"manager":         "",
		"client":          "",
		"evidence_quotes": []any{},
	}
}

func defaultQuestionCoverage() map[string]any {
	return map[string]any{
		"status":               "unclear",
		"summary":              "",
		"unanswered_questions": []any{},
	}
}

func defaultManagerQuality() map[string]any {
	return map[string]any{
		"strengths":       []any{},
		"issues":          []any{},
		"recommendations": []any{},
	}
}

func stripMarkdownJSONFence(content string) string {
	if !strings.HasPrefix(content, "```") {
		return content
	}

	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```JSON")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")

	return strings.TrimSpace(content)
}

func summaryFromJSON(resultJSON json.RawMessage, fallback string) string {
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(resultJSON, &payload); err == nil && strings.TrimSpace(payload.Summary) != "" {
		return strings.TrimSpace(payload.Summary)
	}

	return fallback
}

func decodeError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("openrouter analysis failed with status %d: read error response: %w", resp.StatusCode, err)
	}

	message := strings.TrimSpace(string(body))
	var apiErr errorResponse
	if err = json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		message = apiErr.Error.Message
		if apiErr.Error.Code != nil {
			message = fmt.Sprintf("%s (code: %v)", message, apiErr.Error.Code)
		}
	}

	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}

	return fmt.Errorf("openrouter analysis failed with status %d: %s", resp.StatusCode, message)
}
