package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) AnalyzeCall(ctx context.Context, input models.AnalyzeCallInput) (models.CallAnalysis, error) {
	if input.CallUUID == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CallAnalysis{}, models.ErrInvalidAnalysisInput
	}

	call, err := s.callRepository.GetByUUID(ctx, input.CallUUID, input.UserUUID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("get call: %w", err)
	}

	transcription, err := s.transcriptionRepository.GetByCallUUID(ctx, call.ID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("get transcription: %w", err)
	}

	if transcription.Status != models.TranscriptionStatusTranscribed || transcription.Text == nil {
		return models.CallAnalysis{}, models.ErrInvalidAnalysisStatus
	}
	analysis, err := s.createPendingAnalysis(ctx, call.ID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("create analysis: %w", err)
	}

	if err = s.enqueueAnalyzeJob(ctx, call.ID); err != nil {
		return models.CallAnalysis{}, fmt.Errorf("enqueue analysis job: %w", err)
	}

	s.log.Info(ctx, "call analysis job enqueued", zap.String("call_id", call.ID.String()), zap.String("analysis_id", analysis.ID.String()))

	return analysis, nil
}

func (s *Service) ProcessAnalyzeCall(ctx context.Context, callID uuid.UUID) error {
	if callID == uuid.Nil {
		return models.ErrCallNotFound
	}

	if s.analyzer == nil {
		return models.ErrAnalyzerNotConfigured
	}

	call, err := s.callRepository.GetByUUIDForProcessing(ctx, callID)
	if err != nil {
		return fmt.Errorf("get call for analysis processing: %w", err)
	}

	if call.Status == models.CallStatusAnalyzed {
		s.log.Info(ctx, "call already analyzed", zap.String("call_id", call.ID.String()))
		return nil
	}

	if !call.UploadedByUserUUID.Valid {
		return models.ErrInvalidAnalysisInput
	}

	_, err = s.analyzeCall(ctx, call, call.UploadedByUserUUID.UUID, analyzeCallOptions{
		markAttemptFailed: false,
	})
	return err
}

func (s *Service) MarkAnalyzeCallFailed(ctx context.Context, callID uuid.UUID, cause error) error {
	if callID == uuid.Nil {
		return models.ErrCallNotFound
	}

	errorMessage := "analysis failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		errorMessage = cause.Error()
	}

	analysis, err := s.analysisRepository.GetByCallUUID(ctx, callID)
	if err != nil {
		if !errors.Is(err, models.ErrAnalysisNotFound) {
			return fmt.Errorf("get analysis for failure: %w", err)
		}

		failedAnalysis, createErr := s.createFailedAnalysis(ctx, callID, errorMessage)
		if createErr != nil {
			return fmt.Errorf("create failed analysis: %w", createErr)
		}
		analysis = failedAnalysis
	} else {
		failedAnalysis, markErr := s.analysisRepository.MarkFailed(ctx, analysis.ID, errorMessage)
		if markErr != nil {
			return fmt.Errorf("mark analysis failed: %w", markErr)
		}
		analysis = failedAnalysis
	}

	if _, err := s.callRepository.UpdateCallStatus(ctx, callID, models.CallStatusFailed); err != nil {
		return fmt.Errorf("mark call failed: %w", err)
	}

	s.log.Error(ctx, "call analysis permanently failed", zap.String("call_id", callID.String()), zap.String("analysis_id", analysis.ID.String()), zap.Error(cause))

	return nil
}

type analyzeCallOptions struct {
	markAttemptFailed bool
}

