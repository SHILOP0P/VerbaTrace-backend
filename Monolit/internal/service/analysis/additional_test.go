package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	mockAnalyzer "calllens/monolit/internal/analyzer/mock"
	analyzerMocks "calllens/monolit/internal/analyzer/mocks"
	"calllens/monolit/internal/models"
	repositoryMocks "calllens/monolit/internal/repository/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestGetByCallUUID(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	analysisRepo := repositoryMocks.NewAnalysisRepository(t)
	existing := models.CallAnalysis{ID: uuid.New(), CallUUID: callID}
	callRepo.EXPECT().GetByUUID(mock.Anything, callID, userID).Return(models.Call{ID: callID}, nil).Once()
	analysisRepo.EXPECT().GetByCallUUID(mock.Anything, callID).Return(existing, nil).Once()
	service := NewService(callRepo, nil, nil, analysisRepo, nil, nil, nil)

	got, err := service.GetByCallUUID(context.Background(), callID, userID)
	if err != nil || got.ID != existing.ID {
		t.Fatalf("GetByCallUUID = %+v, %v", got, err)
	}
	if _, err := service.GetByCallUUID(context.Background(), uuid.Nil, userID); !errors.Is(err, models.ErrInvalidAnalysisInput) {
		t.Fatalf("invalid input error = %v", err)
	}
}

func TestAnalyzeCallValidationAndStatus(t *testing.T) {
	service := NewService(nil, nil, nil, nil, nil, nil, nil)
	if _, err := service.AnalyzeCall(context.Background(), models.AnalyzeCallInput{}); !errors.Is(err, models.ErrInvalidAnalysisInput) {
		t.Fatalf("invalid input error = %v", err)
	}

	callID := uuid.New()
	userID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	transcriptionRepo := repositoryMocks.NewTranscriptionRepository(t)
	callRepo.EXPECT().GetByUUID(mock.Anything, callID, userID).Return(models.Call{ID: callID}, nil).Once()
	transcriptionRepo.EXPECT().GetByCallUUID(mock.Anything, callID).Return(models.Transcription{
		CallUUID: callID, Status: models.TranscriptionStatusProcessing,
	}, nil).Once()
	service = NewService(callRepo, transcriptionRepo, nil, repositoryMocks.NewAnalysisRepository(t), nil, nil, nil)
	if _, err := service.AnalyzeCall(context.Background(), models.AnalyzeCallInput{CallUUID: callID, UserUUID: userID}); !errors.Is(err, models.ErrInvalidAnalysisStatus) {
		t.Fatalf("status error = %v", err)
	}
}

func TestProcessAnalyzeCallValidation(t *testing.T) {
	service := NewService(nil, nil, nil, nil, nil, nil, nil)
	if err := service.ProcessAnalyzeCall(context.Background(), uuid.Nil); !errors.Is(err, models.ErrCallNotFound) {
		t.Fatalf("nil call error = %v", err)
	}
	if err := service.ProcessAnalyzeCall(context.Background(), uuid.New()); !errors.Is(err, models.ErrAnalyzerNotConfigured) {
		t.Fatalf("missing analyzer error = %v", err)
	}

	callID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	analyzer := analyzerMocks.NewAnalyzer(t)
	callRepo.EXPECT().GetByUUIDForProcessing(mock.Anything, callID).
		Return(models.Call{ID: callID, Status: models.CallStatusAnalyzed}, nil).Once()
	service = NewService(callRepo, nil, nil, nil, nil, analyzer, nil)
	if err := service.ProcessAnalyzeCall(context.Background(), callID); err != nil {
		t.Fatalf("already analyzed: %v", err)
	}

	callRepo = repositoryMocks.NewCallRepository(t)
	callRepo.EXPECT().GetByUUIDForProcessing(mock.Anything, callID).
		Return(models.Call{ID: callID, Status: models.CallStatusTranscribed}, nil).Once()
	service = NewService(callRepo, nil, nil, nil, nil, analyzerMocks.NewAnalyzer(t), nil)
	if err := service.ProcessAnalyzeCall(context.Background(), callID); !errors.Is(err, models.ErrInvalidAnalysisInput) {
		t.Fatalf("missing uploader error = %v", err)
	}
}

