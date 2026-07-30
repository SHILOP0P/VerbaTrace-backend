//go:build integration

package analysis_instruction

import (
	"context"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/repositorytest"
	userRepo "calllens/monolit/internal/repository/user"

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
	userID := createInstructionUser(t, ctx, userRepo.NewUserRepository(db))
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := models.AnalysisInstruction{
		ID: uuid.New(), Scope: models.AnalysisInstructionScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		Title:    "Sales rubric", OriginalFilename: "rubric.txt", FilePath: "instructions/rubric.txt",
		MimeType: "text/plain", SizeBytes: 100, ContentSHA256: "abc123", SortOrder: 2,
		IsActive: true, CreatedByUserUUID: userID, CreatedAt: now, UpdatedAt: now,
	}

	created, err := repository.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, input.ID, created.ID)

	got, err := repository.GetByUUID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.Title, got.Title)

	filter := models.ListAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
	}
	list, err := repository.List(ctx, filter)
	require.NoError(t, err)
	require.Len(t, list, 1)

	count, err := repository.CountActive(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	require.NoError(t, repository.Deactivate(ctx, created.ID))
	_, err = repository.GetByUUID(ctx, created.ID)
	require.ErrorIs(t, err, models.ErrAnalysisInstructionNotFound)
	list, err = repository.List(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, list)
	count, err = repository.CountActive(ctx, filter)
	require.NoError(t, err)
	require.Zero(t, count)

	err = repository.Deactivate(ctx, created.ID)
	require.ErrorIs(t, err, models.ErrAnalysisInstructionNotFound)
	err = repository.Deactivate(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrAnalysisInstructionNotFound)
}

func createInstructionUser(t *testing.T, ctx context.Context, repository *userRepo.Repository) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := repository.CreateUser(ctx, models.CurrentUser{
		ID: id, Email: id.String() + "@example.com", PasswordHash: "hash",
		FullName: "Dmitry", FullSurname: "Mukhachev", Username: "user_" + id.String()[:8],
		Role: models.UserRoleUser, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)
	return id
}
