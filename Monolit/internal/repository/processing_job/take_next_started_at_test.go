//go:build integration

package processing_job

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTakeNextRecordsProcessingStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	jobID := uuid.New()
	entityID := uuid.New()
	createdAt := time.Now().UTC().Add(-2 * time.Hour)

	_, err := db.ExecContext(ctx, `
		INSERT INTO processing_jobs
			(job_uuid, job_type, entity_uuid, status, attempts, max_attempts, available_at, created_at, updated_at)
		VALUES ($1, 'transcribe_call', $2, 'pending', 0, 3, $3, $3, $3)
	`, jobID, entityID, createdAt)
	require.NoError(t, err)

	_, err = NewRepository(db).TakeNext(ctx, "monitoring-test-worker", time.Minute)
	require.NoError(t, err)

	var startedAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT started_at FROM processing_jobs WHERE job_uuid=$1`, jobID).Scan(&startedAt))
	require.True(t, startedAt.After(createdAt.Add(time.Hour)), "started_at must be recorded when the worker claims the job, not when it was enqueued")
}
