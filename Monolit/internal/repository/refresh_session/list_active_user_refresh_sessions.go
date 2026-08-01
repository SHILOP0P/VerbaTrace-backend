package refresh_session

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) ListActiveUserRefreshSessions(ctx context.Context, userID uuid.UUID) ([]model.RefreshSession, error) {
	query := `
	SELECT session_uuid,
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
	FROM refresh_sessions
	WHERE user_uuid = $1
	  AND revoked_at IS NULL
	  AND expires_at > now()
	ORDER BY COALESCE(last_used_at, created_at) DESC,
	         created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list active user refresh sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []model.RefreshSession
	for rows.Next() {
		repoSession, err := scaner.ScanRefreshSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan active user refresh session: %w", err)
		}

		session, err := converter.RepoRefreshSessionToModel(repoSession)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active user refresh sessions: %w", err)
	}

	return sessions, nil
}