func (s *Service) analyzeCall(ctx context.Context, call models.Call, userID uuid.UUID, opts analyzeCallOptions) (models.CallAnalysis, error) {
	transcription, err := s.transcriptionRepository.GetByCallUUID(ctx, call.ID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("get transcription: %w", err)
	}

	if transcription.Status != models.TranscriptionStatusTranscribed || transcription.Text == nil {
		return models.CallAnalysis{}, models.ErrInvalidAnalysisStatus
	}

	instructions, err := s.loadInstructions(ctx, call, userID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("load instructions: %w", err)
	}

	var personalization []string
	if s.personalizationReader != nil {
		personalization, err = s.personalizationReader.ContextForCall(ctx, call)
		if err != nil {
			return models.CallAnalysis{}, fmt.Errorf("load analysis personalization: %w", err)
		}
	}

	analysis, err := s.createPendingAnalysis(ctx, call.ID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("create analysis: %w", err)
	}

	analysis, err = s.analysisRepository.MarkProcessing(ctx, analysis.ID)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("mark analysis processing: %w", err)
	}

	analysisStartedAt := time.Now()
	result, err := s.analyzer.Analyze(ctx, models.AnalysisRequest{
		CallUUID:        call.ID,
		Transcription:   *transcription.Text,
		Instructions:    instructions,
		Personalization: personalization,
	})
	if err != nil {
		if opts.markAttemptFailed {
			failedAnalysis, markErr := s.analysisRepository.MarkFailed(ctx, analysis.ID, err.Error())
			if markErr != nil {
				return models.CallAnalysis{}, fmt.Errorf("mark analysis failed: %w", markErr)
			}
			analysis = failedAnalysis
		}
		s.log.Error(ctx, "call analysis failed", zap.String("call_id", call.ID.String()), zap.Error(err))
		return analysis, fmt.Errorf("analyze call: %w", err)
	}

	result, err = normalizeAnalysisResult(result)
	if err != nil {
		if opts.markAttemptFailed {
			failedAnalysis, markErr := s.analysisRepository.MarkFailed(ctx, analysis.ID, err.Error())
			if markErr != nil {
				return models.CallAnalysis{}, fmt.Errorf("mark analysis failed: %w", markErr)
			}
			analysis = failedAnalysis
		}
		s.log.Error(ctx, "call analysis result is invalid", zap.String("call_id", call.ID.String()), zap.Error(err))
		return analysis, fmt.Errorf("normalize analysis result: %w", err)
	}

	analysis, err = s.analysisRepository.MarkDone(ctx, analysis.ID, result)
	if err != nil {
		return models.CallAnalysis{}, fmt.Errorf("mark analysis done: %w", err)
	}

	if _, err = s.callRepository.UpdateCallStatus(ctx, call.ID, models.CallStatusAnalyzed); err != nil {
		return models.CallAnalysis{}, fmt.Errorf("mark call analyzed: %w", err)
	}

	s.log.Info(ctx, "call analyzed", zap.String("call_id", call.ID.String()), zap.String("provider", s.analyzer.Provider()), zap.Duration("analysis_duration", time.Since(analysisStartedAt)))

	return analysis, nil
}

