package analysis

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/analyzer/analysisflow"
	"verbatrace/monolit/internal/analyzer/mockstaged"
	"verbatrace/monolit/internal/models"
)

// The deterministic analyzer must carry a call through every stage of the real
// pipeline and produce a result the service accepts as a finished analysis.
func TestMockStagedRunsTheWholePipeline(t *testing.T) {
	provider := mockstaged.New("")
	callID := uuid.New()
	instructionID := uuid.New()
	segments := []analysisflow.Segment{
		{ID: "s1", Speaker: "A", Text: "Здравствуйте, это компания Ромашка."},
		{ID: "s2", Speaker: "A", Text: "Какой у вас бюджет на проект?"},
		{ID: "s3", Speaker: "B", Text: "Около миллиона. [[missed: Выяснил сроки]]"},
		{ID: "s4", Speaker: "A", Text: "Когда нужно запуститься?"},
		{ID: "s5", Speaker: "B", Text: "Через месяц."},
	}
	calls := 0
	runner := analysisflow.Runner{
		Request: models.AnalysisRequest{CallUUID: callID, Instructions: []models.AnalysisInstructionContent{{
			ID: instructionID, Scope: models.AnalysisInstructionScopePersonal, Title: "Стандарт",
			Content: "Требования:\n- Выяснил бюджет\n- Выяснил сроки\n",
		}}},
		Segments: segments,
		Schema:   provider.AnalysisSchema(),
		Execute: func(ctx context.Context, _ string, task models.AnalysisTask) (models.AnalysisResult, error) {
			calls++
			return provider.Analyze(ctx, models.AnalysisRequest{CallUUID: callID, Task: &task})
		},
	}

	raw, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.Positive(t, calls)
	normalized, err := normalizeAnalysisResult(raw)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(normalized.ResultJSON, &result))
	require.EqualValues(t, 3, result["schema_version"])
	require.Equal(t, "complete", result["coverage"].(map[string]any)["status"])

	requirements := map[string]map[string]any{}
	questions := 0
	for _, raw := range result["items"].([]any) {
		item := raw.(map[string]any)
		switch item["kind"] {
		case "requirement":
			requirements[item["title"].(string)] = item
			require.Equal(t, []any{instructionID.String()}, item["instruction_sources"])
		case "question":
			questions++
		}
	}
	require.Equal(t, 2, questions)
	require.Contains(t, requirements, "Выяснил бюджет")
	require.Equal(t, "missed", requirements["Выяснил сроки"]["status"], "a marker forces the status")

	again, err := (&analysisflow.Runner{
		Request:  runner.Request,
		Segments: segments,
		Schema:   provider.AnalysisSchema(),
		Execute: func(ctx context.Context, _ string, task models.AnalysisTask) (models.AnalysisResult, error) {
			return provider.Analyze(ctx, models.AnalysisRequest{CallUUID: callID, Task: &task})
		},
	}).Run(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, string(raw.ResultJSON), string(again.ResultJSON), "the same call must get the same answers")
}

// With scorecards the model no longer breaks instructions into requirements:
// that step, paid on every call, disappears, and each requirement card carries
// the scorecard's identity and weight instead of whatever the model returns.
func TestScorecardRequirementsReplaceTheDecompositionStep(t *testing.T) {
	provider := mockstaged.New("")
	callID, instructionID, scorecardID := uuid.New(), uuid.New(), uuid.New()
	budgetKey, companyKey := uuid.New(), uuid.New()
	request := models.AnalysisRequest{
		CallUUID:     callID,
		Instructions: []models.AnalysisInstructionContent{{ID: instructionID, Scope: models.AnalysisInstructionScopeDepartment, Title: "Стандарт", Content: "- Выяснил бюджет"}},
		Scorecards: &models.AnalysisScorecards{
			Requirements: []models.AnalysisRequirement{{
				CriterionKey: budgetKey, AlsoCriterionKeys: []uuid.UUID{companyKey}, ScorecardID: scorecardID,
				InstructionID: instructionID, InstructionTitle: "Стандарт", Title: "Выяснил бюджет",
				Requirement: "Выяснить бюджет клиента", Weight: 3, IsCritical: true,
			}},
			AdhocInstructions: []uuid.UUID{},
			Applied:           []models.AppliedScorecard{{ScorecardID: scorecardID, InstructionID: instructionID, Revision: 2}},
			Mode:              models.ScorecardModeFixed,
		},
	}
	segments := []analysisflow.Segment{{ID: "s1", Speaker: "A", Text: "Какой бюджет?"}, {ID: "s2", Speaker: "B", Text: "Миллион."}}
	steps := map[string]int{}
	run := func() map[string]any {
		runner := analysisflow.Runner{Request: request, Segments: segments, Schema: provider.AnalysisSchema(),
			Execute: func(ctx context.Context, _ string, task models.AnalysisTask) (models.AnalysisResult, error) {
				steps[task.Name]++
				return provider.Analyze(ctx, models.AnalysisRequest{CallUUID: callID, Task: &task})
			}}
		raw, err := runner.Run(context.Background())
		require.NoError(t, err)
		normalized, err := normalizeAnalysisResult(raw)
		require.NoError(t, err)
		var result map[string]any
		require.NoError(t, json.Unmarshal(normalized.ResultJSON, &result))
		return result
	}

	first, second := run(), run()
	require.Zero(t, steps[analysisflow.StepRequirements], "the scorecard replaces the decomposition step")
	require.Equal(t, analysisflow.PromptVersion, first["prompt_version"])
	require.Equal(t, models.ScorecardModeFixed, first["scorecard_mode"])
	for _, result := range []map[string]any{first, second} {
		var requirement map[string]any
		for _, raw := range result["items"].([]any) {
			if item := raw.(map[string]any); item["kind"] == "requirement" {
				requirement = item
			}
		}
		require.NotNil(t, requirement)
		require.Equal(t, budgetKey.String(), requirement["criterion_key"])
		require.Equal(t, []any{companyKey.String()}, requirement["also_criterion_keys"])
		require.Equal(t, scorecardID.String(), requirement["scorecard_uuid"])
		require.EqualValues(t, 3, requirement["weight"])
		require.Equal(t, true, requirement["is_critical"])
		require.Equal(t, []any{instructionID.String()}, requirement["instruction_sources"])
		require.NotEmpty(t, requirement["evidence"], "a requirement may quote any part of the conversation")
	}
}

func TestMockStagedRefusesWholeCallRequests(t *testing.T) {
	_, err := mockstaged.New("").Analyze(context.Background(), models.AnalysisRequest{CallUUID: uuid.New(), Transcription: "text"})
	require.ErrorIs(t, err, mockstaged.ErrNotStaged)
}
