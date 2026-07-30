package user

import (
	"context"
	"fmt"

	model "calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	repoModel "calllens/monolit/internal/repository/models"
	"calllens/monolit/internal/repository/scaner"
)

func (r *Repository) CreateUser(ctx context.Context, user model.CurrentUser) (model.CurrentUser, error) {
	repoUser, err := converter.ModelUserToRepoModel(user)
	if err != nil {
		return model.CurrentUser{}, fmt.Errorf("convert model to repo model: %w", err)
	}

	query := `
	WITH created_account AS (
	    INSERT INTO users (user_uuid, email, password_hash, role, created_at)
	    VALUES ($1, $2, $3, $4, $5)
	    RETURNING user_uuid, email, password_hash, role, created_at
	),
	created_profile AS (
	    INSERT INTO user_profiles (
	        user_uuid, full_name, full_surname, username, headline, phone, timezone,
	        avatar_path, avatar_mime_type, avatar_size_bytes, avatar_updated_at
	    )
	    VALUES ($1, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	    RETURNING *
	)
	SELECT a.user_uuid,
	       a.email,
	       a.password_hash,
	       p.full_name,
	       p.full_surname,
	       p.username,
	       a.role,
	       p.headline,
	       p.phone,
	       p.timezone,
	       p.avatar_path,
	       p.avatar_mime_type,
	       p.avatar_size_bytes,
	       p.avatar_updated_at,
	       a.created_at
	FROM created_account a
	JOIN created_profile p ON p.user_uuid = a.user_uuid
	`
	var createdRepoUser repoModel.CurrentUserRecord

	row := r.db.QueryRowContext(ctx, query,
		repoUser.ID,
		repoUser.Email,
		repoUser.PasswordHash,
		repoUser.Role,
		repoUser.CreatedAt,
		repoUser.FullName,
		repoUser.FullSurname,
		repoUser.Username,
		repoUser.Post,
		repoUser.Phone,
		repoUser.Timezone,
		repoUser.AvatarPath,
		repoUser.AvatarMime,
		repoUser.AvatarSize,
		repoUser.AvatarUpdatedAt,
	)

	createdRepoUser, err = scaner.ScanUser(row)
	if err != nil {
		return model.CurrentUser{}, fmt.Errorf("create user: %w", normalizeUserError(err))
	}

	return converter.RepoUserToModel(createdRepoUser)
}