func (s *Service) createPendingAnalysis(ctx context.Context, callID uuid.UUID) (models.CallAnalysis, error) {
	analysisID, err := uuid.NewV7()
	if err != nil {
		return models.CallAnalysis{}, err
	}

	now := time.Now().UTC()

	return s.analysisRepository.Create(ctx, models.CallAnalysis{
		ID:        analysisID,
		CallUUID:  callID,
		Status:    models.CallAnalysisStatusPending,
		Provider:  s.analyzerProviderName(),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) enqueueAnalyzeJob(ctx context.Context, callID uuid.UUID) error {
	if s.processingJobRepository == nil {
		return models.ErrProcessingJobNotFound
	}

	jobID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate analysis job uuid: %w", err)
	}

	now := time.Now().UTC()

	_, err = s.processingJobRepository.Enqueue(ctx, models.ProcessingJob{
		ID:          jobID,
		Type:        models.ProcessingJobTypeAnalyzeCall,
		EntityUUID:  callID,
		Status:      models.ProcessingJobStatusPending,
		Attempts:    0,
		MaxAttempts: s.processingJobMaxAttempts,
		AvailableAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) createFailedAnalysis(ctx context.Context, callID uuid.UUID, errorMessage string) (models.CallAnalysis, error) {
	analysisID, err := uuid.NewV7()
	if err != nil {
		return models.CallAnalysis{}, err
	}

	now := time.Now().UTC()

	return s.analysisRepository.Create(ctx, models.CallAnalysis{
		ID:           analysisID,
		CallUUID:     callID,
		Status:       models.CallAnalysisStatusFailed,
		Provider:     s.analyzerProviderName(),
		ErrorMessage: &errorMessage,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
}

func (s *Service) analyzerProviderName() string {
	if s.analyzer == nil {
		return "unknown"
	}

	return s.analyzer.Provider()
}

func (s *Service) loadInstructions(ctx context.Context, call models.Call, userID uuid.UUID) ([]models.AnalysisInstructionContent, error) {
	if call.SkipCustomInstructions {
		return []models.AnalysisInstructionContent{}, nil
	}

	instructions, err := s.selectInstructions(ctx, call, userID)
	if err != nil {
		return nil, err
	}

	contents := make([]models.AnalysisInstructionContent, 0, len(instructions))
	for _, instruction := range instructions {
		content, err := s.readInstructionContent(ctx, instruction)
		if err != nil {
			if errors.Is(err, models.ErrInstructionFileNotFound) {
				s.log.Warn(ctx, "analysis instruction file skipped", zap.String("instruction_id", instruction.ID.String()), zap.String("file_path", instruction.FilePath), zap.Error(err))
				continue
			}
			return nil, err
		}
		contents = append(contents, content)
	}

	return contents, nil
}

func (s *Service) selectInstructions(ctx context.Context, call models.Call, userID uuid.UUID) ([]models.AnalysisInstruction, error) {
	var selected []models.AnalysisInstruction
	var err error
	switch call.VisibilityScope {
	case models.CallVisibilityScopePersonal:
		selected, err = s.instructionRepository.List(ctx, models.ListAnalysisInstructionsInput{
			Scope:    models.AnalysisInstructionScopePersonal,
			UserUUID: userID,
		})
	case models.CallVisibilityScopeCompany:
		selected, err = s.instructionRepository.List(ctx, models.ListAnalysisInstructionsInput{
			Scope:       models.AnalysisInstructionScopeCompany,
			CompanyUUID: call.CompanyUUID,
		})
	case models.CallVisibilityScopeDepartment:
		companyInstructions, err := s.instructionRepository.List(ctx, models.ListAnalysisInstructionsInput{
			Scope:       models.AnalysisInstructionScopeCompany,
			CompanyUUID: call.CompanyUUID,
		})
		if err != nil {
			return nil, err
		}

		departmentInstructions, err := s.instructionRepository.List(ctx, models.ListAnalysisInstructionsInput{
			Scope:          models.AnalysisInstructionScopeDepartment,
			CompanyUUID:    call.CompanyUUID,
			DepartmentUUID: call.DepartmentUUID,
		})
		if err != nil {
			return nil, err
		}

		selected = append(companyInstructions, departmentInstructions...)
	default:
		return nil, models.ErrInvalidAnalysisInput
	}
	if err != nil {
		return nil, err
	}
	if s.folderInstructionReader == nil {
		return selected, nil
	}
	folderInstructions, err := s.folderInstructionReader.ListInstructionsForCall(ctx, call.ID)
	if err != nil {
		return nil, err
	}
	seen := make(map[uuid.UUID]struct{}, len(selected)+len(folderInstructions))
	result := make([]models.AnalysisInstruction, 0, len(selected)+len(folderInstructions))
	for _, instruction := range append(selected, folderInstructions...) {
		if _, exists := seen[instruction.ID]; exists {
			continue
		}
		seen[instruction.ID] = struct{}{}
		result = append(result, instruction)
	}
	return result, nil
}

func (s *Service) readInstructionContent(ctx context.Context, instruction models.AnalysisInstruction) (models.AnalysisInstructionContent, error) {
	content, err := s.instructionStorage.Open(ctx, instruction.FilePath)
	if err != nil {
		return models.AnalysisInstructionContent{}, err
	}
	defer func() { _ = content.Close() }()

	data, err := io.ReadAll(content)
	if err != nil {
		return models.AnalysisInstructionContent{}, err
	}

	return models.AnalysisInstructionContent{
		ID:      instruction.ID,
		Scope:   instruction.Scope,
		Title:   instruction.Title,
		Content: string(data),
	}, nil
}

func normalizeAnalysisResult(result models.AnalysisResult) (models.AnalysisResult, error) {
	payload := map[string]any{}

	if len(result.ResultJSON) > 0 {
		if err := json.Unmarshal(result.ResultJSON, &payload); err != nil {
			return models.AnalysisResult{}, fmt.Errorf("decode analysis result json: %w", err)
		}
	}

	resultText := ""
	if result.ResultText != nil {
		resultText = strings.TrimSpace(*result.ResultText)
	}
	// Some providers occasionally return a complete JSON document as the
	// summary field of an outer JSON object. Unwrap it here so clients never
	// receive raw JSON instead of a readable analysis.
	payload = unwrapNestedAnalysisPayload(payload, resultText)
	if looksLikeStructuredAnalysisText(stringField(payload, "summary")) {
		return models.AnalysisResult{}, errors.New("analysis summary contains invalid nested structured JSON")
	}

	summary := stringField(payload, "summary")
	if summary == "" {
		summary = resultText
	}
	if summary == "" {
		summary = "Анализ завершен, но провайдер не вернул резюме."
	}
	summary = normalizeRussianAnalysisText(summary)
	payload["summary"] = summary

	payload["schema_version"] = float64(2)
	ensureArrayField(payload, "topics")
	ensureObjectFields(payload, "dialogue_tone", map[string]any{
		"overall":         "",
		"manager":         "",
		"client":          "",
		"evidence_quotes": []any{},
	})
	ensureArrayField(payload, "client_questions")
	ensureObjectFields(payload, "question_coverage", map[string]any{
		"status":               "unclear",
		"summary":              "",
		"unanswered_questions": []any{},
	})
	ensureObjectFields(payload, "manager_quality", map[string]any{
		"strengths":       []any{},
		"issues":          []any{},
		"recommendations": []any{},
	})
	ensureStringField(payload, "call_outcome")
	ensureArrayField(payload, "customer_objections")
	ensureArrayField(payload, "risks")
	ensureArrayField(payload, "next_steps")
	ensureObjectFields(payload, "next_step_quality", map[string]any{
		"has_next_step":          false,
		"specific":               false,
		"has_deadline":           false,
		"has_responsible_person": false,
	})
	ensureObjectFields(payload, "business_outcome", map[string]any{
		"status":      "unclear",
		"summary":     "",
		"lost_reason": "not_applicable",
	})
	ensureObjectFields(payload, "customer_signals", map[string]any{
		"intent":                 "unclear",
		"urgency":                "unclear",
		"budget_discussed":       false,
		"decision_maker_present": false,
	})
	normalizeBusinessOutcome(payload)
	normalizeCustomerSignals(payload)
	normalizeIssueCodes(payload)
	ensureArrayField(payload, "evidence_quotes")
	ensureConfidenceField(payload)
	normalizeCriteriaAndScore(payload)

	if stringField(payload, "next_step") == "" {
		payload["next_step"] = firstStringFromArray(payload["next_steps"])
	}
	normalizeNextStepQuality(payload)
	normalizePayloadRussianText(payload)
	summary = stringField(payload, "summary")

	resultText = summary
	result.ResultText = &resultText

	resultJSON, err := json.Marshal(payload)
	if err != nil {
		return models.AnalysisResult{}, fmt.Errorf("encode normalized analysis result json: %w", err)
	}

	result.ResultJSON = resultJSON

	return result, nil
}

func normalizeCriteriaAndScore(payload map[string]any) {
	inputScore := normalizeScore(payload["score"], payload["score_scale"])
	payload["score_scale"] = float64(100)

	rawCriteria, _ := payload["criteria_results"].([]any)
	criteria := make([]any, 0, len(rawCriteria))
	pointsAwarded := 0.0
	pointsPossible := 0.0
	applicableCount := 0

	for _, raw := range rawCriteria {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		normalized := normalizeCriterionResult(item)
		criteria = append(criteria, normalized)

		status := stringField(normalized, "status")
		pointsMax := numberField(normalized, "points_max")
		if status == "not_applicable" || pointsMax <= 0 {
			continue
		}
		pointsAwarded += numberField(normalized, "points_awarded")
		pointsPossible += pointsMax
		applicableCount++
	}

	payload["criteria_results"] = criteria
	score := inputScore
	if pointsPossible > 0 {
		score = clampScore(math.Round(pointsAwarded / pointsPossible * 100))
	}
	payload["score"] = score
	payload["score_breakdown"] = map[string]any{
		"points_awarded":            pointsAwarded,
		"points_possible":           pointsPossible,
		"applicable_criteria_count": float64(applicableCount),
		"total_criteria_count":      float64(len(criteria)),
	}
}

func normalizeCriterionResult(item map[string]any) map[string]any {
	out := copyStringMap(item)
	code := stringField(out, "code")
	legacyInstructionCriterion := code == "" && (stringField(out, "instruction_title") != "" || stringField(out, "result") != "")
	if legacyInstructionCriterion {
		code = "custom_instruction_match"
	}
	if code == "" {
		code = "custom_instruction_match"
	}
	out["code"] = code

	criterion, known := analysisCriterionByCode(code)
	if stringField(out, "title") == "" {
		if known {
			out["title"] = criterion.Title
		} else if title := stringField(out, "instruction_title"); title != "" {
			out["title"] = title
		} else {
			out["title"] = code
		}
	}
	if stringField(out, "topic") == "" {
		out["topic"] = stringField(out, "title")
	}

	status := stringField(out, "status")
	if status == "" && stringField(out, "result") != "" {
		status = "unclear"
	}
	status = normalizeCriterionStatus(status, !known)
	out["status"] = status

	pointsMax := float64(0)
	if _, ok := out["points_max"]; ok {
		pointsMax = math.Max(0, numberField(out, "points_max"))
	} else {
		if legacyInstructionCriterion {
			pointsMax = 0
		} else if known {
			pointsMax = float64(criterion.PointsMax)
		} else {
			pointsMax = 0
		}
	}
	if status == "not_applicable" {
		pointsMax = 0
	}
	out["points_max"] = pointsMax

	pointsAwarded := float64(0)
	if _, ok := out["points_awarded"]; ok {
		pointsAwarded = math.Max(0, numberField(out, "points_awarded"))
	}
	if pointsMax > 0 && (pointsAwarded == 0 && statusImpliesPositiveScore(status)) {
		pointsAwarded = defaultCriterionPointsAwarded(status, pointsMax)
	}
	if status == "not_applicable" || pointsMax <= 0 {
		pointsAwarded = 0
	} else if pointsAwarded > pointsMax {
		pointsAwarded = pointsMax
	}
	out["points_awarded"] = pointsAwarded

	ensureArrayField(out, "evidence_quotes")
	normalizeCriterionExplanation(out, status)
	quotes, _ := out["evidence_quotes"].([]any)
	if stringField(out, "quote") == "" && len(quotes) > 0 {
		if quote, ok := quotes[0].(string); ok {
			out["quote"] = strings.TrimSpace(quote)
		}
	}
	if stringField(out, "quote") == "" {
		out["quote"] = "Не указано"
	}
	out["explanation"] = stringField(out, "issue")
	if pointsMax > 0 {
		out["score"] = math.Round(pointsAwarded / pointsMax * 100)
	} else {
		out["score"] = float64(0)
	}
	return out
}

func unwrapNestedAnalysisPayload(payload map[string]any, resultText string) map[string]any {
	candidates := []string{stringField(payload, "summary"), resultText}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if !strings.HasPrefix(candidate, "{") {
			continue
		}
		var nested map[string]any
		if err := json.Unmarshal([]byte(candidate), &nested); err != nil {
			continue
		}
		if _, hasCriteria := nested["criteria_results"]; hasCriteria {
			return nested
		}
		if _, hasSchema := nested["schema_version"]; hasSchema {
			return nested
		}
	}
	return payload
}

func looksLikeStructuredAnalysisText(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") {
		return false
	}
	return strings.Contains(value, `"schema_version"`) ||
		strings.Contains(value, `"criteria_results"`) ||
		strings.Contains(value, `"summary"`)
}

func normalizeCriterionStatus(status string, preserveUnknown bool) string {
	status = strings.TrimSpace(status)
	switch status {
	case "met", "partially_met", "missed", "not_applicable", "unclear":
		return status
	default:
		if status == "" || !preserveUnknown {
			return "unclear"
		}
		return status
	}
}

func statusImpliesPositiveScore(status string) bool {
	return status == "met" || status == "partially_met"
}

func defaultCriterionPointsAwarded(status string, pointsMax float64) float64 {
	switch status {
	case "met":
		return pointsMax
	case "partially_met":
		return math.Round(pointsMax / 2)
	default:
		return 0
	}
}

func normalizeCriterionExplanation(out map[string]any, status string) {
	issue := normalizeCriterionText(stringField(out, "issue"))
	recommendation := normalizeCriterionText(stringField(out, "recommendation"))

	if issue == "" {
		switch status {
		case "met":
			issue = "Проблема не выявлена."
		case "partially_met":
			issue = "Критерий выполнен частично."
		case "missed":
			issue = "Критерий не подтвержден в расшифровке."
		case "not_applicable":
			issue = "Критерий не применим к этому звонку."
		default:
			issue = "Данных в расшифровке недостаточно для уверенной оценки."
		}
	}
	if recommendation == "" {
		switch status {
		case "met", "not_applicable":
			recommendation = "Рекомендация не требуется."
		case "partially_met":
			recommendation = "Усилить выполнение этого критерия."
		case "missed":
			recommendation = "Добавить явное выполнение этого критерия в разговор."
		default:
			recommendation = "Проверить качество расшифровки или уточнить критерий."
		}
	}

	out["issue"] = issue
	out["recommendation"] = recommendation
}

func normalizeCriterionText(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "not_applicable", "not applicable", "not specified", "not provided", "none", "n/a":
		return ""
	default:
		return normalizeRussianAnalysisText(value)
	}
}

func normalizeScore(scoreValue, scaleValue any) float64 {
	score, ok := numericValue(scoreValue)
	if !ok {
		return 0
	}
	if scale, ok := numericValue(scaleValue); ok && scale > 0 {
		return clampScore(math.Round(score / scale * 100))
	}
	switch {
	case score <= 5:
		return clampScore(math.Round(score * 20))
	case score <= 10:
		return clampScore(math.Round(score * 10))
	default:
		return clampScore(math.Round(score))
	}
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func normalizeBusinessOutcome(payload map[string]any) {
	value, _ := payload["business_outcome"].(map[string]any)
	if !allowedString(value, "status", []string{"success", "follow_up_needed", "no_decision", "lost", "support_resolved", "unclear"}) {
		value["status"] = "unclear"
	}
	if !allowedString(value, "lost_reason", []string{"price", "timing", "no_need", "competitor", "no_next_step", "unclear_value", "bad_fit", "not_applicable", "unclear"}) {
		value["lost_reason"] = "not_applicable"
	}
	if stringField(value, "summary") == "" {
		value["summary"] = "Не указано"
	}
}

func normalizeCustomerSignals(payload map[string]any) {
	value, _ := payload["customer_signals"].(map[string]any)
	for _, key := range []string{"intent", "urgency"} {
		if !allowedString(value, key, []string{"high", "medium", "low", "unclear"}) {
			value[key] = "unclear"
		}
	}
	ensureBoolField(value, "budget_discussed")
	ensureBoolField(value, "decision_maker_present")
}

func normalizeNextStepQuality(payload map[string]any) {
	value, _ := payload["next_step_quality"].(map[string]any)
	for _, key := range []string{"has_next_step", "specific", "has_deadline", "has_responsible_person"} {
		ensureBoolField(value, key)
	}
	if hasMeaningfulNextStep(payload) {
		value["has_next_step"] = true
		if !boolValue(value["specific"]) && hasSpecificNextStep(payload) {
			value["specific"] = true
		}
	}
}

func normalizeIssueCodes(payload map[string]any) {
	values, ok := payload["issue_codes"].([]any)
	if !ok {
		payload["issue_codes"] = []any{}
		return
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		switch normalizeAnalysisIssueCode(text) {
		case "not_call", "not_a_call":
			continue
		}
		if text != "" {
			out = append(out, text)
		}
	}
	payload["issue_codes"] = out
}

func normalizeAnalysisIssueCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "_")
	code = strings.Join(strings.Fields(code), "_")
	return code
}

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func hasMeaningfulNextStep(payload map[string]any) bool {
	if isMeaningfulAnalysisText(stringField(payload, "next_step")) {
		return true
	}
	values, ok := payload["next_steps"].([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		text, ok := value.(string)
		if ok && isMeaningfulAnalysisText(text) {
			return true
		}
	}
	return false
}

func hasSpecificNextStep(payload map[string]any) bool {
	if isSpecificAnalysisText(stringField(payload, "next_step")) {
		return true
	}
	values, ok := payload["next_steps"].([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		text, ok := value.(string)
		if ok && isSpecificAnalysisText(text) {
			return true
		}
	}
	return false
}

func isMeaningfulAnalysisText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "unclear", "not specified", "not provided", "none", "n/a", "не указано", "неясно":
		return false
	default:
		return true
	}
}

func isSpecificAnalysisText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !isMeaningfulAnalysisText(normalized) {
		return false
	}
	switch normalized {
	case "следующий шаг не указан", "не указан", "нет", "не требуется":
		return false
	default:
		return len([]rune(normalized)) >= 12
	}
}

