package refresh_session

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) RevokeRefreshSession(ctx context.Context, sessionID uuid.UUID, reason string) error {
	query := `
	UPDATE refresh_sessions
	SET revoked_at = COALESCE(revoked_at, now()),
	    revoked_reason = COALESCE(revoked_reason, $2)
	WHERE session_uuid = $1
	`

	result, err := r.db.ExecContext(ctx, query, sessionID, reason)
	if err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke refresh session rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return model.ErrRefreshSessionNotFound
	}

	return nil
}
