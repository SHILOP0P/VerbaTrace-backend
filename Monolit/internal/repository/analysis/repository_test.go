//go:build integration

package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	callRepo "verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/repositorytest"
	userRepo "verbatrace/monolit/internal/repository/user"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRepositoryLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	call := createAnalysisCall(t, ctx, userRepo.NewUserRepository(db), callRepo.NewRepository(db))
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := models.CallAnalysis{
		ID: uuid.New(), CallUUID: call.ID, Status: models.CallAnalysisStatusPending,
		Provider: "openrouter", CreatedAt: now, UpdatedAt: now,
	}

	created, err := repository.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, input.ID, created.ID)

	instructionID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO analysis_instructions(instruction_uuid,scope,user_uuid,title,original_filename,file_path,mime_type,size_bytes,content_sha256,sort_order,is_active,created_by_user_uuid,created_at,updated_at) VALUES($1,'personal',$2,'Контроль следующего шага','next-step.md','personal/test/next-step.md','text/markdown',18,'snapshot-hash',0,true,$2,now(),now())`, instructionID, call.UploadedByUserUUID.UUID)
	require.NoError(t, err)
	require.NoError(t, repository.SaveInstructionSnapshots(ctx, created.ID, []models.AnalysisInstructionContent{{ID: instructionID, Scope: models.AnalysisInstructionScopePersonal, Title: "Контроль следующего шага", Content: "Проверить следующий шаг", ContentSHA256: "snapshot-hash"}}))
	snapshots, err := repository.ListInstructionSnapshots(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, "Контроль следующего шага", snapshots[0].Title)
	detail, err := repository.GetInstructionSnapshot(ctx, created.ID, snapshots[0].VersionUUID)
	require.NoError(t, err)
	require.Equal(t, "Проверить следующий шаг", detail.Content)

	var retentionDays int
	var retentionBase, retentionExpires time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT retention_days_at_creation,retention_base_at,retention_expires_at FROM calls WHERE call_uuid=$1`, call.ID).Scan(&retentionDays, &retentionBase, &retentionExpires))
	require.Equal(t, 30, retentionDays)
	require.Equal(t, retentionBase.AddDate(0, 0, retentionDays), retentionExpires)
	rows, err := db.QueryContext(ctx, `SELECT code,history_retention_days FROM plans WHERE code LIKE 'business_%' ORDER BY code`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	businessRetention := map[string]int{}
	for rows.Next() {
		var code string
		var days int
		require.NoError(t, rows.Scan(&code, &days))
		businessRetention[code] = days
	}
	require.Equal(t, map[string]int{"business_plus": 365, "business_pro": 550, "business_start": 180}, businessRetention)

	got, err := repository.GetByCallUUID(ctx, call.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	processing, err := repository.MarkProcessing(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, models.CallAnalysisStatusProcessing, processing.Status)

	modelName := "test-model"
	resultText := "summary"
	done, err := repository.MarkDone(ctx, created.ID, models.AnalysisResult{
		ResultJSON: json.RawMessage(`{"score":91}`),
		ResultText: &resultText,
		Model:      &modelName,
	})
	require.NoError(t, err)
	require.Equal(t, models.CallAnalysisStatusDone, done.Status)
	require.JSONEq(t, `{"score":91}`, string(done.ResultJSON))

	failed, err := repository.MarkFailed(ctx, created.ID, "provider failed")
	require.NoError(t, err)
	require.Equal(t, models.CallAnalysisStatusFailed, failed.Status)
	require.Equal(t, "provider failed", *failed.ErrorMessage)

	_, err = repository.GetByCallUUID(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrAnalysisNotFound)
	_, err = repository.MarkProcessing(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrAnalysisNotFound)
	_, err = repository.MarkDone(ctx, uuid.New(), models.AnalysisResult{})
	require.ErrorIs(t, err, models.ErrAnalysisNotFound)
	_, err = repository.MarkFailed(ctx, uuid.New(), "missing")
	require.ErrorIs(t, err, models.ErrAnalysisNotFound)

	invalid := input
	invalid.ID = uuid.Nil
	invalid.CallUUID = uuid.Nil
	_, err = repository.Create(ctx, invalid)
	require.True(t, errors.Is(err, models.ErrInvalidAnalysisInput) || err != nil)
}

func createAnalysisCall(
	t *testing.T,
	ctx context.Context,
	users *userRepo.Repository,
	calls *callRepo.Repository,
) models.Call {
	t.Helper()

	userID := uuid.New()
	_, err := users.CreateUser(ctx, models.CurrentUser{
		ID: userID, Email: userID.String() + "@example.com", PasswordHash: "hash",
		FullName: "Dmitry", FullSurname: "Mukhachev", Username: "user_" + userID.String()[:8],
		Role: models.UserRoleUser, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)

	call, err := calls.CreateCall(ctx, models.Call{
		ID: uuid.New(), Title: "Analysis call", Status: models.CallStatusTranscribed,
		AudioPath: "uploads/analysis.wav", OriginalFilename: "analysis.wav",
		MimeType: "audio/wav", SizeBytes: 10,
		UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal,
		CreatedAt:          time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)
	return call
}
