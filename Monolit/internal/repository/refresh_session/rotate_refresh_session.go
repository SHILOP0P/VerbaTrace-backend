package refresh_session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) RotateRefreshSession(ctx context.Context, oldRefreshTokenHash string, newRefreshTokenHash string, expiresAt time.Time) (model.RefreshSession, error) {
	query := `
	UPDATE refresh_sessions
	SET previous_refresh_token_hash = refresh_token_hash,
	    refresh_token_hash = $2,
	    rotated_at = now(),
	    last_used_at = now(),
	    expires_at = $3
	WHERE refresh_token_hash = $1
	  AND revoked_at IS NULL
	  AND expires_at > now()
	RETURNING session_uuid,
	          user_uuid,
	          refresh_token_hash,
	          access_version,
	          user_agent,
	          ip_address::TEXT,
	          created_at,
	          last_used_at,
	          expires_at,
	          revoked_at,
	          revoked_reason
	`

	row := r.db.QueryRowContext(ctx, query, oldRefreshTokenHash, newRefreshTokenHash, expiresAt)

	repoSession, err := scaner.ScanRefreshSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.RefreshSession{}, r.classifyPreviousToken(ctx, oldRefreshTokenHash)
		}

		return model.RefreshSession{}, fmt.Errorf("rotate refresh session: %w", err)
	}

	return converter.RepoRefreshSessionToModel(repoSession)
}

const refreshRotationGracePeriod = 10 * time.Second

func (r *Repository) classifyPreviousToken(ctx context.Context, refreshTokenHash string) error {
	var rotatedAt time.Time
	var sessionID string
	err := r.db.QueryRowContext(ctx, `
		SELECT session_uuid::TEXT, rotated_at
		FROM refresh_sessions
		WHERE previous_refresh_token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`, refreshTokenHash).Scan(&sessionID, &rotatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrRefreshSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("classify previous refresh token: %w", err)
	}

	if time.Since(rotatedAt) <= refreshRotationGracePeriod {
		return model.ErrRefreshRotationConflict
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE refresh_sessions
		SET revoked_at = now(),
		    revoked_reason = 'refresh_token_reuse',
		    access_version = access_version + 1
		WHERE session_uuid = $1
		  AND previous_refresh_token_hash = $2
		  AND revoked_at IS NULL
	`, sessionID, refreshTokenHash)
	if err != nil {
		return fmt.Errorf("revoke reused refresh token: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return fmt.Errorf("count revoked refresh session: %w", err)
		}
		return model.ErrRefreshSessionNotFound
	}

	return model.ErrRefreshTokenReuse
}
