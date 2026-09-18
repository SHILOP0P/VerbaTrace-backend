//go:build integration

package passwordreset

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"verbatrace/monolit/internal/auth/password"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"
	userRepo "verbatrace/monolit/internal/repository/user"
	"verbatrace/monolit/internal/service/delivery"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var tokenInLink = regexp.MustCompile(`token=([A-Za-z0-9_\-%]+)`)

func lastToken(t *testing.T, db *sql.DB, user uuid.UUID) string {
	t.Helper()
	var text string
	require.NoError(t, db.QueryRow(`SELECT payload_json->>'text' FROM outbound_messages WHERE user_uuid = $1 AND kind = 'password_reset' ORDER BY created_at DESC LIMIT 1`, user).Scan(&text))
	match := tokenInLink.FindStringSubmatch(text)
	require.NotNil(t, match, "the letter carries the link")
	return match[1]
}

func TestAResetLinkWorksOnceAndEndsEverySession(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	user := repositorytest.CreateUser(t, db)
	var email string
	require.NoError(t, db.QueryRow(`SELECT email FROM users WHERE user_uuid = $1`, user).Scan(&email))
	_, err := db.Exec(`INSERT INTO refresh_sessions (session_uuid, user_uuid, refresh_token_hash, expires_at) VALUES ($1, $2, 'h1', now() + interval '1 day')`, uuid.New(), user)
	require.NoError(t, err)

	service := NewService(db, userRepo.NewUserRepository(db), "pepper", "https://app.example", false, nil)
	require.False(t, service.Enabled(), "hidden while letters only reach the mock sender")

	service.Request(ctx, "nobody@example.com", "10.0.0.1")
	var messages int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbound_messages`).Scan(&messages))
	require.Zero(t, messages, "an unknown address gets nothing, and the caller learns nothing")

	service.Request(ctx, "  "+email+" ", "10.0.0.1")
	first := lastToken(t, db, user)
	var address string
	require.NoError(t, db.QueryRow(`SELECT payload_json->>'address' FROM outbound_messages WHERE kind = 'password_reset'`).Scan(&address))
	require.Equal(t, email, address)
	service.Request(ctx, email, "10.0.0.1")
	second := lastToken(t, db, user)
	require.NotEqual(t, first, second)
	require.ErrorIs(t, service.Confirm(ctx, first, "new-password-1"), ErrInvalidToken, "a new request voids the old link")

	require.NoError(t, service.Confirm(ctx, second, "new-password-1"))
	var hash string
	require.NoError(t, db.QueryRow(`SELECT password_hash FROM users WHERE user_uuid = $1`, user).Scan(&hash))
	require.NoError(t, password.Compare("new-password-1", hash, "pepper"))
	require.ErrorIs(t, service.Confirm(ctx, second, "new-password-2"), ErrInvalidToken, "the link works once")
	var active, notified int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM refresh_sessions WHERE user_uuid = $1 AND revoked_at IS NULL`, user).Scan(&active))
	require.Zero(t, active, "every session ends")
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM notifications WHERE user_uuid = $1 AND type = 'password_changed'`, user).Scan(&notified))
	require.Equal(t, 1, notified)

	// The letter's payload is wiped once it is sent.
	require.Equal(t, 2, delivery.NewWorker(db, delivery.NewMockSender(nil), nil, time.Second, 10).RunOnce(ctx))
	var left int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbound_messages WHERE payload_json ? 'text'`).Scan(&left))
	require.Zero(t, left)

	// An expired link is refused; a third request in the hour is not sent.
	service.Request(ctx, email, "10.0.0.2")
	expired := lastToken(t, db, user)
	_, err = db.Exec(`UPDATE password_reset_tokens SET expires_at = now() - interval '1 minute' WHERE used_at IS NULL`)
	require.NoError(t, err)
	require.ErrorIs(t, service.Confirm(ctx, expired, "new-password-3"), ErrInvalidToken)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbound_messages WHERE kind = 'password_reset'`).Scan(&messages))
	service.Request(ctx, email, "10.0.0.3")
	var after int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbound_messages WHERE kind = 'password_reset'`).Scan(&after))
	require.Equal(t, messages, after, "three requests an hour per address")
	require.ErrorIs(t, service.Confirm(ctx, "any", "short"), models.ErrInvalidUserInput, "eight characters at least")
}
