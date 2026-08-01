package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) UpdateProfile(ctx context.Context, input model.UpdateUserProfileInput) (model.CurrentUser, error) {
	query := `WITH updated AS (
	    UPDATE user_profiles
	    SET full_name = COALESCE($2, full_name),
	        full_surname = COALESCE($3, full_surname),
	        headline = CASE WHEN $4::text IS NULL THEN headline ELSE NULLIF($4, '') END,
	        phone = CASE WHEN $5::text IS NULL THEN phone ELSE NULLIF($5, '') END,
	        timezone = CASE WHEN $6::text IS NULL THEN timezone ELSE NULLIF($6, '') END,
	        updated_at = now()
	    WHERE user_uuid = $1
	    RETURNING *
	)
	SELECT u.user_uuid, u.email, u.password_hash, p.full_name, p.full_surname,
	       p.username, u.role, p.headline, p.phone, p.timezone, p.avatar_path,
	       p.avatar_mime_type, p.avatar_size_bytes, p.avatar_updated_at, u.created_at
	FROM users u
	JOIN updated p ON p.user_uuid = u.user_uuid`

	row := r.db.QueryRowContext(ctx, query, input.UserUUID, input.FullName, input.FullSurname, input.Post, input.Phone, input.Timezone)

	repoUser, err := scaner.ScanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CurrentUser{}, model.ErrUserNotFound
		}
		return model.CurrentUser{}, fmt.Errorf("update profile: %w", err)
	}

	return converter.RepoUserToModel(repoUser)
}
