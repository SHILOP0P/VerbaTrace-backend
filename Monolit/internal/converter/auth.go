package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

const avatarURL = "/api/v1/auth/me/avatar"

func UserModelToAPI(user models.CurrentUser) (dto.UserResponse, error) {
	var userAvatarURL *string
	if user.AvatarPath != nil {
		value := avatarURL
		userAvatarURL = &value
	}

	return dto.UserResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		FullName:    user.FullName,
		FullSurname: user.FullSurname,
		Username:    user.Username,
		Role:        string(user.Role),
		Headline:    user.Post,
		Phone:       user.Phone,
		Timezone:    user.Timezone,
		AvatarURL:   userAvatarURL,
		CreatedAt:   user.CreatedAt.Format(time.RFC3339),
	}, nil
}

func PreferencesModelToAPI(preferences models.UserPreferences) dto.UserPreferencesResponse {
	var activeCompanyUUID *string
	if preferences.ActiveCompanyUUID.Valid {
		value := preferences.ActiveCompanyUUID.UUID.String()
		activeCompanyUUID = &value
	}

	return dto.UserPreferencesResponse{
		ActiveCompanyUUID: activeCompanyUUID,
		Theme:             preferences.Theme,
		DateRange: dto.PreferencesDateRange{
			From: preferences.DateRange.From,
			To:   preferences.DateRange.To,
		},
		InvitationsMuted: preferences.InvitationsMuted,
	}
}
