//go:build integration

package teamanalytics

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// perfBudget is the spec's target (section 9): an analytics answer over 100
// thousand criterion facts in under 300 ms on the local stack.
const perfBudget = 300 * time.Millisecond

// Ten thousand analysed calls of ten criteria each, over 85 days, is the volume
// the spec measures against. Every analytics query of the page is timed warm.
func TestAnalyticsAnswersWithinBudgetOnAHundredThousandFacts(t *testing.T) {
	tm := newTeam(t, "business_plus")
	ctx := context.Background()
	keys := []uuid.UUID{tm.budget, tm.nextStep}
	for i := 3; i <= 10; i++ {
		key := uuid.New()
		keys = append(keys, key)
		tm.exec(`INSERT INTO instruction_scorecard_criteria (scorecard_uuid, criterion_key, position, title, requirement) VALUES ($1,$2,$3,$4,'т')`, tm.scorecard, key, i, "Критерий "+key.String()[:4])
	}
	tm.exec(`CREATE TEMP TABLE perf_calls AS
		SELECT gen_random_uuid() AS call_uuid, gen_random_uuid() AS analysis_uuid, i,
		       (ARRAY[$1::uuid, $2::uuid, $3::uuid])[1 + i % 3] AS subject,
		       now() - (i % 85) * interval '1 day' - (i % 600) * interval '1 minute' AS at
		FROM generate_series(1, 10000) AS i`, tm.ivan, tm.olga, tm.petr)
	tm.exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		SELECT call_uuid, 'Звонок', 'analyzed', 'a.mp3', 'a.mp3', 'audio/mpeg', 1, subject, $1, CASE WHEN subject = $2 THEN $3::uuid ELSE $4::uuid END, 'department', at FROM perf_calls`,
		tm.company, tm.petr, tm.support, tm.sales)
	tm.exec(`INSERT INTO call_subjects (call_uuid, user_uuid, source, is_primary, grants_access) SELECT call_uuid, subject, 'speaker_match', true, true FROM perf_calls`)
	tm.exec(`INSERT INTO analytics_call_facts (call_uuid, analysis_uuid, occurred_at, is_shared, schema_version, scorecard_mode, pipeline_version, ai_overall_score, criteria_score, coverage_status, criteria_total, criteria_scored)
		SELECT call_uuid, analysis_uuid, at, false, 3, 'fixed', 'universal-staged-v7', 40 + i % 60, 30 + i % 70, 'complete', 10, 10 FROM perf_calls`)
	tm.exec(`INSERT INTO analytics_criterion_facts (call_uuid, criterion_key, instruction_uuid, scorecard_uuid, item_id, ai_status, ai_score, weight, is_critical, evidence_start_seconds)
		SELECT c.call_uuid, k.key, $2, $3, 'r' || k.n, s.status, s.score, 1 + k.n % 3, k.n = 1, 10.5
		FROM perf_calls c
		CROSS JOIN LATERAL unnest($1::uuid[]) WITH ORDINALITY AS k(key, n)
		CROSS JOIN LATERAL (SELECT (ARRAY[0, 25, 50, 75, 100])[1 + (c.i + k.n::int) % 5] AS score) sc
		CROSS JOIN LATERAL (SELECT CASE sc.score WHEN 100 THEN 'met' WHEN 75 THEN 'mostly_met' WHEN 50 THEN 'partially_met' WHEN 25 THEN 'minimally_met' ELSE 'missed' END AS status, sc.score) s`,
		uuidArray(keys), tm.instruction, tm.scorecard)
	tm.exec(`ANALYZE calls, call_subjects, analytics_call_facts, analytics_criterion_facts`)
	var facts int
	require.NoError(t, tm.db.QueryRow(`SELECT count(*) FROM analytics_criterion_facts`).Scan(&facts))
	require.Equal(t, 100000, facts)

	owner := tm.req(tm.owner)
	from, to := tm.now.AddDate(0, 0, -90), tm.now.Add(time.Hour)
	owner.From, owner.To = &from, &to
	instruction := owner
	instruction.InstructionID = uuid.NullUUID{UUID: tm.instruction, Valid: true}
	queries := []struct {
		name string
		run  func() error
	}{
		{"summary", func() error { _, err := tm.service.Summary(ctx, owner); return err }},
		{"criteria", func() error { _, err := tm.service.Criteria(ctx, owner, "", ""); return err }},
		{"employees", func() error { _, err := tm.service.Employees(ctx, owner); return err }},
		{"departments", func() error { _, err := tm.service.Departments(ctx, owner); return err }},
		{"matrix", func() error { _, err := tm.service.Matrix(ctx, instruction); return err }},
		{"criterion calls", func() error {
			_, err := tm.service.CriterionCalls(ctx, owner, tm.budget, "", "score", 50, 0)
			return err
		}},
		{"profile", func() error { _, err := tm.service.Profile(ctx, owner, tm.ivan); return err }},
	}
	var slow []string
	for _, query := range queries {
		require.NoError(t, query.run(), query.name)
		// The best of three: the budget is about the query, not about what else
		// the machine happens to be doing during the pre-commit run.
		took := time.Duration(math.MaxInt64)
		for i := 0; i < 3; i++ {
			started := time.Now()
			require.NoError(t, query.run(), query.name)
			took = min(took, time.Since(started))
		}
		t.Logf("%-16s %v", query.name, took.Round(time.Millisecond))
		if took >= perfBudget {
			slow = append(slow, query.name+" "+took.Round(time.Millisecond).String())
		}
	}
	require.Empty(t, slow, "over the budget of "+perfBudget.String())
}

func uuidArray(ids []uuid.UUID) string {
	out := "{"
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id.String()
	}
	return out + "}"
}
