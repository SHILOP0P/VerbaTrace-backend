package analysisflow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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
		// Batches finish in any order, so check the invariant on every partial snapshot.
		if done := p["items_done"].(float64); done > 0 && done < 8 {
			sawPartial = true
			require.Len(t, s["items"], 8)
			ready := 0
			for _, raw := range s["items"].([]any) {
				if raw.(map[string]any)["processing_status"] == "ready" {
					ready++
				}
			}
			require.Equal(t, int(done), ready)
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
	require.Equal(t, []string{"inventory/0/extract/try0", "inventory/0/audit/try0", "inventory/0/recover/try0", "assess/u1.1/repair0/try0", "assess/u1.1/audit0/try0", "summary/try0"}, keys)
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

func TestAssessmentStepsShareCacheablePrefixAndSummaryOmitsEvidence(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	segments := []analysisflow.Segment{}
	for i := 0; i < 7; i++ {
		start := float64(i)
		segments = append(segments, analysisflow.Segment{ID: fmt.Sprintf("s%d", i), Speaker: "A", Start: &start, Text: fmt.Sprintf("Вопрос %d?", i)})
	}
	var mu sync.Mutex
	tasks := map[string]models.AnalysisTask{}
	fixture := fixtureExecutor(t, false, false)
	runner := analysisflow.Runner{Segments: segments, Schema: provider.AnalysisSchema()}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		mu.Lock()
		tasks[key] = task
		mu.Unlock()
		return fixture(ctx, key, task)
	}
	_, err := runner.Run(context.Background())
	require.NoError(t, err)

	var shared string
	assessments := 0
	for key, task := range tasks {
		if !strings.HasPrefix(key, "assess/") {
			continue
		}
		assessments++
		if shared == "" {
			shared = task.Context
		}
		// Byte-identical context is what lets the provider serve it from cache.
		require.Equal(t, shared, task.Context, key)
		require.NotContains(t, task.Input, "source_segments", key)
	}
	require.Equal(t, 6, assessments, "three batches, each assessed and audited once")
	for _, s := range segments {
		require.Contains(t, shared, fmt.Sprintf(`["%s","A","%s"]`, s.ID, s.Text))
	}
	require.NotContains(t, shared, "start_seconds")

	properties := tasks["assess/u1.1/repair0/try0"].Schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	for _, backendOwned := range []string{"title", "topic", "order", "score"} {
		require.NotContains(t, properties, backendOwned)
	}
	summary := tasks["summary/try0"].Input
	require.Contains(t, summary, "Разбор конкретного ответа")
	require.NotContains(t, summary, "evidence")
}

func TestAuditRepairsOnlyCardsMarkedMustFix(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	segments := []analysisflow.Segment{{ID: "s0", Speaker: "A", Text: "Вопрос 0?"}, {ID: "s1", Speaker: "A", Text: "Вопрос 1?"}, {ID: "s2", Speaker: "A", Text: "Вопрос 2?"}}
	fixture := fixtureExecutor(t, false, false)
	var repairs [][]analysisflow.Unit
	runner := analysisflow.Runner{Segments: segments, Schema: provider.AnalysisSchema()}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		switch {
		case key == "assess/u1.1/audit0/try0":
			raw, _ := json.Marshal(map[string]any{"issues": []any{
				map[string]any{"id": "u1.1", "category": "unfair_penalty", "reason": "Подтверждаю корректность.", "must_fix": false},
				map[string]any{"id": "u1.2", "category": "attribution", "reason": "Ответ дал другой участник.", "must_fix": true},
				// Unactionable issues are dropped instead of failing the audit.
				map[string]any{"id": "u9.9", "category": "missed_answer", "reason": "Карточка вне пакета.", "must_fix": true},
				map[string]any{"id": "u1.3", "category": "missed_answer", "reason": " ", "must_fix": true},
			}})
			return models.AnalysisResult{ResultJSON: raw}, nil
		case strings.HasPrefix(key, "assess/u1.1/repair") && !strings.HasPrefix(key, "assess/u1.1/repair0"):
			var input struct {
				Units  []analysisflow.Unit `json:"assigned_units"`
				Issues []map[string]any    `json:"audit_issues"`
			}
			require.NoError(t, json.Unmarshal([]byte(task.Input), &input))
			require.Len(t, input.Issues, 1)
			repairs = append(repairs, input.Units)
		}
		return fixture(ctx, key, task)
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.Len(t, repairs, 1)
	require.Len(t, repairs[0], 1)
	require.Equal(t, "u1.2", repairs[0][0].ID)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	require.Len(t, final["items"], 3)
	for _, raw := range final["items"].([]any) {
		require.NotContains(t, raw.(map[string]any), "validation_warning")
	}
}