func TestMarkAnalyzeCallFailedCreatesMissingAnalysis(t *testing.T) {
	callID := uuid.New()
	callRepo := repositoryMocks.NewCallRepository(t)
	analysisRepo := repositoryMocks.NewAnalysisRepository(t)
	analysisRepo.EXPECT().GetByCallUUID(mock.Anything, callID).
		Return(models.CallAnalysis{}, models.ErrAnalysisNotFound).Once()
	analysisRepo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(value models.CallAnalysis) bool {
		return value.CallUUID == callID && value.Status == models.CallAnalysisStatusFailed
	})).Return(models.CallAnalysis{ID: uuid.New(), CallUUID: callID, Status: models.CallAnalysisStatusFailed}, nil).Once()
	callRepo.EXPECT().UpdateCallStatus(mock.Anything, callID, models.CallStatusFailed).
		Return(models.Call{ID: callID, Status: models.CallStatusFailed}, nil).Once()
	service := NewService(callRepo, nil, nil, analysisRepo, nil, nil, nil)
	if err := service.MarkAnalyzeCallFailed(context.Background(), callID, nil); err != nil {
		t.Fatalf("MarkAnalyzeCallFailed: %v", err)
	}
	if err := service.MarkAnalyzeCallFailed(context.Background(), uuid.Nil, nil); !errors.Is(err, models.ErrCallNotFound) {
		t.Fatalf("nil call error = %v", err)
	}
}

func TestNormalizationHelpers(t *testing.T) {
	text := "summary"
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultText: &text})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.ResultJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["summary"] != text || payload["confidence"] != "low" {
		t.Fatalf("payload = %+v", payload)
	}
	if _, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte("{")}); err == nil {
		t.Fatal("expected invalid JSON error")
	}

	values := []any{1, " ", "next"}
	if firstStringFromArray(values) != "next" || firstStringFromArray("bad") != "" {
		t.Fatal("firstStringFromArray mismatch")
	}
	object := map[string]any{"existing": "value"}
	ensureObjectFields(object, "nested", map[string]any{"x": 1})
	ensureObjectFields(object, "nested", map[string]any{"y": 2})
	ensureStringField(object, "string")
	ensureNumberField(object, "number")
	ensureConfidenceField(object)
}

