//go:build integration

package report

import (
	"context"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	analysisRepo "calllens/monolit/internal/repository/analysis"
	callRepo "calllens/monolit/internal/repository/call"
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
	userID, call, analysis := createReportDependencies(t, ctx,
		userRepo.NewUserRepository(db), callRepo.NewRepository(db), analysisRepo.NewRepository(db))
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := models.ReportExport{
		ID: uuid.New(), CallUUID: call.ID, AnalysisUUID: analysis.ID,
		RequestedByUserUUID: userID, Format: models.ReportFormatMD,
		Status: models.ReportStatusPending, FileName: "report.md", ContentType: "text/markdown",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
	}

	created, err := repository.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, input.ID, created.ID)

	got, err := repository.GetByUUID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	ready, err := repository.MarkReady(ctx, models.MarkReportReadyInput{
		ID: created.ID, StoragePath: "reports/report.md", FileName: "ready.md",
		ContentType: "text/markdown", SizeBytes: 321,
	})
	require.NoError(t, err)
	require.Equal(t, models.ReportStatusReady, ready.Status)
	require.Equal(t, "reports/report.md", *ready.StoragePath)

	list, err := repository.ListByCallUUID(ctx, call.ID, now)
	require.NoError(t, err)
	require.Len(t, list, 1)

	expired := input
	expired.ID = uuid.New()
	expired.Format = models.ReportFormatPDF
	expired.Status = models.ReportStatusReady
	expired.ExpiresAt = now.Add(-time.Minute)
	expired.FileName = "expired.md"
	expiredPath := "reports/expired.md"
	expired.StoragePath = &expiredPath
	expired.SizeBytes = 123
	_, err = repository.Create(ctx, expired)
	require.NoError(t, err)
	expiredList, err := repository.ListExpiredReady(ctx, now, 0)
	require.NoError(t, err)
	require.Len(t, expiredList, 1)
	require.Equal(t, expired.ID, expiredList[0].ID)

	failure := input
	failure.ID = uuid.New()
	failure.Format = models.ReportFormatPDF
	failure.FileName = "failed.md"
	_, err = repository.Create(ctx, failure)
	require.NoError(t, err)
	failed, err := repository.MarkFailed(ctx, models.MarkReportFailedInput{
		ID: failure.ID, ErrorMessage: "generation failed",
	})
	require.NoError(t, err)
	require.Equal(t, models.ReportStatusFailed, failed.Status)
	require.Equal(t, "generation failed", *failed.ErrorMessage)

	require.NoError(t, repository.Delete(ctx, created.ID))
	_, err = repository.GetByUUID(ctx, created.ID)
	require.ErrorIs(t, err, models.ErrReportNotFound)
	require.ErrorIs(t, repository.Delete(ctx, created.ID), models.ErrReportNotFound)
	_, err = repository.MarkReady(ctx, models.MarkReportReadyInput{ID: uuid.New()})
	require.ErrorIs(t, err, models.ErrReportNotFound)
	_, err = repository.MarkFailed(ctx, models.MarkReportFailedInput{ID: uuid.New()})
	require.ErrorIs(t, err, models.ErrReportNotFound)
}

func createReportDependencies(
	t *testing.T,
	ctx context.Context,
	users *userRepo.Repository,
	calls *callRepo.Repository,
	analyses *analysisRepo.Repository,
) (uuid.UUID, models.Call, models.CallAnalysis) {
	t.Helper()

	userID := uuid.New()
	_, err := users.CreateUser(ctx, models.User{
		ID: userID, Email: userID.String() + "@example.com", PasswordHash: "hash",
		FullName: "Dmitry", FullSurname: "Mukhachev", Username: "user_" + userID.String()[:8],
		Role: models.UserRoleUser, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)

	call, err := calls.CreateCall(ctx, models.Call{
		ID: uuid.New(), Title: "Report call", Status: models.CallStatusAnalyzed,
		AudioPath: "uploads/report.wav", OriginalFilename: "report.wav",
		MimeType: "audio/wav", SizeBytes: 10,
		UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal,
		CreatedAt:          time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Microsecond)
	analysis, err := analyses.Create(ctx, models.CallAnalysis{
		ID: uuid.New(), CallUUID: call.ID, Status: models.CallAnalysisStatusPending,
		Provider: "openrouter", CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	resultText := "Report source"
	modelName := "test-model"
	analysis, err = analyses.MarkDone(ctx, analysis.ID, models.AnalysisResult{
		ResultJSON: []byte(`{"summary":"Report source"}`),
		ResultText: &resultText,
		Model:      &modelName,
	})
	require.NoError(t, err)
	return userID, call, analysis
}
