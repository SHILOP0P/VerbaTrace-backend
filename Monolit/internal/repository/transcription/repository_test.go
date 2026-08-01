//go:build integration

package transcription

import (
	"context"
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
	call := createTranscriptionCall(t, ctx, userRepo.NewUserRepository(db), callRepo.NewRepository(db))
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := models.Transcription{
		ID: uuid.New(), CallUUID: call.ID, Status: models.TranscriptionStatusProcessing,
		Provider: "openrouter", CreatedAt: now, UpdatedAt: now,
	}

	created, err := repository.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, input.ID, created.ID)

	got, err := repository.GetByCallUUID(ctx, call.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	language := "ru"
	start, end := 0.0, 2.5
	transcribed, err := repository.MarkTranscribed(ctx, created.ID, "Здравствуйте", []models.TranscriptionSegment{{
		Speaker: "manager", StartSeconds: &start, EndSeconds: &end, Text: "Здравствуйте",
	}}, &language)
	require.NoError(t, err)
	require.Equal(t, models.TranscriptionStatusTranscribed, transcribed.Status)
	require.Equal(t, "Здравствуйте", *transcribed.Text)
	require.Len(t, transcribed.Segments, 1)

	failed, err := repository.MarkFailed(ctx, created.ID, "transcriber failed")
	require.NoError(t, err)
	require.Equal(t, models.TranscriptionStatusFailed, failed.Status)
	require.Equal(t, "transcriber failed", *failed.ErrorMessage)

	_, err = repository.GetByCallUUID(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrTranscriptionNotFound)
	_, err = repository.MarkTranscribed(ctx, uuid.New(), "", nil, nil)
	require.ErrorIs(t, err, models.ErrTranscriptionNotFound)
	_, err = repository.MarkFailed(ctx, uuid.New(), "missing")
	require.ErrorIs(t, err, models.ErrTranscriptionNotFound)
}

func createTranscriptionCall(
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
		ID: uuid.New(), Title: "Transcription call", Status: models.CallStatusProcessing,
		AudioPath: "uploads/transcription.wav", OriginalFilename: "transcription.wav",
		MimeType: "audio/wav", SizeBytes: 10,
		UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal,
		CreatedAt:          time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)
	return call
}
