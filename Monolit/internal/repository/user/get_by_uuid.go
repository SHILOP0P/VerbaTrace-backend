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

func (r *Repository) GetUserByUUID(ctx context.Context, id uuid.UUID) (model.CurrentUser, error) {
	query := `
	SELECT u.user_uuid,
	       u.email,
	       u.password_hash,
	       p.full_name,
	       p.full_surname,
	       p.username,
	       u.role,
	       p.headline,
	       p.phone,
	       p.timezone,
	       p.avatar_path,
	       p.avatar_mime_type,
	       p.avatar_size_bytes,
	       p.avatar_updated_at,
	       u.created_at
	FROM users u
	JOIN user_profiles p ON p.user_uuid = u.user_uuid
	WHERE u.user_uuid = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)

	repoUser, err := scaner.ScanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CurrentUser{}, model.ErrUserNotFound
		}
		return model.CurrentUser{}, fmt.Errorf("get user by uuid: %w", err)
	}

	return converter.RepoUserToModel(repoUser)
}
