package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	"calllens/monolit/internal/repository/scaner"
)

func (r *Repository) UpdateUsername(ctx context.Context, input model.UpdateUsernameInput) (model.CurrentUser, error) {
	query := `WITH updated AS (
	    UPDATE user_profiles
	    SET username = $2, updated_at = now()
	    WHERE user_uuid = $1
	    RETURNING *
	)
	SELECT u.user_uuid, u.email, u.password_hash, p.full_name, p.full_surname,
	       p.username, u.role, p.headline, p.phone, p.timezone, p.avatar_path,
	       p.avatar_mime_type, p.avatar_size_bytes, p.avatar_updated_at, u.created_at
	FROM users u
	JOIN updated p ON p.user_uuid = u.user_uuid`

	row := r.db.QueryRowContext(ctx, query, input.UserUUID, input.Username)

	repoUser, err := scaner.ScanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CurrentUser{}, model.ErrUserNotFound
		}
		return model.CurrentUser{}, fmt.Errorf("update username: %w", normalizeUserError(err))
	}

	return converter.RepoUserToModel(repoUser)
}
