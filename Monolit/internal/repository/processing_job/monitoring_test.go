//go:build integration

package processing_job

import (
	"context"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetMonitoringAggregatesQueueAndLastFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	repository := NewRepository(db)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	companyID := uuid.New()
	firstCallID := uuid.New()
	secondCallID := uuid.New()
	thirdCallID := uuid.New()
	fourthCallID := uuid.New()
	otherCallID := uuid.New()

	managerID := uuid.New()
	_, err := db.ExecContext(ctx, `
		WITH account AS (
			INSERT INTO users (user_uuid, email, password_hash, role, created_at)
			VALUES ($1, 'manager@example.com', 'hash', 'user', $2)
			RETURNING user_uuid
		)
		INSERT INTO user_profiles (user_uuid, full_name, full_surname, username)
		SELECT user_uuid, 'Dmitry', 'Mukhachev', '@manager' FROM account
	`, managerID, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit, created_at)
		VALUES ($1, 'CallLens', $2, $3, 5, $4)
	`, companyID, "@"+companyID.String(), managerID, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status, created_at)
		VALUES ($1, $2, 'company_manager', 'active', $3)
	`, companyID, managerID, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, duration_seconds, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		VALUES
			($1, 'company call 1', 'processing', 'a.wav', 'a.wav', 'audio/wav', 1, 10, $6, $7, NULL, 'company', $8),
			($2, 'company call 2', 'processing', 'b.wav', 'b.wav', 'audio/wav', 1, 10, $6, $7, NULL, 'company', $8),
			($3, 'company call 3', 'processing', 'c.wav', 'c.wav', 'audio/wav', 1, 10, $6, $7, NULL, 'company', $8),
			($4, 'company call 4', 'processing', 'd.wav', 'd.wav', 'audio/wav', 1, 10, $6, $7, NULL, 'company', $8),
			($5, 'other call', 'processing', 'e.wav', 'e.wav', 'audio/wav', 1, 10, $6, NULL, NULL, 'personal', $8)
	`, firstCallID, secondCallID, thirdCallID, fourthCallID, otherCallID, managerID, companyID, now)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO processing_jobs (job_uuid, job_type, entity_uuid, status, attempts, max_attempts, available_at, last_error, created_at, updated_at)
		VALUES
			($1, 'transcribe_call', $6, 'pending', 0, 3, $11, NULL, $11, $11),
			($2, 'analyze_call', $7, 'pending', 2, 3, $11, 'temporary', $11, $11),
			($3, 'transcribe_call', $8, 'failed', 3, 3, $11, 'permanent', $11, $12),
			($4, 'analyze_call', $9, 'done', 1, 3, $11, NULL, $11, $13),
			($5, 'transcribe_call', $10, 'failed', 3, 3, $11, 'outside filter', $11, $12)
	`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), firstCallID, secondCallID, thirdCallID, fourthCallID, otherCallID, now, now.Add(time.Minute), now.Add(30*time.Second))
	require.NoError(t, err)

	monitoring, err := repository.GetMonitoring(ctx, models.ProcessingMonitoringInput{
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, 2, monitoring.Queue.Pending)
	require.Equal(t, 1, monitoring.Queue.Done)
	require.Equal(t, 1, monitoring.Queue.Failed)
	require.Equal(t, 1, monitoring.Queue.Retry)
	require.NotNil(t, monitoring.AverageProcessingSeconds)
	require.Equal(t, 30, *monitoring.AverageProcessingSeconds)
	require.Len(t, monitoring.LastFailedJobs, 1)
	require.Equal(t, "permanent", *monitoring.LastFailedJobs[0].LastError)
	require.Equal(t, "ok", monitoring.Services.Transcriber)
	require.Equal(t, "ok", monitoring.Services.Analyzer)
	require.Equal(t, "ok", monitoring.Services.Storage)
}
