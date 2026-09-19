//go:build integration

package retention

import (
	"context"
	"database/sql"
	"io"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	instructionRepo "verbatrace/monolit/internal/repository/analysis_instruction"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recordingInstructionStorage struct{ deleted []string }

func (s *recordingInstructionStorage) Save(context.Context, models.SaveInstructionInput) (models.SavedInstructionFile, error) {
	return models.SavedInstructionFile{}, nil
}

func (s *recordingInstructionStorage) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}

func (s *recordingInstructionStorage) Delete(_ context.Context, path string) error {
	s.deleted = append(s.deleted, path)
	return nil
}

func TestSweepKeepsOnlyVersionsSomethingDependsOn(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	userID := repositorytest.CreateUser(t, db)

	now := time.Now().UTC()
	instruction, err := instructionRepo.NewRepository(db).Create(ctx, models.AnalysisInstruction{
		ID: uuid.New(), Scope: models.AnalysisInstructionScopePersonal, UserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		Title: "Стандарт", OriginalFilename: "rubric.md", FilePath: "p/a.md", MimeType: "text/markdown",
		SizeBytes: 1, ContentSHA256: "a", IsActive: true, CreatedByUserUUID: userID, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	// v2 and v3 share a file: a rename makes a version and keeps the file.
	exec(`UPDATE analysis_instructions SET file_path = 'p/b.md', content_sha256 = 'b' WHERE instruction_uuid = $1`, instruction.ID)
	exec(`UPDATE analysis_instructions SET title = 'Стандарт продаж' WHERE instruction_uuid = $1`, instruction.ID)
	exec(`UPDATE analysis_instructions SET file_path = 'p/c.md', content_sha256 = 'c' WHERE instruction_uuid = $1`, instruction.ID)
	exec(`UPDATE analysis_instructions SET file_path = 'p/d.md', content_sha256 = 'd' WHERE instruction_uuid = $1`, instruction.ID)
	version := map[int]uuid.UUID{}
	rows, err := db.Query(`SELECT version, instruction_version_uuid FROM analysis_instruction_versions WHERE instruction_uuid = $1`, instruction.ID)
	require.NoError(t, err)
	for rows.Next() {
		var n int
		var id uuid.UUID
		require.NoError(t, rows.Scan(&n, &id))
		version[n] = id
	}
	require.NoError(t, rows.Close())
	require.Len(t, version, 5)

	exec(`UPDATE analysis_instruction_versions SET superseded_at = now() - interval '7 hours' WHERE superseded_at IS NOT NULL`)
	// v1 was used by a call.
	callID, analysisID := uuid.New(), uuid.New()
	exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, visibility_scope, created_at)
		VALUES ($1, 'call', 'analyzed', 'a.mp3', 'a.mp3', 'audio/mpeg', 1, $2, 'personal', now())`, callID, userID)
	exec(`INSERT INTO call_analyses (analysis_uuid, call_uuid, status, provider, result_json, result_text, created_at, updated_at)
		VALUES ($1, $2, 'done', 'test', '{}'::jsonb, '', now(), now())`, analysisID, callID)
	exec(`INSERT INTO call_analysis_instruction_snapshots (analysis_uuid, instruction_version_uuid, instruction_uuid, position, selection_source, title_snapshot, scope_snapshot, content_sha256, content_snapshot)
		VALUES ($1, $2, $3, 0, 'explicit', 'Стандарт', 'personal', 'a', 'text')`, analysisID, version[1], instruction.ID)
	// v2 was replaced too recently.
	exec(`UPDATE analysis_instruction_versions SET superseded_at = now() - interval '1 hour' WHERE instruction_version_uuid = $1`, version[2])
	// v4's scorecard is in force while v5's waits for confirmation. An older
	// hand-edited revision of it that no call used can go.
	oldRevision := uuid.New()
	exec(`INSERT INTO instruction_scorecards (scorecard_uuid, instruction_uuid, instruction_version_uuid, revision, status, origin, content_sha256, superseded_at)
		VALUES ($1, $2, $3, 1, 'ready', 'compiled', 'c', now() - interval '7 hours')`, oldRevision, instruction.ID, version[4])
	exec(`INSERT INTO instruction_scorecards (scorecard_uuid, instruction_uuid, instruction_version_uuid, revision, status, origin, content_sha256, is_current)
		VALUES ($1, $2, $3, 2, 'ready', 'edited', 'c', true)`, uuid.New(), instruction.ID, version[4])

	files := &recordingInstructionStorage{}
	NewInstructionWorker(NewService(db, nil, nil, files, nil), time.Hour, 10).RunOnce(ctx)

	remaining := map[uuid.UUID]bool{}
	rows, err = db.Query(`SELECT instruction_version_uuid FROM analysis_instruction_versions WHERE instruction_uuid = $1`, instruction.ID)
	require.NoError(t, err)
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		remaining[id] = true
	}
	require.NoError(t, rows.Close())
	require.Equal(t, map[uuid.UUID]bool{version[1]: true, version[2]: true, version[4]: true, version[5]: true}, remaining, "only the unused v3 goes")
	require.Empty(t, files.deleted, "v3's file still belongs to v2")

	var revisions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM instruction_scorecards WHERE scorecard_uuid = $1`, oldRevision).Scan(&revisions))
	require.Zero(t, revisions)

	var selected sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT item_count FROM retention_audit_events WHERE event_type = 'instruction_versions_selected_for_sweep'`).Scan(&selected))
	require.EqualValues(t, 1, selected.Int64)
	var swept int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM retention_audit_events WHERE event_type = 'instruction_version_swept' AND entity_uuid = $1`, version[3]).Scan(&swept))
	require.Equal(t, 1, swept)

	// Once v2 is past its grace period it goes with the file it shared.
	exec(`UPDATE analysis_instruction_versions SET superseded_at = now() - interval '7 hours' WHERE instruction_version_uuid = $1`, version[2])
	NewInstructionWorker(NewService(db, nil, nil, files, nil), time.Hour, 10).RunOnce(ctx)
	require.Equal(t, []string{"p/b.md"}, files.deleted)
}
