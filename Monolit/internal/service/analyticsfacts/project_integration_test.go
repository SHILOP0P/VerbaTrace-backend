//go:build integration

package analyticsfacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

type fixture struct {
	db                                       *sql.DB
	owner, company, call, analysis           uuid.UUID
	instruction, other, scorecard, otherCard uuid.UUID
	budget, nextStep, shared, otherShared    uuid.UUID
}

func seed(t *testing.T) fixture {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	f := fixture{db: db, owner: repositorytest.CreateUser(t, db), company: uuid.New(), call: uuid.New(), analysis: uuid.New(),
		instruction: uuid.New(), other: uuid.New(), budget: uuid.New(), nextStep: uuid.New(), shared: uuid.New(), otherShared: uuid.New()}
	exec(t, db, `INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Ромашка',$2,$3,now())`, f.company, "t"+f.company.String()[:8], f.owner)
	repositorytest.InsertCompanyMember(t, db, f.company, f.owner, "company_manager", "active")
	exec(t, db, `INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at)
		VALUES ($1,'Звонок','analyzed','a.mp3','a.mp3','audio/mpeg',1,$2,$3,'company',now())`, f.call, f.owner, f.company)
	for _, id := range []uuid.UUID{f.instruction, f.other} {
		exec(t, db, `INSERT INTO analysis_instructions (instruction_uuid, scope, company_uuid, title, original_filename, file_path, mime_type, size_bytes, content_sha256, sort_order, is_active, created_by_user_uuid, created_at, updated_at)
			VALUES ($1,'company',$2,'Стандарт','s.md',$3,'text/markdown',1,'h',0,true,$4,now(),now())`, id, f.company, "p/"+id.String(), f.owner)
	}
	// Saving an instruction queues its scorecard; criteria are attached to it here.
	require.NoError(t, db.QueryRow(`SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_uuid = $1`, f.instruction).Scan(&f.scorecard))
	require.NoError(t, db.QueryRow(`SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_uuid = $1`, f.other).Scan(&f.otherCard))
	for i, key := range []uuid.UUID{f.budget, f.nextStep, f.shared} {
		exec(t, db, `INSERT INTO instruction_scorecard_criteria (scorecard_uuid, criterion_key, position, title, requirement) VALUES ($1,$2,$3,'К','Т')`, f.scorecard, key, i+1)
	}
	exec(t, db, `INSERT INTO instruction_scorecard_criteria (scorecard_uuid, criterion_key, position, title, requirement) VALUES ($1,$2,1,'К','Т')`, f.otherCard, f.otherShared)

	requirement := func(id string, key uuid.UUID, status string, score any, weight int, critical bool, also ...uuid.UUID) map[string]any {
		alsoKeys := []string{}
		for _, a := range also {
			alsoKeys = append(alsoKeys, a.String())
		}
		return map[string]any{"id": id, "kind": "requirement", "status": status, "score": score, "weight": weight, "is_critical": critical,
			"criterion_key": key.String(), "also_criterion_keys": alsoKeys, "scorecard_uuid": f.scorecard.String(),
			"instruction_sources": []string{f.instruction.String()}, "evidence": []any{map[string]any{"segment_id": "s1", "start_seconds": 12.5}}}
	}
	result := map[string]any{
		"schema_version": 3, "pipeline_version": "universal-staged-v7", "scorecard_mode": "fixed", "overall_score": 70,
		"coverage": map[string]any{"status": "complete"},
		"items": []any{
			requirement("r1", f.budget, "missed", 0, 3, true),
			requirement("r2", f.nextStep, "met", 100, 1, false),
			requirement("r3", f.shared, "mostly_met", 75, 2, false, f.otherShared),
			map[string]any{"id": "u1.1", "kind": "question", "status": "partially_met", "score": 50, "weight": 1},
			map[string]any{"id": "u1.2", "kind": "question", "status": "unclear", "score": nil, "weight": 1},
		},
	}
	raw, _ := json.Marshal(result)
	exec(t, db, `INSERT INTO call_analyses (analysis_uuid, call_uuid, status, provider, model, result_json, created_at, updated_at) VALUES ($1,$2,'done','openrouter','openai/gpt-5-mini',$3::jsonb,now(),now())`, f.analysis, f.call, raw)
	return f
}

type fact struct {
	score, ai, human sql.NullInt64
	counted          bool
	instruction      uuid.UUID
}

func criterionFacts(t *testing.T, db *sql.DB, call uuid.UUID) map[uuid.UUID]fact {
	rows, err := db.Query(`SELECT criterion_key, score, ai_score, human_score, counted_in_score, instruction_uuid FROM analytics_criterion_facts WHERE call_uuid = $1`, call)
	require.NoError(t, err)
	defer rows.Close()
	result := map[uuid.UUID]fact{}
	for rows.Next() {
		var key uuid.UUID
		var f fact
		require.NoError(t, rows.Scan(&key, &f.score, &f.ai, &f.human, &f.counted, &f.instruction))
		result[key] = f
	}
	return result
}

