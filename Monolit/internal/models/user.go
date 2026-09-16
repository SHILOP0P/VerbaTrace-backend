package models

import (
	"io"
	"time"

	"github.com/google/uuid"
)

type UserAccount struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         UserRole
	CreatedAt    time.Time
}

type UserProfile struct {
	UserID          uuid.UUID
	FullName        string
	FullSurname     string
	Username        string
	Headline        *string
	Phone           *string
	Timezone        *string
	AvatarPath      *string
	AvatarMime      *string
	AvatarSize      *int64
	AvatarUpdatedAt *time.Time
}

// CurrentUser is the authenticated account/profile aggregate returned by auth use cases.
// Public lookup and contacts must use PublicUser instead.
type CurrentUser struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	FullName        string
	FullSurname     string
	Username        string
	Role            UserRole
	Post            *string
	Phone           *string
	Timezone        *string
	AvatarPath      *string
	AvatarMime      *string
	AvatarSize      *int64
	AvatarUpdatedAt *time.Time
	CreatedAt       time.Time
}

type UserRole string

const (
	UserRoleUser       UserRole = "user"
	UserRoleHelper     UserRole = "helper"
	UserRoleAdmin      UserRole = "admin"
	UserRoleSuperAdmin UserRole = "superadmin"
)

type CreateUserInput struct {
	Email       string
	Password    string
	FullName    string
	FullSurname string
	Username    string
	Post        *string
}

type UpdateUsernameInput struct {
	UserUUID uuid.UUID
	Username string
}

type UpdatePasswordInput struct {
	UserUUID        uuid.UUID
	SessionUUID     uuid.UUID
	CurrentPassword string
	NewPassword     string
}

type UpdatePasswordResult struct {
	UpdatedAt time.Time
}

type UpdateUserProfileInput struct {
	UserUUID    uuid.UUID
	FullName    *string
	FullSurname *string
	Post        *string
	Phone       *string
	Timezone    *string
}

type SaveUserAvatarInput struct {
	UserUUID         uuid.UUID
	OriginalFilename string
	MimeType         string
	SizeBytes        int64
	Content          io.Reader
}

type SavedUserAvatar struct {
	Path      string
	MimeType  string
	SizeBytes int64
}

type UserAvatarUpdate struct {
	UserUUID  uuid.UUID
	Path      *string
	MimeType  *string
	SizeBytes *int64
	UpdatedAt *time.Time
}

type UserAvatarResponse struct {
	AvatarURL string
	UpdatedAt time.Time
}

type PreferencesDateRange struct {
	From *string
	To   *string
}

type UserPreferences struct {
	UserUUID          uuid.UUID
	ActiveCompanyUUID uuid.NullUUID
	Theme             string
	DateRange         PreferencesDateRange
	// InvitationsMuted is the "do not disturb" switch: nobody can send this user
	// company or department invitations while it is on.
	InvitationsMuted bool
	UpdatedAt        time.Time
}

type UpdateUserPreferencesInput struct {
	UserUUID          uuid.UUID
	ActiveCompanyUUID *uuid.NullUUID
	Theme             *string
	DateRange         *PreferencesDateRange
	InvitationsMuted  *bool
}

type LoginInput struct {
	Email     string
	Password  string
	UserAgent *string
	IPAddress *string
}

type RefreshTokenInput struct {
	RefreshToken string
}
