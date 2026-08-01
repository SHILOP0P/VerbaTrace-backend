package refresh_session

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) CreateRefreshSession(ctx context.Context, session model.RefreshSession) (model.RefreshSession, error) {
	repoSession, err := converter.ModelRefreshSessionToRepoModel(session)
	if err != nil {
		return model.RefreshSession{}, fmt.Errorf("convert refresh session to repo model: %w", err)
	}

	query := `
	INSERT INTO refresh_sessions (
	    session_uuid,
	    user_uuid,
	    refresh_token_hash,
	    access_version,
	    user_agent,
	    ip_address,
	    created_at,
	    last_used_at,
	    expires_at,
	    revoked_at,
	    revoked_reason
	)
	VALUES ($1, $2, $3, $4, $5, $6::INET, $7, $8, $9, $10, $11)
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

	var createdRepoSession repoModel.RefreshSession

	row := r.db.QueryRowContext(ctx, query,
		repoSession.ID,
		repoSession.UserID,
		repoSession.RefreshTokenHash,
		repoSession.AccessVersion,
		repoSession.UserAgent,
		repoSession.IPAddress,
		repoSession.CreatedAt,
		repoSession.LastUsedAt,
		repoSession.ExpiresAt,
		repoSession.RevokedAt,
		repoSession.RevokedReason,
	)

	createdRepoSession, err = scaner.ScanRefreshSession(row)
	if err != nil {
		return model.RefreshSession{}, fmt.Errorf("create refresh session: %w", err)
	}

	return converter.RepoRefreshSessionToModel(createdRepoSession)
}
