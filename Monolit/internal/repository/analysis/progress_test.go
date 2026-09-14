//go:build integration

package analysis

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
	callRepo "verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/repositorytest"
	userRepo "verbatrace/monolit/internal/repository/user"
)

func TestProgressPersistenceAndTaskRecovery(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	call := createAnalysisCall(t, ctx, userRepo.NewUserRepository(db), callRepo.NewRepository(db))
	repo := NewRepository(db)
	now := time.Now().UTC()
	analysis, err := repo.Create(ctx, models.CallAnalysis{ID: uuid.New(), CallUUID: call.ID, Status: models.CallAnalysisStatusProcessing, Provider: "openrouter", CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO call_transcriptions(transcription_uuid,call_uuid,status,provider,text,segments,words) VALUES($1,$2,'transcribed','mock','Вопрос?', '[]','[]')`, uuid.New(), call.ID)
	require.NoError(t, err)
	require.NoError(t, repo.BeginPipeline(ctx, analysis.ID, "run-a", 1))
	raw := json.RawMessage(`{"schema_version":3,"progress":{"stage":"answers","stage_rank":2,"items_done":3,"windows_done":1},"items":[{"id":"u1","processing_status":"ready"}]}`)
	require.NoError(t, repo.SaveProgress(ctx, analysis.ID, "run-a", 1, raw))
	require.NoError(t, repo.SaveProgress(ctx, analysis.ID, "run-a", 1, json.RawMessage(`{"progress":{"stage_rank":1,"items_done":0,"windows_done":0},"items":[]}`)))
	stored, err := repo.GetByCallUUID(ctx, call.ID)
	require.NoError(t, err)
	require.Contains(t, string(stored.ResultJSON), `"u1"`)
	require.Equal(t, models.CallAnalysisStatusProcessing, stored.Status)
	require.ErrorIs(t, repo.SaveProgress(ctx, analysis.ID, "wrong-run", 1, raw), models.ErrAnalysisSuperseded)
	taskID := uuid.New()
	cached, err := repo.ClaimAnalysisTask(ctx, taskID, analysis.ID)
	require.NoError(t, err)
	require.Nil(t, cached)
	_, err = repo.ClaimAnalysisTask(ctx, taskID, analysis.ID)
	require.ErrorContains(t, err, "uncertain provider outcome")
	receipt := models.AnalysisResult{ResultJSON: json.RawMessage(`{"items":[]}`), CreditOperationID: uuid.New(), Usage: &models.ProviderUsage{PromptTokens: 120, CompletionTokens: 40}}
	require.NoError(t, repo.SaveAnalysisTask(ctx, taskID, receipt))
	cached, err = repo.ClaimAnalysisTask(ctx, taskID, analysis.ID)
	require.NoError(t, err)
	require.Equal(t, receipt.CreditOperationID, cached.CreditOperationID)
	require.Equal(t, int64(40), cached.Usage.CompletionTokens)
	_, err = repo.MarkDone(ctx, analysis.ID, models.AnalysisResult{ResultJSON: raw, PipelineRunKey: "wrong-run", TranscriptionRevision: 1})
	require.Error(t, err)
	_, err = repo.MarkDone(ctx, analysis.ID, models.AnalysisResult{ResultJSON: raw, PipelineRunKey: "run-a", TranscriptionRevision: 1})
	require.NoError(t, err)
}
