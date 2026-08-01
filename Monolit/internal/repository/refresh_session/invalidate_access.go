package refresh_session

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) InvalidateSessionAccess(ctx context.Context, sessionID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
	UPDATE refresh_sessions
	SET access_version = access_version + 1
	WHERE session_uuid = $1
	  AND revoked_at IS NULL
	  AND expires_at > now()
	`, sessionID)
	if err != nil {
		return fmt.Errorf("invalidate session access: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("invalidate session access rows affected: %w", err)
	}
	if rows == 0 {
		return models.ErrRefreshSessionNotFound
	}

	return nil
}

func (r *Repository) InvalidateAllUserAccess(ctx context.Context, userID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
	UPDATE refresh_sessions
	SET access_version = access_version + 1
	WHERE user_uuid = $1
	  AND revoked_at IS NULL
	  AND expires_at > now()
	`, userID)
	if err != nil {
		return fmt.Errorf("invalidate all user access: %w", err)
	}

	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("invalidate all user access rows affected: %w", err)
	}

	return nil
}
