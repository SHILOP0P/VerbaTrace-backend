//go:build integration

package analysis

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	instructionRepo "verbatrace/monolit/internal/repository/analysis_instruction"
	callRepo "verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/repositorytest"
	userRepo "verbatrace/monolit/internal/repository/user"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A save that lands between reading the instruction and writing the snapshot
// must not make the snapshot claim the newer version.
func TestSnapshotKeepsTheVersionThatWasRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	call := createAnalysisCall(t, ctx, userRepo.NewUserRepository(db), callRepo.NewRepository(db))
	repository := NewRepository(db)
	instructions := instructionRepo.NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	instruction, err := instructions.Create(ctx, models.AnalysisInstruction{
		ID: uuid.New(), Scope: models.AnalysisInstructionScopePersonal,
		UserUUID: uuid.NullUUID{UUID: call.UploadedByUserUUID.UUID, Valid: true},
		Title:    "Стандарт звонка", OriginalFilename: "standard.md", FilePath: "personal/standard-v1.md",
		MimeType: "text/markdown", SizeBytes: 10, ContentSHA256: "hash-v1", IsActive: true,
		CreatedByUserUUID: call.UploadedByUserUUID.UUID, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	read, err := instructions.ReadableVersion(ctx, instruction)
	require.NoError(t, err)
	require.Nil(t, read.Text)
	require.NoError(t, instructions.SaveVersionText(ctx, read.VersionID, "Выясни бюджет"))
	require.NoError(t, instructions.SaveVersionText(ctx, read.VersionID, "ignored second write"))
	cached, err := instructions.ReadableVersion(ctx, instruction)
	require.NoError(t, err)
	require.Equal(t, read.VersionID, cached.VersionID)
	require.NotNil(t, cached.Text)
	require.Equal(t, "Выясни бюджет", *cached.Text)

	// Another save replaces the file after the analysis has read version 1.
	_, err = db.ExecContext(ctx, `UPDATE analysis_instructions SET file_path='personal/standard-v2.md', content_sha256='hash-v2', updated_at=now() WHERE instruction_uuid=$1`, instruction.ID)
	require.NoError(t, err)

	analysis, err := repository.Create(ctx, models.CallAnalysis{ID: uuid.New(), CallUUID: call.ID, Status: models.CallAnalysisStatusPending, Provider: "mock", CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	require.NoError(t, repository.SaveInstructionSnapshots(ctx, analysis.ID, []models.AnalysisInstructionContent{{
		ID: instruction.ID, Scope: instruction.Scope, Title: instruction.Title, Content: "Выясни бюджет",
		ContentSHA256: "hash-v1", VersionID: read.VersionID,
	}}))

	snapshots, err := repository.ListInstructionSnapshots(ctx, analysis.ID)
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, read.VersionID, snapshots[0].VersionUUID)
	require.Equal(t, 1, snapshots[0].Version)
}
