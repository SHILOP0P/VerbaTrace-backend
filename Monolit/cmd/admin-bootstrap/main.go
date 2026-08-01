package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"verbatrace/monolit/internal/config"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	email := flag.String("email", "", "email of the existing user to promote")
	flag.Parse()

	if strings.TrimSpace(*email) == "" {
		fmt.Fprintln(os.Stderr, "--email is required")
		os.Exit(2)
	}
	if err := config.Load("./.env"); err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", config.AppConfig().Postgres.URI())
	if err != nil {
		fmt.Fprintf(os.Stderr, "open postgres: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	userID, changed, err := bootstrapSuperAdmin(context.Background(), db, *email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap superadmin: %v\n", err)
		os.Exit(1)
	}

	if changed {
		fmt.Printf("promoted %s to superadmin; active access tokens were invalidated\n", userID)
		return
	}
	fmt.Printf("%s is already the superadmin\n", userID)
}

func bootstrapSuperAdmin(ctx context.Context, db *sql.DB, email string) (uuid.UUID, bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var userID uuid.UUID
	var role string
	err = tx.QueryRowContext(ctx, `
		SELECT user_uuid, role
		FROM users
		WHERE lower(email) = lower($1)
		FOR UPDATE
	`, strings.TrimSpace(email)).Scan(&userID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, fmt.Errorf("user not found")
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("find user: %w", err)
	}

	if role == "superadmin" {
		if err := tx.Commit(); err != nil {
			return uuid.Nil, false, fmt.Errorf("commit transaction: %w", err)
		}
		return userID, false, nil
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = 'superadmin' WHERE user_uuid = $1`, userID); err != nil {
		return uuid.Nil, false, fmt.Errorf("promote user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE refresh_sessions
		SET access_version = access_version + 1
		WHERE user_uuid = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`, userID); err != nil {
		return uuid.Nil, false, fmt.Errorf("invalidate access tokens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return uuid.Nil, false, fmt.Errorf("commit transaction: %w", err)
	}
	return userID, true, nil
}
