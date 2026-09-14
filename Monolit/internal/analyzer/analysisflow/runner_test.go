package analysisflow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/analyzer/analysisflow"
	"verbatrace/monolit/internal/analyzer/openrouter"
	"verbatrace/monolit/internal/models"
)

func TestProgressiveAnalysisPreservesEveryQuestionAndPublishesAuditedAnswers(t *testing.T) {
	provider, _ := openrouter.New("test-key", "test-model")
	segments := []analysisflow.Segment{}
	for i := 0; i < 8; i++ {
		segments = append(segments, analysisflow.Segment{ID: fmt.Sprintf("s%d", i), Speaker: "A", Text: fmt.Sprintf("Вопрос %d?", i)})
	}
	var snapshots []map[string]any
	runner := analysisflow.Runner{Segments: segments, Schema: provider.AnalysisSchema(), Publish: func(_ context.Context, raw json.RawMessage) error {
		var v map[string]any
		require.NoError(t, json.Unmarshal(raw, &v))
		snapshots = append(snapshots, v)
		return nil
	}}
	runner.Execute = fixtureExecutor(t, false, false)
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	require.Len(t, final["items"], 8)
	require.Equal(t, "complete", final["coverage"].(map[string]any)["status"])
	require.Equal(t, float64(8), final["coverage"].(map[string]any)["analyzed_actual_question_count"])
	sawInventory, sawPartial := false, false
	for _, s := range snapshots {
		require.Nil(t, s["overall_score"])
		p := s["progress"].(map[string]any)
		if p["stage"] == "inventory" && len(s["items"].([]any)) == 8 {
			sawInventory = true
			require.Equal(t, "pending", s["items"].([]any)[0].(map[string]any)["processing_status"])
		}
		if p["items_done"] == float64(3) {
			sawPartial = true
			require.Len(t, s["items"], 8)
			ready := 0
			for _, raw := range s["items"].([]any) {
				if raw.(map[string]any)["processing_status"] == "ready" {
					ready++
				}
			}
			require.Equal(t, 3, ready)
		}
	}
	require.True(t, sawInventory)
	require.True(t, sawPartial)
	evidence := final["items"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	require.Equal(t, "A", evidence["speaker"])
	require.Equal(t, "Вопрос 0?", evidence["quote"])
}

func TestNormalizedProviderQuoteFallsBackToExactSourceEvidence(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	runner := analysisflow.Runner{Segments: []analysisflow.Segment{{ID: "s0", Text: "Вопрос 0?", Speaker: "A"}}, Schema: provider.AnalysisSchema(), Execute: fixtureExecutor(t, true, false)}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	evidence := final["items"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	require.Equal(t, "Вопрос 0?", evidence["quote"])
}

func TestUncoveredSourceCannotBecomeCompletedAnalysis(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	runner := analysisflow.Runner{Segments: []analysisflow.Segment{{ID: "s0", Text: "Расскажите о проекте", Speaker: "A"}}, Schema: provider.AnalysisSchema(), Execute: fixtureExecutor(t, false, true)}
	_, err := runner.Run(context.Background())
	require.ErrorContains(t, err, "incomplete_coverage")
}

func TestInventoryRecoversOnlySegmentsMissedByBothFullPasses(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	segment := analysisflow.Segment{ID: "s0", Text: "Расскажите о проекте", Speaker: "A"}
	var keys []string
	runner := analysisflow.Runner{Segments: []analysisflow.Segment{segment}, Schema: provider.AnalysisSchema()}
	runner.Execute = func(_ context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		keys = append(keys, key)
		var output any
		switch {
		case strings.Contains(key, "/recover"):
			output = map[string]any{"units": []analysisflow.Unit{{ID: "local", Kind: "question", Title: segment.Text, Topic: "опыт", SegmentIDs: []string{segment.ID}, Parts: []string{segment.Text}}}, "excluded": []any{}}
		case strings.HasPrefix(key, "inventory/"):
			output = map[string]any{"units": []any{}, "excluded": []any{}}
		case strings.HasPrefix(key, "assess/") && strings.Contains(key, "/audit"):
			output = map[string]any{"issues": []any{}}
		case strings.HasPrefix(key, "assess/"):
			var input map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(task.Input), &input))
			var units []analysisflow.Unit
			require.NoError(t, json.Unmarshal(input["assigned_units"], &units))
			output = map[string]any{"items": []any{
				map[string]any{
					"id": units[0].ID, "kind": "question", "title": units[0].Title,
					"explanation": "Разбор", "answer_summary": "Ответ", "information_status": "complete",
					"fulfilled_earlier": false, "status": "met", "weight": 1, "score": 100,
					"strengths": []any{}, "gaps": []any{}, "instruction_sources": []any{},
					"improvement_kind": "not_needed",
					"evidence":         []any{map[string]any{"segment_id": segment.ID, "quote": segment.Text}},
				},
			}}
		case strings.HasPrefix(key, "summary"):
			output = map[string]any{"summary": "Обсудили опыт.", "recommendations": []any{}, "priority_recommendation_ids": []any{}, "strengths": []any{}, "work_on": []any{}}
		default:
			t.Fatalf("unexpected stage %s", key)
		}
		raw, err := json.Marshal(output)
		return models.AnalysisResult{ResultJSON: raw}, err
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, result.ResultJSON)
	require.Equal(t, []string{"inventory/0/extract/try0", "inventory/0/audit/try0", "inventory/0/recover/try0", "assess/u1/repair0/try0", "assess/u1/audit0/try0", "summary/try0"}, keys)
}

