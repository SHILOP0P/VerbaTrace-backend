package analysis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/analyzer/analysisflow"
	"verbatrace/monolit/internal/analyzer/mockstaged"
	"verbatrace/monolit/internal/models"
)

func growthSegments(markers string) []analysisflow.Segment {
	return []analysisflow.Segment{
		{ID: "s1", Speaker: "A", Text: "Расскажите, что у вас сейчас с поставками?"},
		{ID: "s2", Speaker: "B", Text: "Всё сложно. " + markers},
		{ID: "s3", Speaker: "A", Text: "Понятно, давайте обсудим сроки?"},
		{ID: "s4", Speaker: "B", Text: "Давайте."},
	}
}

type recordedRun struct {
	summary models.AnalysisTask
	outcome *models.GrowthOutcome
	result  map[string]any
}

func runWithGrowth(t *testing.T, request models.AnalysisRequest, segments []analysisflow.Segment) recordedRun {
	t.Helper()
	provider := mockstaged.New("")
	var run recordedRun
	runner := analysisflow.Runner{Request: request, Segments: segments, Schema: provider.AnalysisSchema(),
		Execute: func(ctx context.Context, _ string, task models.AnalysisTask) (models.AnalysisResult, error) {
			if task.Name == analysisflow.StepSummary {
				run.summary = task
			}
			return provider.Analyze(ctx, models.AnalysisRequest{CallUUID: request.CallUUID, Task: &task})
		},
		OnGrowth: func(_ context.Context, outcome models.GrowthOutcome) { run.outcome = &outcome },
	}
	raw, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw.ResultJSON, &run.result))
	return run
}

// Without a growth context the summary step is exactly what it was before
// growth areas: same prompt, same input keys, same schema, and the run key of
// the request does not change either.
func TestSummaryStepWithoutGrowthIsUnchanged(t *testing.T) {
	request := models.AnalysisRequest{CallUUID: uuid.New()}
	plain := runWithGrowth(t, request, growthSegments(""))
	require.Nil(t, plain.outcome)
	require.NotContains(t, plain.summary.System, "growth_context")
	require.NotContains(t, plain.summary.Input, "growth_context")
	schema, _ := json.Marshal(plain.summary.Schema)
	require.NotContains(t, string(schema), "growth")
	require.NotContains(t, plain.result, "growth_observations")
	key, err := json.Marshal(request)
	require.NoError(t, err)
	require.NotContains(t, string(key), "Growth", "a request without growth hashes as before")

	growth := request
	growth.Growth = &models.GrowthContext{SubjectSpeaker: "A", OpenAreas: []models.GrowthAreaRef{}}
	with := runWithGrowth(t, growth, growthSegments(""))
	require.True(t, strings.HasPrefix(with.summary.System, plain.summary.System), "the growth text is appended, nothing else changes")
	require.Contains(t, with.summary.Input, "growth_context")
	schema, _ = json.Marshal(with.summary.Schema)
	require.Contains(t, string(schema), "new_growth_areas")
	require.NotContains(t, string(schema), "growth_observations", "no open areas, nothing to observe")
}

func TestSummaryStepJudgesOpenAreasAndOpensNewOnes(t *testing.T) {
	areaID := uuid.New().String()
	request := models.AnalysisRequest{CallUUID: uuid.New(), Growth: &models.GrowthContext{SubjectSpeaker: "A", OpenAreas: []models.GrowthAreaRef{
		{ID: areaID, Title: "Отвечает общими словами", Description: "Без примеров."},
		{ID: uuid.New().String(), Title: "Перебивает клиента", Description: "Не даёт договорить."},
	}}}
	run := runWithGrowth(t, request, growthSegments("[[growth:repeated: Отвечает общими словами]] [[growth:new: Не называет следующий шаг]]"))
	require.NotNil(t, run.outcome)
	require.Len(t, run.outcome.Observations, 2)
	verdicts := map[string]models.GrowthObservation{}
	for _, o := range run.outcome.Observations {
		verdicts[o.AreaID] = o
	}
	require.Equal(t, models.GrowthVerdictRepeated, verdicts[areaID].Verdict)
	require.NotEmpty(t, verdicts[areaID].ItemIDs)
	require.Len(t, run.outcome.NewAreas, 1)
	require.Equal(t, "Не называет следующий шаг", run.outcome.NewAreas[0].Title)
	require.NotContains(t, run.result, "growth_observations", "growth fields never reach the result JSON")
	require.NotContains(t, run.result, "new_growth_areas")
}

// A model that keeps getting the growth fields wrong costs two retries, then the
// summary is taken without them: the analysis itself never fails over growth.
func TestBrokenGrowthFieldsNeverFailTheAnalysis(t *testing.T) {
	provider := mockstaged.New("")
	request := models.AnalysisRequest{CallUUID: uuid.New(), Growth: &models.GrowthContext{SubjectSpeaker: "A", OpenAreas: []models.GrowthAreaRef{
		{ID: uuid.New().String(), Title: "Отвечает общими словами", Description: "Без примеров."},
	}}}
	summaries, warned, grew := 0, false, false
	runner := analysisflow.Runner{Request: request, Segments: growthSegments(""), Schema: provider.AnalysisSchema(),
		Execute: func(ctx context.Context, _ string, task models.AnalysisTask) (models.AnalysisResult, error) {
			result, err := provider.Analyze(ctx, models.AnalysisRequest{CallUUID: request.CallUUID, Task: &task})
			if err != nil || task.Name != analysisflow.StepSummary {
				return result, err
			}
			summaries++
			var answer map[string]any
			require.NoError(t, json.Unmarshal(result.ResultJSON, &answer))
			answer["growth_observations"] = []any{map[string]any{"area_id": "unknown", "verdict": "repeated", "item_ids": []any{}, "note": ""}}
			result.ResultJSON, _ = json.Marshal(answer)
			return result, nil
		},
		OnGrowth: func(context.Context, models.GrowthOutcome) { grew = true },
		Warn:     func(context.Context, string) { warned = true },
	}
	raw, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, summaries)
	require.True(t, warned)
	require.False(t, grew, "invalid growth fields are dropped")
	require.NotContains(t, string(raw.ResultJSON), "growth_observations")
}