func normalizePayloadRussianText(value any) {
	normalizePayloadRussianTextValue("", value)
}

func normalizePayloadRussianTextValue(key string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			switch childTyped := childValue.(type) {
			case string:
				if isSchemaEnumKey(childKey) {
					continue
				}
				typed[childKey] = normalizeRussianAnalysisText(childTyped)
			default:
				normalizePayloadRussianTextValue(childKey, childValue)
			}
		}
	case []any:
		for i, item := range typed {
			switch childTyped := item.(type) {
			case string:
				if isSchemaEnumKey(key) {
					continue
				}
				typed[i] = normalizeRussianAnalysisText(childTyped)
			default:
				normalizePayloadRussianTextValue(key, item)
			}
		}
	}
}

func isSchemaEnumKey(key string) bool {
	switch key {
	case "answer_status", "confidence", "status", "code", "lost_reason", "intent", "urgency":
		return true
	default:
		return false
	}
}

func normalizeRussianAnalysisText(value string) string {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "":
		return ""
	case "unclear":
		return "Неясно"
	case "not specified", "not provided", "none", "n/a":
		return "Не указано"
	case "no client questions were identified in the transcription.":
		return "В расшифровке не выявлены вопросы клиента."
	case "the transcription provided does not contain a sales or client call. it is a text about the history and new directions of advertising, including the use of human billboards. therefore, no analysis of a sales or client call can be provided.":
		return "Не удалось надежно определить итог разговора по расшифровке."
	default:
		return trimmed
	}
}