func TestPersistentAuditDisagreementKeepsCardAfterBoundedRepairs(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	fixture := fixtureExecutor(t, false, false)
	assessments := 0
	runner := analysisflow.Runner{Segments: []analysisflow.Segment{{ID: "s0", Speaker: "A", Text: "Вопрос 0?"}}, Schema: provider.AnalysisSchema()}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		if strings.HasPrefix(key, "assess/") && strings.Contains(key, "/audit") {
			raw, _ := json.Marshal(map[string]any{"issues": []any{map[string]any{"id": "u1.1", "category": "missed_answer", "reason": "Спорное замечание.", "must_fix": true}}})
			return models.AnalysisResult{ResultJSON: raw}, nil
		}
		if strings.HasPrefix(key, "assess/") {
			assessments++
		}
		return fixture(ctx, key, task)
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, assessments, "initial assessment plus two targeted repairs")
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	require.NotEmpty(t, final["items"].([]any)[0].(map[string]any)["validation_warning"])
}

func TestInventoryWindowsRunConcurrentlyAndKeepTranscriptOrder(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	segments := []analysisflow.Segment{}
	for i := 0; i < 6; i++ {
		// Two such turns fill one inventory window, so six turns make three windows.
		segments = append(segments, analysisflow.Segment{ID: fmt.Sprintf("s%d", i), Speaker: "A", Text: fmt.Sprintf("Вопрос %d? %s", i, strings.Repeat("я", 3300))})
	}
	started := make(chan struct{}, 3)
	allStarted := make(chan struct{})
	var once sync.Once
	fixture := fixtureExecutor(t, false, false)
	runner := analysisflow.Runner{Segments: segments, Schema: provider.AnalysisSchema()}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		if strings.HasSuffix(key, "/extract/try0") {
			started <- struct{}{}
			if len(started) == cap(started) {
				once.Do(func() { close(allStarted) })
			}
			select {
			case <-allStarted:
			case <-time.After(5 * time.Second):
				return models.AnalysisResult{}, fmt.Errorf("inventory windows did not run concurrently")
			}
			// Finish the last window first to prove ordering does not follow completion.
			if strings.HasPrefix(key, "inventory/0/") {
				time.Sleep(50 * time.Millisecond)
			}
		}
		return fixture(ctx, key, task)
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	items := final["items"].([]any)
	require.Len(t, items, 6)
	for i, raw := range items {
		require.Equal(t, segments[i].Text, raw.(map[string]any)["title"])
	}
}

