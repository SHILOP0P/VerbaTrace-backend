package converter

import (
	"database/sql"

	model "calllens/monolit/internal/models"
	repoModel "calllens/monolit/internal/repository/models"
)

func RepoUserToModel(repoUser repoModel.CurrentUserRecord) (model.CurrentUser, error) {
	return model.CurrentUser{
		ID:              repoUser.ID,
		Email:           repoUser.Email,
		PasswordHash:    repoUser.PasswordHash,
		FullName:        repoUser.FullName,
		FullSurname:     repoUser.FullSurname,
		Username:        repoUser.Username,
		Role:            model.UserRole(repoUser.Role),
		Post:            nullStringToStringPtr(repoUser.Post),
		Phone:           nullStringToStringPtr(repoUser.Phone),
		Timezone:        nullStringToStringPtr(repoUser.Timezone),
		AvatarPath:      nullStringToStringPtr(repoUser.AvatarPath),
		AvatarMime:      nullStringToStringPtr(repoUser.AvatarMime),
		AvatarSize:      nullInt64ToInt64Ptr(repoUser.AvatarSize),
		AvatarUpdatedAt: nullTimeToTimePtr(repoUser.AvatarUpdatedAt),
		CreatedAt:       repoUser.CreatedAt,
	}, nil
}

func ModelUserToRepoModel(user model.CurrentUser) (repoModel.CurrentUserRecord, error) {
	return repoModel.CurrentUserRecord{
		ID:              user.ID,
		Email:           user.Email,
		PasswordHash:    user.PasswordHash,
		FullName:        user.FullName,
		FullSurname:     user.FullSurname,
		Username:        user.Username,
		Role:            string(user.Role),
		Post:            stringPtrToNullString(user.Post),
		Phone:           stringPtrToNullString(user.Phone),
		Timezone:        stringPtrToNullString(user.Timezone),
		AvatarPath:      stringPtrToNullString(user.AvatarPath),
		AvatarMime:      stringPtrToNullString(user.AvatarMime),
		AvatarSize:      int64PtrToNullInt64(user.AvatarSize),
		AvatarUpdatedAt: timePtrToNullTime(user.AvatarUpdatedAt),
		CreatedAt:       user.CreatedAt,
	}, nil
}

func nullStringToStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64ToInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func int64PtrToNullInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func stringPtrToNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{
		String: *value,
		Valid:  true,
	}
}
