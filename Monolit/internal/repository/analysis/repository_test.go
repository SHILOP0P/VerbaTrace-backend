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