func TestAssessmentStartsBeforeSlowestInventoryWindowFinishes(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	segments := []analysisflow.Segment{}
	for i := 0; i < 6; i++ {
		segments = append(segments, analysisflow.Segment{ID: fmt.Sprintf("s%d", i), Speaker: "A", Text: fmt.Sprintf("Вопрос %d? %s", i, strings.Repeat("я", 3300))})
	}
	firstWindowAssessed := make(chan struct{})
	var once sync.Once
	fixture := fixtureExecutor(t, false, false)
	runner := analysisflow.Runner{Segments: segments, Schema: provider.AnalysisSchema()}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		if strings.HasPrefix(key, "assess/u1.") {
			once.Do(func() { close(firstWindowAssessed) })
		}
		if key == "inventory/2/extract/try0" {
			// Windows 0 and 1 are final, so their cards must not wait for this one.
			select {
			case <-firstWindowAssessed:
			case <-time.After(5 * time.Second):
				return models.AnalysisResult{}, fmt.Errorf("assessment waited for the slowest inventory window")
			}
		}
		return fixture(ctx, key, task)
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	var ids []string
	for i, raw := range final["items"].([]any) {
		item := raw.(map[string]any)
		ids = append(ids, item["id"].(string))
		require.Equal(t, segments[i].Text, item["title"])
		require.Equal(t, "ready", item["processing_status"])
		require.Equal(t, float64(i), item["order"])
	}
	require.Equal(t, []string{"u1.1", "u1.2", "u2.1", "u2.2", "u3.1", "u3.2"}, ids)
}

func TestAssessmentBatchesAreFullCardsAfterAnswerEpisodesMerge(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	kinds := []string{"question", "episode", "question", "episode", "question", "question", "episode", "question", "question"}
	segments := []analysisflow.Segment{}
	units := []analysisflow.Unit{}
	for i, kind := range kinds {
		id := fmt.Sprintf("s%d", i)
		segments = append(segments, analysisflow.Segment{ID: id, Speaker: "A", Text: fmt.Sprintf("Реплика %d", i)})
		units = append(units, analysisflow.Unit{ID: id, Kind: kind, Title: id, Topic: "Тема", SegmentIDs: []string{id}, Parts: []string{id}})
	}
	fixture := fixtureExecutor(t, false, false)
	var mu sync.Mutex
	batches := map[string][]string{}
	runner := analysisflow.Runner{Segments: segments, Schema: provider.AnalysisSchema()}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		if strings.HasPrefix(key, "inventory/") {
			raw, _ := json.Marshal(map[string]any{"units": units, "excluded": []any{}})
			return models.AnalysisResult{ResultJSON: raw}, nil
		}
		if strings.HasPrefix(key, "assess/") && strings.Contains(key, "/repair") {
			var input struct {
				Units []analysisflow.Unit `json:"assigned_units"`
			}
			require.NoError(t, json.Unmarshal([]byte(task.Input), &input))
			mu.Lock()
			for _, u := range input.Units {
				batches[key] = append(batches[key], u.ID)
			}
			mu.Unlock()
		}
		return fixture(ctx, key, task)
	}
	_, err := runner.Run(context.Background())
	require.NoError(t, err)
	// Six cards remain after merging answer episodes, so two full batches.
	require.Equal(t, map[string][]string{
		"assess/u1.1/repair0/try0": {"u1.1", "u1.3", "u1.5"},
		"assess/u1.6/repair0/try0": {"u1.6", "u1.8", "u1.9"},
	}, batches)
}

