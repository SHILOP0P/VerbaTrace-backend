//go:build integration

package report

import (
	"context"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	callRepo "calllens/monolit/internal/repository/call"
	"calllens/monolit/internal/repository/repositorytest"
	userRepo "calllens/monolit/internal/repository/user"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAggregateReportRepositoryLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	userID, analysis := createAggregateReportDependencies(t, ctx, userRepo.NewUserRepository(db), callRepo.NewRepository(db))
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := models.AggregateReportExport{
		ID: uuid.New(), AggregateAnalysisUUID: analysis.ID,
		RequestedByUserUUID: userID, Format: models.ReportFormatMD,
		Status: models.ReportStatusPending, FileName: "deep.md", ContentType: "text/markdown",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
	}

	created, err := repository.CreateAggregate(ctx, input)
	require.NoError(t, err)
	require.Equal(t, input.ID, created.ID)

	got, err := repository.GetAggregateByUUID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.AggregateAnalysisUUID, got.AggregateAnalysisUUID)

	ready, err := repository.MarkAggregateReady(ctx, models.MarkAggregateReportReadyInput{
		ID: created.ID, StoragePath: "aggregate/deep.md", FileName: "ready.md",
		ContentType: "text/markdown", SizeBytes: 123,
	})
	require.NoError(t, err)
	require.Equal(t, models.ReportStatusReady, ready.Status)
	require.Equal(t, "aggregate/deep.md", *ready.StoragePath)

	list, err := repository.ListAggregateByAnalysisUUID(ctx, analysis.ID, now)
	require.NoError(t, err)
	require.Len(t, list, 1)

	failure := input
	failure.ID = uuid.New()
	failure.FileName = "failed.md"
	_, err = repository.CreateAggregate(ctx, failure)
	require.NoError(t, err)
	failed, err := repository.MarkAggregateFailed(ctx, models.MarkAggregateReportFailedInput{
		ID: failure.ID, ErrorMessage: "generation failed",
	})
	require.NoError(t, err)
	require.Equal(t, models.ReportStatusFailed, failed.Status)
	require.Equal(t, "generation failed", *failed.ErrorMessage)

	require.NoError(t, repository.DeleteAggregate(ctx, created.ID))
	_, err = repository.GetAggregateByUUID(ctx, created.ID)
	require.ErrorIs(t, err, models.ErrAggregateReportNotFound)
	require.ErrorIs(t, repository.DeleteAggregate(ctx, created.ID), models.ErrAggregateReportNotFound)
	_, err = repository.MarkAggregateReady(ctx, models.MarkAggregateReportReadyInput{ID: uuid.New()})
	require.ErrorIs(t, err, models.ErrAggregateReportNotFound)
	_, err = repository.MarkAggregateFailed(ctx, models.MarkAggregateReportFailedInput{ID: uuid.New()})
	require.ErrorIs(t, err, models.ErrAggregateReportNotFound)
}

func createAggregateReportDependencies(
	t *testing.T,
	ctx context.Context,
	users *userRepo.Repository,
	calls *callRepo.Repository,
) (uuid.UUID, models.AggregateAnalysis) {
	t.Helper()
	userID := uuid.New()
	_, err := users.CreateUser(ctx, models.CurrentUser{
		ID: userID, Email: userID.String() + "@example.com", PasswordHash: "hash",
		FullName: "Dmitry", FullSurname: "Mukhachev", Username: "aggregate_" + userID.String()[:8],
		Role: models.UserRoleUser, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Microsecond)
	analysis, err := calls.CreateAggregateAnalysis(ctx, models.AggregateAnalysis{
		ID: uuid.New(), Scope: models.AggregateAnalysisScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true}, PeriodFrom: now.AddDate(0, 0, -7), PeriodTo: now,
		Status: models.AggregateAnalysisStatusDone, Provider: "mock", SourceCallsCount: 2,
		ResultJSON: []byte(`{"summary":"summary"}`), CreatedByUserUUID: userID, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	return userID, analysis
}
