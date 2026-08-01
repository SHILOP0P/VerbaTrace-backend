package refresh_session

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) RevokeUserRefreshSession(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, reason string) error {
	query := `
	UPDATE refresh_sessions
	SET revoked_at = COALESCE(revoked_at, now()),
	    revoked_reason = COALESCE(revoked_reason, $3)
	WHERE user_uuid = $1
	  AND session_uuid = $2
	`

	result, err := r.db.ExecContext(ctx, query, userID, sessionID, reason)
	if err != nil {
		return fmt.Errorf("revoke user refresh session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke user refresh session rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return model.ErrRefreshSessionNotFound
	}

	return nil
}