func TestInstructionRequirementsOverlapInventoryAndShareAssessmentContext(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	instruction := models.AnalysisInstructionContent{ID: uuid.New(), Title: "Приветствие", Content: "Поздороваться"}
	requirementsDone := make(chan struct{})
	fixture := fixtureExecutor(t, false, false)
	var mu sync.Mutex
	contexts := map[string]string{}
	runner := analysisflow.Runner{
		Request:  models.AnalysisRequest{Instructions: []models.AnalysisInstructionContent{instruction}},
		Segments: []analysisflow.Segment{{ID: "s0", Speaker: "A", Text: "Вопрос 0?"}},
		Schema:   provider.AnalysisSchema(),
	}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		switch {
		case key == "requirements/try0":
			defer close(requirementsDone)
			raw, _ := json.Marshal(map[string]any{"units": []any{map[string]any{"id": "req", "kind": "requirement", "title": "Поздороваться", "topic": "Приветствие", "segment_ids": []any{}, "parts": []any{"Поздороваться"}, "required_question": false}}, "excluded": []any{}})
			return models.AnalysisResult{ResultJSON: raw}, nil
		case key == "inventory/0/extract/try0":
			select {
			case <-requirementsDone:
			case <-time.After(5 * time.Second):
				return models.AnalysisResult{}, fmt.Errorf("requirements waited for the inventory")
			}
		case strings.HasPrefix(key, "assess/"):
			mu.Lock()
			contexts[key] = task.Context
			mu.Unlock()
			if strings.HasPrefix(key, "assess/r1/repair") {
				raw, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"id": "r1", "kind": "requirement", "explanation": "Приветствие прозвучало", "status": "met", "weight": 1, "strengths": []any{}, "gaps": []any{}, "improvement_kind": "not_needed", "evidence": []any{}, "instruction_sources": []any{instruction.ID.String()}}}})
				return models.AnalysisResult{ResultJSON: raw}, nil
			}
		}
		return fixture(ctx, key, task)
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	items := final["items"].([]any)
	require.Len(t, items, 2)
	require.Equal(t, "u1.1", items[0].(map[string]any)["id"])
	require.Equal(t, "r1", items[1].(map[string]any)["id"])
	require.Equal(t, contexts["assess/u1.1/repair0/try0"], contexts["assess/r1/repair0/try0"])
	require.Contains(t, contexts["assess/u1.1/repair0/try0"], "Поздороваться", "every assessment sees all requirements")
}

// A requirement the model broke out of an instruction comes back without its
// source now and then. The source is filled in from the instructions the model
// was decomposing, instead of failing the call and starting it again, and the
// card names the instruction by its title.
func TestRequirementWithoutSourceTakesTheDecomposedInstruction(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	instruction := models.AnalysisInstructionContent{ID: uuid.New(), Title: "Приветствие", Content: "Поздороваться"}
	fixture := fixtureExecutor(t, false, false)
	runner := analysisflow.Runner{
		Request:  models.AnalysisRequest{Instructions: []models.AnalysisInstructionContent{instruction}},
		Segments: []analysisflow.Segment{{ID: "s0", Speaker: "A", Text: "Вопрос 0?"}},
		Schema:   provider.AnalysisSchema(),
	}
	runner.Execute = func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		switch {
		case key == "requirements/try0":
			raw, _ := json.Marshal(map[string]any{"units": []any{map[string]any{"id": "req", "kind": "requirement", "title": "Поздороваться", "topic": "Приветствие", "segment_ids": []any{}, "parts": []any{"Поздороваться"}, "required_question": false}}, "excluded": []any{}})
			return models.AnalysisResult{ResultJSON: raw}, nil
		case strings.HasPrefix(key, "assess/r1/repair"):
			raw, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"id": "r1", "kind": "requirement", "explanation": "Приветствие прозвучало", "status": "met", "weight": 1, "strengths": []any{}, "gaps": []any{}, "improvement_kind": "not_needed", "evidence": []any{}, "instruction_sources": []any{}}}})
			return models.AnalysisResult{ResultJSON: raw}, nil
		}
		return fixture(ctx, key, task)
	}
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	var final map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &final))
	var requirement map[string]any
	for _, item := range final["items"].([]any) {
		if item.(map[string]any)["id"] == "r1" {
			requirement = item.(map[string]any)
		}
	}
	require.NotNil(t, requirement)
	require.Equal(t, []any{instruction.ID.String()}, requirement["instruction_sources"])
	require.Equal(t, []any{"Приветствие"}, requirement["instruction_titles"])
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
			var shared struct {
				Segments [][3]string `json:"context_segments"`
			}
			require.NoError(t, json.Unmarshal([]byte(task.Context), &shared))
			units := []analysisflow.Unit{}
			if !missingSource {
				for _, id := range ids {
					for _, s := range shared.Segments {
						if s[0] == id {
							units = append(units, analysisflow.Unit{ID: id, Kind: "question", Title: s[2], Topic: "Тема", SegmentIDs: []string{id}, Parts: []string{s[2]}})
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