func stringField(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

func firstStringFromArray(value any) string {
	values, ok := value.([]any)
	if !ok {
		return ""
	}

	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}

	return ""
}

func ensureArrayField(payload map[string]any, key string) {
	if _, ok := payload[key].([]any); ok {
		return
	}

	payload[key] = []any{}
}

func ensureObjectFields(payload map[string]any, key string, defaults map[string]any) {
	value, ok := payload[key].(map[string]any)
	if !ok {
		payload[key] = defaults
		return
	}

	for defaultKey, defaultValue := range defaults {
		if _, exists := value[defaultKey]; !exists {
			value[defaultKey] = defaultValue
		}
	}
}

func ensureStringField(payload map[string]any, key string) {
	if _, ok := payload[key].(string); ok {
		return
	}

	payload[key] = ""
}

func ensureNumberField(payload map[string]any, key string) {
	switch payload[key].(type) {
	case float64, int, int64:
		return
	default:
		payload[key] = 0
	}
}

func ensureBoolField(payload map[string]any, key string) {
	if _, ok := payload[key].(bool); ok {
		return
	}
	payload[key] = false
}

func ensureConfidenceField(payload map[string]any) {
	switch stringField(payload, "confidence") {
	case "low", "medium", "high":
		return
	default:
		payload["confidence"] = "low"
	}
}

func allowedString(payload map[string]any, key string, allowed []string) bool {
	value := stringField(payload, key)
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func numberField(payload map[string]any, key string) float64 {
	value, _ := numericValue(payload[key])
	return value
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func copyStringMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
