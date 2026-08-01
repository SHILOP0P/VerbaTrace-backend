package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanRefreshSession(row rowScanner) (repoModel.RefreshSession, error) {
	var session repoModel.RefreshSession

	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.RefreshTokenHash,
		&session.AccessVersion,
		&session.UserAgent,
		&session.IPAddress,
		&session.CreatedAt,
		&session.LastUsedAt,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.RevokedReason,
	)
	if err != nil {
		return repoModel.RefreshSession{}, err
	}

	return session, nil
}