func TestNormalizeAnalysisResultRewritesKnownEnglishFallbacks(t *testing.T) {
	text := "The transcription provided does not contain a sales or client call. It is a text about the history and new directions of advertising, including the use of human billboards. Therefore, no analysis of a sales or client call can be provided."
	result, err := normalizeAnalysisResult(models.AnalysisResult{
		ResultJSON: []byte(`{
			"summary":"The transcription provided does not contain a sales or client call. It is a text about the history and new directions of advertising, including the use of human billboards. Therefore, no analysis of a sales or client call can be provided.",
			"dialogue_tone":{"overall":"unclear","manager":"unclear","client":"unclear","evidence_quotes":[]},
			"question_coverage":{"status":"unclear","summary":"No client questions were identified in the transcription.","unanswered_questions":[]},
			"call_outcome":"unclear",
			"next_steps":["unclear"],
			"confidence":"low"
		}`),
		ResultText: &text,
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(result.ResultJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["summary"] != "Не удалось надежно определить итог разговора по расшифровке." {
		t.Fatalf("summary = %q", payload["summary"])
	}
	if result.ResultText == nil || strings.Contains(*result.ResultText, "The transcription") {
		t.Fatalf("result text was not normalized: %v", result.ResultText)
	}
	dialogueTone := payload["dialogue_tone"].(map[string]any)
	if dialogueTone["overall"] != "Неясно" {
		t.Fatalf("dialogue tone = %#v", dialogueTone)
	}
	questionCoverage := payload["question_coverage"].(map[string]any)
	if questionCoverage["status"] != "unclear" || questionCoverage["summary"] != "В расшифровке не выявлены вопросы клиента." {
		t.Fatalf("question coverage = %#v", questionCoverage)
	}
	if payload["call_outcome"] != "Неясно" {
		t.Fatalf("call outcome = %q", payload["call_outcome"])
	}
	nextSteps := payload["next_steps"].([]any)
	if len(nextSteps) != 1 || nextSteps[0] != "Неясно" {
		t.Fatalf("next steps = %#v", nextSteps)
	}
}

func TestNormalizeAnalysisResultV2ContractAndScoreScaling(t *testing.T) {
	for name, tc := range map[string]struct {
		json      string
		wantScore float64
	}{
		"five point scale": {json: `{"summary":"ok","score":4.5}`, wantScore: 90},
		"ten point scale":  {json: `{"summary":"ok","score":8}`, wantScore: 80},
		"hundred scale":    {json: `{"summary":"ok","score":76}`, wantScore: 76},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(tc.json)})
			if err != nil {
				t.Fatal(err)
			}
			payload := decodeAnalysisPayload(t, result)
			if payload["schema_version"] != float64(2) || payload["score_scale"] != float64(100) {
				t.Fatalf("version/scale = %#v", payload)
			}
			if payload["score"] != tc.wantScore {
				t.Fatalf("score = %v, want %v", payload["score"], tc.wantScore)
			}
			if _, ok := payload["business_outcome"].(map[string]any); !ok {
				t.Fatalf("missing business_outcome: %#v", payload)
			}
		})
	}
}

