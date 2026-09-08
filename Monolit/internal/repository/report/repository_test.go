//go:build integration

package report

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	transcriptionRepo "verbatrace/monolit/internal/repository/transcription"

	"verbatrace/monolit/internal/models"
	analysisRepo "verbatrace/monolit/internal/repository/analysis"
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
	_, err := users.CreateUser(ctx, models.CurrentUser{
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

func TestTranscriptionReportsPersistModeAndSeparateVersions(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	calls := callRepo.NewRepository(db)
	userID, existing, _ := createReportDependencies(t, ctx, userRepo.NewUserRepository(db), calls, analysisRepo.NewRepository(db))
	original := existing
	original.ID = uuid.New()
	original.Status = models.CallStatusTranscribed
	original.TranscriptionOnly = true
	created, err := calls.CreateCall(ctx, original)
	require.NoError(t, err)
	require.True(t, created.TranscriptionOnly)
	loaded, err := calls.GetByUUIDForProcessing(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, loaded.TranscriptionOnly)
	text := "Первоначальный текст"
	transcripts := transcriptionRepo.NewRepository(db)
	transcript, err := transcripts.Create(ctx, models.Transcription{ID: uuid.New(), CallUUID: created.ID, Status: models.TranscriptionStatusTranscribed, Text: &text, Provider: "test", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	require.NoError(t, err)
	legacy, revision, err := transcripts.GetReportTranscription(ctx, created.ID, 0)
	require.NoError(t, err)
	require.Equal(t, 1, revision)
	require.Equal(t, text, *legacy.Text)
	for number := 1; number <= 2; number++ {
		payload, _ := json.Marshal(map[string]any{"text": []string{"", "Версия один", "Версия два"}[number], "segments": []any{}, "words": []any{}})
		hash := sha256.Sum256(payload)
		contentID := uuid.New()
		_, err = db.ExecContext(ctx, `INSERT INTO call_transcription_contents(transcription_content_uuid,transcription_uuid,content_sha256,canonical_size_bytes,payload,created_at) VALUES($1,$2,$3,$4,$5,now())`, contentID, transcript.ID, hash[:], len(payload), payload)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO call_transcription_revisions(transcription_revision_uuid,transcription_uuid,transcription_content_uuid,revision,reason,changed_word_indexes,created_at) VALUES($1,$2,$3,$4,'test','[]',now())`, uuid.New(), transcript.ID, contentID, number)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO call_transcription_revision_state(transcription_uuid,active_revision,updated_at) VALUES($1,2,now())`, transcript.ID)
	require.NoError(t, err)
	active, revision, err := transcripts.GetReportTranscription(ctx, created.ID, 0)
	require.NoError(t, err)
	require.Equal(t, 2, revision)
	require.Equal(t, "Версия два", *active.Text)
	first, revision, err := transcripts.GetReportTranscription(ctx, created.ID, 1)
	require.NoError(t, err)
	require.Equal(t, 1, revision)
	require.Equal(t, "Версия один", *first.Text)
	_, _, err = transcripts.GetReportTranscription(ctx, created.ID, 99)
	require.ErrorIs(t, err, models.ErrTranscriptionNotFound)
	reports := NewRepository(db)
	in := models.ReportExport{ID: uuid.New(), CallUUID: created.ID, RequestedByUserUUID: userID, Format: models.ReportFormatMD, Content: "transcription", TranscriptionRevision: 1, Status: models.ReportStatusPending, FileName: "transcript.md", ContentType: "text/markdown", CreatedAt: time.Now(), UpdatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	saved, err := reports.Create(ctx, in)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, saved.AnalysisUUID)
	in.ID = uuid.New()
	_, err = reports.Create(ctx, in)
	require.ErrorIs(t, err, models.ErrReportAlreadyExists)
	in.TranscriptionRevision = 2
	_, err = reports.Create(ctx, in)
	require.NoError(t, err)
	list, err := reports.ListByCallUUID(ctx, created.ID, time.Now())
	require.NoError(t, err)
	require.Len(t, list, 2)
}