func TestSourceSegmentsPreserveLongUnicodeTurnAndTail(t *testing.T) {
	source := strings.Repeat("Длинный ответ с кириллицей 🙂. ", 500) + "Последний вопрос?"
	segments := analysisflow.SourceSegments(models.Transcription{Text: &source})
	require.Greater(t, len(segments), 1)
	var rebuilt strings.Builder
	ids := map[string]bool{}
	for _, s := range segments {
		require.False(t, ids[s.ID])
		ids[s.ID] = true
		require.LessOrEqual(t, len([]rune(s.Text)), 2400)
		rebuilt.WriteString(s.Text)
	}
	require.Equal(t, source, rebuilt.String())
}

func fixtureExecutor(t *testing.T, badQuote, missingSource bool) func(context.Context, string, models.AnalysisTask) (models.AnalysisResult, error) {
	return func(_ context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		var input map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(task.Input), &input))
		var output any
		switch {
		case strings.HasPrefix(key, "inventory/"):
			var ids []string
			require.NoError(t, json.Unmarshal(input["owned_segment_ids"], &ids))
			var segments []analysisflow.Segment
			require.NoError(t, json.Unmarshal(input["context_segments"], &segments))
			units := []analysisflow.Unit{}
			if !missingSource {
				for _, id := range ids {
					for _, s := range segments {
						if s.ID == id {
							units = append(units, analysisflow.Unit{ID: id, Kind: "question", Title: s.Text, Topic: "Тема", SegmentIDs: []string{id}, Parts: []string{s.Text}})
						}
					}
				}
			}
			output = map[string]any{"units": units, "excluded": []any{}}
		case strings.Contains(key, "/audit"):
			output = map[string]any{"issues": []any{}}
		case strings.HasPrefix(key, "assess/"):
			var units []analysisflow.Unit
			require.NoError(t, json.Unmarshal(input["assigned_units"], &units))
			items := []map[string]any{}
			for _, u := range units {
				quote := u.Title
				if badQuote {
					quote = "Несуществующая цитата"
				}
				items = append(items, map[string]any{"id": u.ID, "kind": u.Kind, "title": u.Title, "explanation": "Разбор конкретного ответа", "answer_summary": "Ответ", "information_status": "complete", "fulfilled_earlier": false, "status": "met", "weight": 1, "score": 100, "strengths": []any{}, "gaps": []any{}, "instruction_sources": []any{}, "improvement_kind": "not_needed", "evidence": []any{map[string]any{"segment_id": u.SegmentIDs[0], "quote": quote}}})
			}
			output = map[string]any{"items": items}
		case strings.HasPrefix(key, "summary"):
			output = map[string]any{"summary": "Обсудили вопросы. Следует улучшить обоснование ответов.", "recommendations": []any{}, "priority_recommendation_ids": []any{}, "strengths": []any{}, "work_on": []any{}}
		default:
			t.Fatalf("unexpected stage %s", key)
		}
		raw, err := json.Marshal(output)
		return models.AnalysisResult{ResultJSON: raw}, err
	}
}