func TestNormalizeAnalysisResultCriteriaOverrideScore(t *testing.T) {
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{
		"summary":"ok",
		"score":10,
		"criteria_results":[
			{"code":"greeting","status":"met","points_awarded":10,"points_max":10,"evidence_quotes":["Здравствуйте"]},
			{"code":"needs_discovery","status":"missed","points_awarded":0,"points_max":10},
			{"code":"pricing_clarity","status":"not_applicable","points_awarded":10,"points_max":10},
			{"code":"tone_professionalism","status":"bad","points_awarded":5,"points_max":10}
		],
		"issue_codes":["","late_followup",123],
		"business_outcome":{"status":"bad","lost_reason":"wrong"},
		"customer_signals":{"intent":"hot","urgency":"medium","budget_discussed":"yes"},
		"question_coverage":{"status":"unclear","summary":"unclear"}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeAnalysisPayload(t, result)
	if payload["score"] != float64(50) {
		t.Fatalf("score = %v", payload["score"])
	}
	breakdown := payload["score_breakdown"].(map[string]any)
	if breakdown["points_awarded"] != float64(15) || breakdown["points_possible"] != float64(30) || breakdown["applicable_criteria_count"] != float64(3) {
		t.Fatalf("breakdown = %#v", breakdown)
	}
	criteria := payload["criteria_results"].([]any)
	if len(criteria) != 4 {
		t.Fatalf("criteria len = %d", len(criteria))
	}
	if criteria[0].(map[string]any)["issue"] != "Проблема не выявлена." {
		t.Fatalf("met criterion issue was not normalized: %#v", criteria[0])
	}
	if criteria[2].(map[string]any)["points_max"] != float64(0) {
		t.Fatalf("not_applicable points_max = %#v", criteria[2])
	}
	if criteria[3].(map[string]any)["status"] != "unclear" {
		t.Fatalf("unknown status was not normalized: %#v", criteria[3])
	}
	coverage := payload["question_coverage"].(map[string]any)
	if coverage["status"] != "unclear" {
		t.Fatalf("enum was translated: %#v", coverage)
	}
	issueCodes := payload["issue_codes"].([]any)
	if len(issueCodes) != 1 || issueCodes[0] != "late_followup" {
		t.Fatalf("issue_codes = %#v", issueCodes)
	}
}

func TestNormalizeAnalysisResultRepairsCriterionScoreAndLegacyNotCall(t *testing.T) {
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{
		"summary":"ok",
		"criteria_results":[
			{"code":"greeting","status":"met","points_awarded":0,"points_max":10,"issue":"not_applicable","recommendation":"not_applicable","evidence_quotes":["Здравствуйте"]},
			{"code":"next_step_quality","status":"partially_met","points_awarded":0,"points_max":10,"evidence_quotes":["Свяжутся позже"]},
			{"code":"pricing_clarity","status":"not_applicable","points_awarded":10,"points_max":10,"evidence_quotes":[]}
		],
		"business_outcome":{"status":"not_call","summary":"","lost_reason":"bad"},
		"next_steps":["Свяжутся позже"],
		"next_step_quality":{"has_next_step":false,"specific":false}
	}`)})
	if err != nil {
		t.Fatal(err)
	}

	payload := decodeAnalysisPayload(t, result)
	if payload["score"] != float64(75) {
		t.Fatalf("score = %v", payload["score"])
	}
	criteria := payload["criteria_results"].([]any)
	if criteria[0].(map[string]any)["points_awarded"] != float64(10) {
		t.Fatalf("met criterion points = %#v", criteria[0])
	}
	if criteria[1].(map[string]any)["points_awarded"] != float64(5) {
		t.Fatalf("partial criterion points = %#v", criteria[1])
	}
	if criteria[2].(map[string]any)["points_max"] != float64(0) {
		t.Fatalf("not applicable criterion = %#v", criteria[2])
	}
	businessOutcome := payload["business_outcome"].(map[string]any)
	if businessOutcome["status"] != "unclear" || businessOutcome["summary"] != "Не указано" || businessOutcome["lost_reason"] != "not_applicable" {
		t.Fatalf("business outcome = %#v", businessOutcome)
	}
	nextStepQuality := payload["next_step_quality"].(map[string]any)
	if nextStepQuality["has_next_step"] != true || nextStepQuality["specific"] != true {
		t.Fatalf("next step quality = %#v", nextStepQuality)
	}
}

func TestNormalizeAnalysisResultLegacyCriteriaCompatibility(t *testing.T) {
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{
		"summary":"ok",
		"topics":"bad",
		"criteria_results":[{"instruction_title":"Приветствие","result":"Выполнено","evidence_quotes":["Здравствуйте"]}]
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeAnalysisPayload(t, result)
	if len(payload["topics"].([]any)) != 0 {
		t.Fatalf("topics = %#v", payload["topics"])
	}
	criterion := payload["criteria_results"].([]any)[0].(map[string]any)
	if criterion["code"] != "custom_instruction_match" || criterion["status"] != "unclear" || criterion["points_max"] != float64(0) {
		t.Fatalf("legacy criterion = %#v", criterion)
	}
}

func TestNormalizeAnalysisResultPreservesComposedPromptCriterion(t *testing.T) {
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{
		"summary":"ok",
		"criteria_results":[{
			"code":"follow_up_date",
			"title":"Согласование даты следующего контакта",
			"status":"needs_review",
			"points_awarded":3,
			"points_max":5,
			"issue":"Дата не подтверждена.",
			"recommendation":"Согласовать дату.",
			"evidence_quotes":[]
		}]
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	criterion := decodeAnalysisPayload(t, result)["criteria_results"].([]any)[0].(map[string]any)
	if criterion["code"] != "follow_up_date" || criterion["status"] != "needs_review" || criterion["points_max"] != float64(5) {
		t.Fatalf("composed criterion was changed: %#v", criterion)
	}
	if criterion["topic"] != "Согласование даты следующего контакта" || criterion["quote"] != "Не указано" || criterion["explanation"] != "Дата не подтверждена." || criterion["score"] != float64(60) {
		t.Fatalf("criterion card fields = %#v", criterion)
	}
}

func TestNormalizeAnalysisResultUnwrapsJSONFromSummary(t *testing.T) {
	nested := `{"schema_version":2,"summary":"Краткий итог.","criteria_results":[{"code":"greeting","title":"Приветствие","status":"met","points_awarded":10,"points_max":10,"evidence_quotes":["Здравствуйте"],"issue":"Проблема не выявлена.","recommendation":"Рекомендация не требуется."}]}`
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{"summary":` + strconv.Quote(nested) + `}`)})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeAnalysisPayload(t, result)
	if payload["summary"] != "Краткий итог." {
		t.Fatalf("summary = %#v", payload["summary"])
	}
	criterion := payload["criteria_results"].([]any)[0].(map[string]any)
	if criterion["quote"] != "Здравствуйте" || criterion["topic"] != "Приветствие" || criterion["score"] != float64(100) {
		t.Fatalf("unwrapped criterion = %#v", criterion)
	}
}

