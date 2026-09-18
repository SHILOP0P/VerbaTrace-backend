//go:build integration

package analysis

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	callRepo "verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/repositorytest"
	userRepo "verbatrace/monolit/internal/repository/user"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A published human review of a schema v3 analysis must reach the cards a
// reader sees, not only the top-level score.
func TestEffectiveAnalysisAppliesHumanReviewToV3Items(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	call := createAnalysisCall(t, ctx, userRepo.NewUserRepository(db), callRepo.NewRepository(db))
	owner := call.UploadedByUserUUID.UUID
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	companyID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO companies(company_uuid,name,tag,manager_user_uuid,member_limit) VALUES($1,'Company','@company-v3',$2,10)`, companyID, owner)
	require.NoError(t, err)

	analysis, err := repository.Create(ctx, models.CallAnalysis{ID: uuid.New(), CallUUID: call.ID, Status: models.CallAnalysisStatusPending, Provider: "mock_staged", CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	_, err = repository.MarkDone(ctx, analysis.ID, models.AnalysisResult{ResultJSON: json.RawMessage(`{
		"schema_version": 3, "overall_score": 50, "score": 50, "score_scale": 100,
		"items": [
			{"id": "r1", "kind": "requirement", "title": "Выяснил бюджет", "status": "missed", "score": 0, "weight": 1},
			{"id": "r2", "kind": "requirement", "title": "Выяснил сроки", "status": "met", "score": 100, "weight": 1},
			{"id": "u1.1", "kind": "question", "title": "Какой бюджет?", "status": "partially_met", "score": 50, "weight": 1}
		]}`)})
	require.NoError(t, err)

	reviewID, revisionID := uuid.New(), uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO call_quality_reviews(review_uuid,call_uuid,analysis_uuid,transcription_revision,company_uuid,status,created_by_user_uuid,created_at,updated_at) VALUES($1,$2,$3,1,$4,'published',$5,now(),now())`, reviewID, call.ID, analysis.ID, companyID, owner)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO call_quality_review_revisions(revision_uuid,review_uuid,revision_number,author_user_uuid,status,human_score,score_max,source_hash,created_at,updated_at,published_at) VALUES($1,$2,1,$3,'published',66.666,100,'hash',now(),now(),now())`, revisionID, reviewID, owner)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO call_quality_review_criteria(criterion_uuid,revision_uuid,criterion_key,title_snapshot,ai_score,human_score,score_min,score_max,weight,decision,position) VALUES
		($1,$4,'r1','Выяснил бюджет',0,100,0,100,1,'overridden',0),
		($2,$4,'r2','Выяснил сроки',100,NULL,0,100,1,'not_applicable',1),
		($3,$4,'u1.1','Какой бюджет?',50,NULL,0,100,1,'unscored',2)`, uuid.New(), uuid.New(), uuid.New(), revisionID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE call_quality_reviews SET active_revision_uuid=$2 WHERE review_uuid=$1`, reviewID, revisionID)
	require.NoError(t, err)

	got, err := repository.GetByCallUUID(ctx, call.ID)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(got.ResultJSON, &result))
	require.Nil(t, result["criteria_results"], "a v3 result must not grow a v2 criteria list")
	require.EqualValues(t, 67, result["overall_score"])
	require.Equal(t, "67 / 100", result["overall_score_label"])
	require.Equal(t, "human_review_1", result["effective_source"])

	items := result["items"].([]any)
	budget, deadline, question := items[0].(map[string]any), items[1].(map[string]any), items[2].(map[string]any)
	require.EqualValues(t, 100, budget["score"])
	require.Equal(t, "human_review_1", budget["effective_source"])
	require.Equal(t, "not_applicable", deadline["status"])
	require.Nil(t, deadline["score"])
	require.EqualValues(t, 50, question["score"])
	require.Equal(t, "ai", question["effective_source"])
}
