package converter

import (
	"database/sql"
	"time"

	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoRefreshSessionToModel(repoSession repoModel.RefreshSession) (model.RefreshSession, error) {
	return model.RefreshSession{
		ID:               repoSession.ID,
		UserID:           repoSession.UserID,
		RefreshTokenHash: repoSession.RefreshTokenHash,
		AccessVersion:    repoSession.AccessVersion,
		UserAgent:        nullStringToStringPtr(repoSession.UserAgent),
		IPAddress:        nullStringToStringPtr(repoSession.IPAddress),
		CreatedAt:        repoSession.CreatedAt,
		LastUsedAt:       nullTimeToTimePtr(repoSession.LastUsedAt),
		ExpiresAt:        repoSession.ExpiresAt,
		RevokedAt:        nullTimeToTimePtr(repoSession.RevokedAt),
		RevokedReason:    nullStringToStringPtr(repoSession.RevokedReason),
	}, nil
}

func ModelRefreshSessionToRepoModel(session model.RefreshSession) (repoModel.RefreshSession, error) {
	return repoModel.RefreshSession{
		ID:               session.ID,
		UserID:           session.UserID,
		RefreshTokenHash: session.RefreshTokenHash,
		AccessVersion:    session.AccessVersion,
		UserAgent:        stringPtrToNullString(session.UserAgent),
		IPAddress:        stringPtrToNullString(session.IPAddress),
		CreatedAt:        session.CreatedAt,
		LastUsedAt:       timePtrToNullTime(session.LastUsedAt),
		ExpiresAt:        session.ExpiresAt,
		RevokedAt:        timePtrToNullTime(session.RevokedAt),
		RevokedReason:    stringPtrToNullString(session.RevokedReason),
	}, nil
}

func nullTimeToTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}

	return &value.Time
}

func timePtrToNullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}

	return sql.NullTime{
		Time:  *value,
		Valid: true,
	}
}
