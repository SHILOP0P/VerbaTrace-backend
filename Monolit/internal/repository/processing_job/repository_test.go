//go:build integration

package processing_job

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"

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
	repository := NewRepository(db)
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)

	created, err := repository.Create(ctx, testJob(now, models.ProcessingJobTypeTranscribeCall))
	require.NoError(t, err)
	require.Equal(t, models.ProcessingJobStatusPending, created.Status)

	taken, err := repository.TakeNext(ctx, "worker-1", time.Minute)
	require.NoError(t, err)
	require.Equal(t, created.ID, taken.ID)
	require.Equal(t, models.ProcessingJobStatusRunning, taken.Status)
	require.Equal(t, 1, taken.Attempts)
	require.Equal(t, "worker-1", *taken.LockedBy)

	retried, err := repository.MarkRetry(ctx, taken.ID, "temporary", 0)
	require.NoError(t, err)
	require.Equal(t, models.ProcessingJobStatusPending, retried.Status)
	require.Equal(t, "temporary", *retried.LastError)

	takenAgain, err := repository.TakeNext(ctx, "worker-2", time.Minute)
	require.NoError(t, err)
	done, err := repository.MarkDone(ctx, takenAgain.ID)
	require.NoError(t, err)
	require.Equal(t, models.ProcessingJobStatusDone, done.Status)

	second := testJob(now, models.ProcessingJobTypeAnalyzeCall)
	enqueued, err := repository.Enqueue(ctx, second)
	require.NoError(t, err)
	running, err := repository.TakeNext(ctx, "worker-3", time.Minute)
	require.NoError(t, err)
	require.Equal(t, enqueued.ID, running.ID)
	failed, err := repository.MarkFailed(ctx, running.ID, "permanent")
	require.NoError(t, err)
	require.Equal(t, models.ProcessingJobStatusFailed, failed.Status)

	requeued, err := repository.Enqueue(ctx, second)
	require.NoError(t, err)
	require.Equal(t, models.ProcessingJobStatusPending, requeued.Status)
	require.Zero(t, requeued.Attempts)

	_, err = repository.TakeNext(ctx, "worker-4", time.Minute)
	require.NoError(t, err)
	_, err = repository.TakeNext(ctx, "worker-5", time.Minute)
	require.ErrorIs(t, err, models.ErrNoProcessingJobs)

	_, err = repository.MarkDone(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrProcessingJobNotFound)
	_, err = repository.MarkRetry(ctx, uuid.New(), "missing", time.Second)
	require.ErrorIs(t, err, models.ErrProcessingJobNotFound)
	_, err = repository.MarkFailed(ctx, uuid.New(), "missing")
	require.ErrorIs(t, err, models.ErrProcessingJobNotFound)
}

func TestMarkRetryFailsAfterMaxAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	repository := NewRepository(db)
	job := testJob(time.Now().UTC().Add(-time.Second), models.ProcessingJobTypeTranscribeCall)
	job.MaxAttempts = 1
	created, err := repository.Create(ctx, job)
	require.NoError(t, err)
	running, err := repository.TakeNext(ctx, "worker", time.Minute)
	require.NoError(t, err)

	failed, err := repository.MarkRetry(ctx, running.ID, "last attempt", time.Minute)
	require.NoError(t, err)
	require.Equal(t, models.ProcessingJobStatusFailed, failed.Status)
	require.Equal(t, created.ID, failed.ID)
}

func testJob(now time.Time, jobType models.ProcessingJobType) models.ProcessingJob {
	return models.ProcessingJob{
		ID: uuid.New(), Type: jobType, EntityUUID: uuid.New(),
		Status: models.ProcessingJobStatusPending, MaxAttempts: 3,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
}
