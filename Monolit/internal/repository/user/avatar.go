package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) UpdateAvatar(ctx context.Context, input model.UserAvatarUpdate) (model.CurrentUser, error) {
	query := `WITH updated AS (
	UPDATE user_profiles
	SET avatar_path = $2,
	    avatar_mime_type = $3,
	    avatar_size_bytes = $4,
	    avatar_updated_at = $5,
	    updated_at = now()
	WHERE user_uuid = $1
	RETURNING *
	)
	SELECT u.user_uuid, u.email, u.password_hash, p.full_name, p.full_surname,
	       p.username, u.role, p.headline, p.phone, p.timezone, p.avatar_path,
	       p.avatar_mime_type, p.avatar_size_bytes, p.avatar_updated_at, u.created_at
	FROM users u
	JOIN updated p ON p.user_uuid = u.user_uuid`

	row := r.db.QueryRowContext(ctx, query, input.UserUUID, input.Path, input.MimeType, input.SizeBytes, input.UpdatedAt)
	return scanAvatarUser(row, "update avatar")
}

func (r *Repository) DeleteAvatar(ctx context.Context, userID uuid.UUID) (model.CurrentUser, error) {
	query := `WITH updated AS (
	UPDATE user_profiles
	SET avatar_path = NULL,
	    avatar_mime_type = NULL,
	    avatar_size_bytes = NULL,
	    avatar_updated_at = NULL,
	    updated_at = now()
	WHERE user_uuid = $1
	RETURNING *
	)
	SELECT u.user_uuid, u.email, u.password_hash, p.full_name, p.full_surname,
	       p.username, u.role, p.headline, p.phone, p.timezone, p.avatar_path,
	       p.avatar_mime_type, p.avatar_size_bytes, p.avatar_updated_at, u.created_at
	FROM users u
	JOIN updated p ON p.user_uuid = u.user_uuid`

	row := r.db.QueryRowContext(ctx, query, userID)
	return scanAvatarUser(row, "delete avatar")
}

func scanAvatarUser(row interface{ Scan(dest ...any) error }, operation string) (model.CurrentUser, error) {
	repoUser, err := scaner.ScanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CurrentUser{}, model.ErrUserNotFound
		}
		return model.CurrentUser{}, fmt.Errorf("%s: %w", operation, err)
	}

	return converter.RepoUserToModel(repoUser)
}
