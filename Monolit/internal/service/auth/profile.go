package auth

import (
	"context"
	"strings"
	"time"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

const avatarURL = "/api/v1/auth/me/avatar"

func (s *Service) UpdateProfile(ctx context.Context, input models.UpdateUserProfileInput) (models.CurrentUser, error) {
	if input.UserUUID == uuid.Nil {
		return models.CurrentUser{}, models.ErrInvalidUserInput
	}

	input.FullName = normalizeRequiredPatchString(input.FullName)
	input.FullSurname = normalizeRequiredPatchString(input.FullSurname)
	input.Post = normalizeOptionalPatchString(input.Post)
	input.Phone = normalizeOptionalPatchString(input.Phone)
	input.Timezone = normalizeOptionalPatchString(input.Timezone)

	if input.FullName != nil && *input.FullName == "" {
		return models.CurrentUser{}, models.ErrInvalidUserInput
	}
	if input.FullSurname != nil && *input.FullSurname == "" {
		return models.CurrentUser{}, models.ErrInvalidUserInput
	}
	if input.Timezone != nil && *input.Timezone != "" {
		if _, err := time.LoadLocation(*input.Timezone); err != nil {
			return models.CurrentUser{}, models.ErrInvalidUserInput
		}
	}

	return s.userRepository.UpdateProfile(ctx, input)
}

func normalizeOptionalPatchString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func (s *Service) UploadAvatar(ctx context.Context, input models.SaveUserAvatarInput) (models.UserAvatarResponse, error) {
	if s.avatarStorage == nil {
		return models.UserAvatarResponse{}, models.ErrInvalidUserInput
	}

	saved, err := s.avatarStorage.Save(ctx, input)
	if err != nil {
		return models.UserAvatarResponse{}, err
	}

	now := time.Now().UTC()
	_, err = s.userRepository.UpdateAvatar(ctx, models.UserAvatarUpdate{
		UserUUID:  input.UserUUID,
		Path:      &saved.Path,
		MimeType:  &saved.MimeType,
		SizeBytes: &saved.SizeBytes,
		UpdatedAt: &now,
	})
	if err != nil {
		_ = s.avatarStorage.Delete(context.Background(), saved.Path)
		return models.UserAvatarResponse{}, err
	}

	return models.UserAvatarResponse{AvatarURL: avatarURL, UpdatedAt: now}, nil
}

func (s *Service) DeleteAvatar(ctx context.Context, userID uuid.UUID) (models.UserAvatarResponse, error) {
	user, err := s.userRepository.GetUserByUUID(ctx, userID)
	if err != nil {
		return models.UserAvatarResponse{}, err
	}

	if user.AvatarPath != nil && s.avatarStorage != nil {
		_ = s.avatarStorage.Delete(ctx, *user.AvatarPath)
	}

	_, err = s.userRepository.DeleteAvatar(ctx, userID)
	if err != nil {
		return models.UserAvatarResponse{}, err
	}

	return models.UserAvatarResponse{AvatarURL: avatarURL, UpdatedAt: time.Now().UTC()}, nil
}

func (s *Service) GetAvatar(ctx context.Context, userID uuid.UUID) (models.File, error) {
	user, err := s.userRepository.GetUserByUUID(ctx, userID)
	if err != nil {
		return models.File{}, err
	}
	if user.AvatarPath == nil || user.AvatarMime == nil || s.avatarStorage == nil {
		return models.File{}, models.ErrUserNotFound
	}
	content, err := s.avatarStorage.Open(ctx, *user.AvatarPath)
	if err != nil {
		return models.File{}, err
	}
	return models.File{OriginalFilename: *user.AvatarPath, MimeType: *user.AvatarMime, Content: content}, nil
}

func normalizeRequiredPatchString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