func TestNormalizeAnalysisResultRejectsBrokenJSONInSummary(t *testing.T) {
	broken := `{"schema_version":2,"summary":"Оборванный ответ"`
	_, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{"schema_version":2,"summary":` + strconv.Quote(broken) + `}`)})
	if err == nil {
		t.Fatal("expected invalid nested structured JSON to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid nested structured JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeAnalysisResultAlwaysUsesReadableSummaryAsResultText(t *testing.T) {
	rawText := `{"schema_version":2,"summary":"Краткий итог."}`
	result, err := normalizeAnalysisResult(models.AnalysisResult{
		ResultJSON: []byte(rawText),
		ResultText: &rawText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultText == nil || *result.ResultText != "Краткий итог." {
		t.Fatalf("result text = %#v", result.ResultText)
	}
}

func TestMockAnalyzerReturnsV2JSON(t *testing.T) {
	analyzer := mockAnalyzer.New("test-model")
	result, err := analyzer.Analyze(context.Background(), models.AnalysisRequest{CallUUID: uuid.New(), Transcription: "text"})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeAnalysisPayload(t, result)
	if payload["schema_version"] != float64(2) || payload["score_scale"] != float64(100) {
		t.Fatalf("mock payload = %#v", payload)
	}
	criteria, ok := payload["criteria_results"].([]any)
	if !ok || len(criteria) < 2 {
		t.Fatalf("mock criteria = %#v", payload["criteria_results"])
	}
}

func decodeAnalysisPayload(t *testing.T, result models.AnalysisResult) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(result.ResultJSON, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestServiceConfigurationAndInstructionSelection(t *testing.T) {
	instructionRepo := repositoryMocks.NewAnalysisInstructionRepository(t)
	service := NewService(nil, nil, instructionRepo, nil, nil, nil, nil)
	service.SetProcessingJobMaxAttempts(0)
	if service.processingJobMaxAttempts != models.DefaultProcessingJobMaxAttempts {
		t.Fatalf("max attempts = %d", service.processingJobMaxAttempts)
	}
	if service.analyzerProviderName() != "unknown" {
		t.Fatalf("provider = %q", service.analyzerProviderName())
	}
	if _, err := service.selectInstructions(context.Background(), models.Call{VisibilityScope: "invalid"}, uuid.New()); !errors.Is(err, models.ErrInvalidAnalysisInput) {
		t.Fatalf("selection error = %v", err)
	}
	instructionRepo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, nil).Twice()
	if _, err := service.selectInstructions(context.Background(), models.Call{VisibilityScope: models.CallVisibilityScopePersonal}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.selectInstructions(context.Background(), models.Call{VisibilityScope: models.CallVisibilityScopeCompany}, uuid.New()); err != nil {
		t.Fatal(err)
	}
}