func TestProjectionKeepsHumanScoresBesideTheModelAndIsIdempotent(t *testing.T) {
	f := seed(t)
	ctx := context.Background()
	service := NewService(f.db, nil)
	require.NoError(t, service.Project(ctx, f.call))

	var overall, criteria, critical, questions, total, scored sql.NullInt64
	var mode string
	var unassessed float64
	require.NoError(t, f.db.QueryRow(`SELECT overall_score, criteria_score, critical_missed, questions_total, criteria_total, criteria_scored, scorecard_mode, unassessed_weight_share::float8 FROM analytics_call_facts WHERE call_uuid = $1`, f.call).
		Scan(&overall, &criteria, &critical, &questions, &total, &scored, &mode, &unassessed))
	require.EqualValues(t, 70, overall.Int64)
	require.EqualValues(t, 42, criteria.Int64, "(0·3 + 100·1 + 75·2) / 6")
	require.EqualValues(t, 1, critical.Int64)
	require.EqualValues(t, 2, questions.Int64)
	require.EqualValues(t, 3, total.Int64)
	require.EqualValues(t, 3, scored.Int64)
	require.Equal(t, "fixed", mode)
	require.InDelta(t, 0.13, unassessed, 0.001, "one unclear item of weight 1 out of 8")

	facts := criterionFacts(t, f.db, f.call)
	require.Len(t, facts, 4)
	require.False(t, facts[f.otherShared].counted, "a duplicate is written for its own instruction but not counted twice")
	require.Equal(t, f.other, facts[f.otherShared].instruction)

	// A reviewer overrides one criterion and finds another not applicable.
	review, revision := uuid.New(), uuid.New()
	exec(t, f.db, `INSERT INTO call_quality_reviews (review_uuid, call_uuid, analysis_uuid, transcription_revision, company_uuid, status, created_by_user_uuid, created_at, updated_at, published_at)
		VALUES ($1,$2,$3,1,$4,'published',$5,now(),now(),now())`, review, f.call, f.analysis, f.company, f.owner)
	exec(t, f.db, `INSERT INTO call_quality_review_revisions (revision_uuid, review_uuid, revision_number, author_user_uuid, status, human_score, score_max, source_hash, created_at, updated_at, published_at)
		VALUES ($1,$2,1,$3,'published',80,100,'h',now(),now(),now())`, revision, review, f.owner)
	exec(t, f.db, `UPDATE call_quality_reviews SET active_revision_uuid = $2 WHERE review_uuid = $1`, review, revision)
	exec(t, f.db, `INSERT INTO call_quality_review_criteria (criterion_uuid, revision_uuid, criterion_key, title_snapshot, ai_score, human_score, score_min, score_max, weight, decision, position)
		VALUES ($1,$2,'r1','К',0,50,0,100,3,'overridden',0), ($3,$2,'r2','К',100,NULL,0,100,1,'not_applicable',1)`, uuid.New(), revision, uuid.New())

	require.NoError(t, service.Project(ctx, f.call))
	require.NoError(t, service.Project(ctx, f.call), "projecting twice changes nothing")
	facts = criterionFacts(t, f.db, f.call)
	require.EqualValues(t, 50, facts[f.budget].score.Int64)
	require.EqualValues(t, 0, facts[f.budget].ai.Int64, "the model's score stays beside the reviewer's")
	require.False(t, facts[f.nextStep].score.Valid, "not applicable leaves the formula")
	require.True(t, facts[f.nextStep].ai.Valid)
	require.NoError(t, f.db.QueryRow(`SELECT overall_score, criteria_score, critical_missed FROM analytics_call_facts WHERE call_uuid = $1`, f.call).Scan(&overall, &criteria, &critical))
	require.EqualValues(t, 80, overall.Int64, "the human overall score wins")
	require.EqualValues(t, 60, criteria.Int64, "(50·3 + 75·2) / 5")
	require.EqualValues(t, 0, critical.Int64)
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM analytics_call_facts`).Scan(&count))
	require.Equal(t, 1, count)

	// Tying a criterion to an older one moves its history, in the same transaction.
	older := uuid.New()
	tx, err := f.db.Begin()
	require.NoError(t, err)
	require.NoError(t, Rekey(ctx, tx, f.budget, older))
	require.NoError(t, tx.Commit())
	facts = criterionFacts(t, f.db, f.call)
	require.Contains(t, facts, older)
	require.NotContains(t, facts, f.budget)
	exec(t, f.db, `INSERT INTO criterion_key_aliases (alias_key, canonical_key, instruction_uuid) VALUES ($1,$2,$3)`, f.budget, older, f.instruction)
	require.NoError(t, service.Project(ctx, f.call))
	require.Contains(t, criterionFacts(t, f.db, f.call), older, "a later projection keys through the alias")
}

func TestWorkerFillsFactsForCallsAnalysedBefore(t *testing.T) {
	f := seed(t)
	ctx := context.Background()
	service := NewService(f.db, nil)
	require.Positive(t, NewWorker(service, nil, 0, 0).RunOnce(ctx))
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM analytics_call_facts WHERE call_uuid = $1`, f.call).Scan(&count))
	require.Equal(t, 1, count)
	require.Zero(t, NewWorker(service, nil, 0, 0).RunOnce(ctx), "nothing left to catch up")
}
