package auth

import (
	"strings"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	repositoryMocks "calllens/monolit/internal/repository/mocks"
	storageMocks "calllens/monolit/internal/storage/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUpdateProfilePartial(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	name := " Dmitry "
	timezone := "Europe/Moscow"

	s.userRepository.On("UpdateProfile", s.ctx, mock.MatchedBy(func(input models.UpdateUserProfileInput) bool {
		return input.UserUUID == userID &&
			input.FullName != nil && *input.FullName == "Dmitry" &&
			input.FullSurname == nil &&
			input.Timezone != nil && *input.Timezone == timezone
	})).Return(models.CurrentUser{ID: userID, FullName: "Dmitry", Timezone: &timezone}, nil).Once()

	got, err := s.service.UpdateProfile(s.ctx, models.UpdateUserProfileInput{
		UserUUID: userID,
		FullName: &name,
		Timezone: &timezone,
	})

	require.NoError(t, err)
	require.Equal(t, "Dmitry", got.FullName)
}

func TestUploadAndDeleteAvatar(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	avatarStorage := storageMocks.NewAvatarStorage(t)
	s.service.SetAvatarStorage(avatarStorage)

	avatarStorage.On("Save", s.ctx, mock.MatchedBy(func(input models.SaveUserAvatarInput) bool {
		return input.UserUUID == userID && input.OriginalFilename == "avatar.png"
	})).Return(models.SavedUserAvatar{Path: "avatar.png", MimeType: "image/png", SizeBytes: 3}, nil).Once()
	s.userRepository.On("UpdateAvatar", s.ctx, mock.MatchedBy(func(input models.UserAvatarUpdate) bool {
		return input.UserUUID == userID && input.Path != nil && *input.Path == "avatar.png"
	})).Return(models.CurrentUser{ID: userID}, nil).Once()

	uploaded, err := s.service.UploadAvatar(s.ctx, models.SaveUserAvatarInput{
		UserUUID:         userID,
		OriginalFilename: "avatar.png",
		MimeType:         "image/png",
		Content:          strings.NewReader("png"),
	})

	require.NoError(t, err)
	require.Equal(t, "/api/v1/auth/me/avatar", uploaded.AvatarURL)

	path := "avatar.png"
	s.userRepository.On("GetUserByUUID", s.ctx, userID).
		Return(models.CurrentUser{ID: userID, AvatarPath: &path}, nil).
		Once()
	avatarStorage.On("Delete", s.ctx, path).Return(nil).Once()
	s.userRepository.On("DeleteAvatar", s.ctx, userID).Return(models.CurrentUser{ID: userID}, nil).Once()

	deleted, err := s.service.DeleteAvatar(s.ctx, userID)

	require.NoError(t, err)
	require.Equal(t, "/api/v1/auth/me/avatar", deleted.AvatarURL)
}

func TestUpdatePreferencesValidationAndCompanyVisibility(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	preferencesRepository := repositoryMocks.NewUserPreferencesRepository(t)
	companyRepository := repositoryMocks.NewCompanyRepository(t)
	s.service.SetPreferencesRepository(preferencesRepository)
	s.service.SetCompanyRepository(companyRepository)

	userID := uuid.New()
	invalidTheme := "blue"
	_, err := s.service.UpdatePreferences(s.ctx, models.UpdateUserPreferencesInput{
		UserUUID: userID,
		Theme:    &invalidTheme,
	})
	require.ErrorIs(t, err, models.ErrInvalidUserInput)

	companyID := uuid.New()
	darkTheme := "dark"
	dateFrom := "2026-07-01"
	dateTo := "2026-07-31"
	activeCompany := uuid.NullUUID{UUID: companyID, Valid: true}
	companyRepository.On("GetCompanyMember", s.ctx, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).
		Once()

	_, err = s.service.UpdatePreferences(s.ctx, models.UpdateUserPreferencesInput{
		UserUUID:          userID,
		ActiveCompanyUUID: &activeCompany,
		Theme:             &darkTheme,
		DateRange:         &models.PreferencesDateRange{From: &dateFrom, To: &dateTo},
	})
	require.ErrorIs(t, err, models.ErrCompanyNotFound)

	companyRepository.On("GetCompanyMember", s.ctx, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Status: models.MembershipStatusActive}, nil).
		Once()
	preferencesRepository.On("Upsert", s.ctx, mock.MatchedBy(func(input models.UpdateUserPreferencesInput) bool {
		return input.UserUUID == userID && input.ActiveCompanyUUID != nil && input.ActiveCompanyUUID.UUID == companyID
	})).Return(models.UserPreferences{
		UserUUID:          userID,
		ActiveCompanyUUID: activeCompany,
		Theme:             darkTheme,
		DateRange:         models.PreferencesDateRange{From: &dateFrom, To: &dateTo},
		UpdatedAt:         time.Now().UTC(),
	}, nil).Once()

	got, err := s.service.UpdatePreferences(s.ctx, models.UpdateUserPreferencesInput{
		UserUUID:          userID,
		ActiveCompanyUUID: &activeCompany,
		Theme:             &darkTheme,
		DateRange:         &models.PreferencesDateRange{From: &dateFrom, To: &dateTo},
	})

	require.NoError(t, err)
	require.Equal(t, darkTheme, got.Theme)
}
