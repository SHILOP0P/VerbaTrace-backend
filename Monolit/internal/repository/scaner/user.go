package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanUser(row rowScanner) (repoModel.CurrentUserRecord, error) {
	var user repoModel.CurrentUserRecord

	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.FullSurname,
		&user.Username,
		&user.Role,
		&user.Post,
		&user.Phone,
		&user.Timezone,
		&user.AvatarPath,
		&user.AvatarMime,
		&user.AvatarSize,
		&user.AvatarUpdatedAt,
		&user.CreatedAt,
	)
	if err != nil {
		return repoModel.CurrentUserRecord{}, err
	}

	return user, nil
}
