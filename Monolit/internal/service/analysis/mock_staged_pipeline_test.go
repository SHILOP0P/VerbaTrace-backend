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

func TestMockStagedRefusesWholeCallRequests(t *testing.T) {
	_, err := mockstaged.New("").Analyze(context.Background(), models.AnalysisRequest{CallUUID: uuid.New(), Transcription: "text"})
	require.ErrorIs(t, err, mockstaged.ErrNotStaged)
}
