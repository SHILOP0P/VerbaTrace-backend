//go:build integration

package migrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestMigratorUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	tableName := "goose_db_version_migrator_test"
	goose.SetTableName(tableName)
	t.Cleanup(func() {
		goose.SetTableName("goose_db_version")
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+tableName)
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS migrator_test_records`)
	})

	dir := t.TempDir()
	migration := `-- +goose Up
CREATE TABLE migrator_test_records (id integer PRIMARY KEY);
-- +goose Down
DROP TABLE migrator_test_records;
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00001_create_records.sql"), []byte(migration), 0o600))

	require.NoError(t, NewMigrator(db, dir).Up())

	var exists bool
	require.NoError(t, db.QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'migrator_test_records'
		)
	`).Scan(&exists))
	require.True(t, exists)
}

func TestMigratorUpReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	db := repositorytest.OpenTestDB(t)
	err := NewMigrator(db, filepath.Join(t.TempDir(), "missing")).Up()
	require.Error(t, err)
}
