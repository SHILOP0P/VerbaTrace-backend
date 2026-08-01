package repositorytest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

const defaultTestDatabase = "verbatrace_test"

func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()

	loadEnv(t)

	cfg := postgresConfigFromEnv()
	if cfg.testDatabase == "" {
		cfg.testDatabase = defaultTestDatabase
	}
	cfg.testDatabase = packageTestDatabaseName(t, cfg.testDatabase)

	maintenanceDB := openDB(t, cfg.withDatabase("postgres"))
	defer func() { _ = maintenanceDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := maintenanceDB.PingContext(ctx); err != nil {
		t.Skipf("postgres is not available for integration tests: %v", err)
	}

	createDatabaseIfNotExists(t, maintenanceDB, cfg.testDatabase)

	db := openDB(t, cfg.withDatabase(cfg.testDatabase))
	require.NoError(t, db.PingContext(ctx))

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func RunMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	migrationDir := migrationDirectory(t)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(db, migrationDir))
}

func TruncateTables(t *testing.T, db *sql.DB) {
	t.Helper()

	query := `
	TRUNCATE TABLE
	    admin_audit_logs,
	    notifications,
	    usage_counters,
	    subscriptions,
	    membership_invitations,
	    processing_jobs,
	    call_transcriptions,
	    refresh_sessions,
	    calls,
	    department_members,
	    departments,
	    company_members,
	    companies,
	    users
	RESTART IDENTITY CASCADE
	`

	_, err := db.ExecContext(context.Background(), query)
	require.NoError(t, err)
}

type postgresTestConfig struct {
	host         string
	port         string
	user         string
	password     string
	sslMode      string
	testDatabase string
}

func postgresConfigFromEnv() postgresTestConfig {
	return postgresTestConfig{
		host:         getenvDefault("POSTGRES_HOST", "localhost"),
		port:         getenvDefault("POSTGRES_PORT", "5432"),
		user:         getenvDefault("POSTGRES_USER", "verbatrace"),
		password:     getenvDefault("POSTGRES_PASSWORD", "change-me"),
		sslMode:      getenvDefault("POSTGRES_SSL_MODE", "disable"),
		testDatabase: os.Getenv("POSTGRES_TEST_DB"),
	}
}

func (cfg postgresTestConfig) withDatabase(database string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.user,
		cfg.password,
		cfg.host,
		cfg.port,
		database,
		cfg.sslMode,
	)
}

func openDB(t *testing.T, uri string) *sql.DB {
	t.Helper()

	pgxConfig, err := pgx.ParseConfig(uri)
	require.NoError(t, err)

	return stdlib.OpenDB(*pgxConfig)
}

func createDatabaseIfNotExists(t *testing.T, db *sql.DB, database string) {
	t.Helper()

	require.True(t, validDatabaseName(database), "invalid test database name: %q", database)

	var exists bool
	err := db.QueryRowContext(
		context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`,
		database,
	).Scan(&exists)
	require.NoError(t, err)

	if exists {
		return
	}

	_, err = db.ExecContext(context.Background(), `CREATE DATABASE "`+database+`"`)
	require.NoError(t, err)
}

func migrationDirectory(t *testing.T) string {
	t.Helper()

	migrationDir := getenvDefault("MIGRATION_DIRECTORY", "./migrations")
	if filepath.IsAbs(migrationDir) {
		return migrationDir
	}

	root := projectRoot(t)
	return filepath.Join(root, migrationDir)
}

func projectRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "failed to find project root")
		dir = parent
	}
}

func loadEnv(t *testing.T) {
	t.Helper()

	root := projectRoot(t)
	_ = godotenv.Load(filepath.Join(root, ".env"))
}

func getenvDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func validDatabaseName(database string) bool {
	return regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(database)
}

func packageTestDatabaseName(t *testing.T, base string) string {
	t.Helper()

	root := projectRoot(t)
	wd, err := os.Getwd()
	require.NoError(t, err)

	rel, err := filepath.Rel(root, wd)
	require.NoError(t, err)

	suffix := regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(rel, "_")
	suffix = strings.Trim(suffix, "_")
	if suffix == "" || suffix == "." {
		return base
	}

	name := base + "_" + strings.ToLower(suffix)
	if len(name) > 63 {
		name = name[:63]
	}

	return strings.TrimRight(name, "_")
}
