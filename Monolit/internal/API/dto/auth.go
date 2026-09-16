package dto

type RegisterRequest struct {
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	FullName    string  `json:"full_name"`
	FullSurname string  `json:"full_surname"`
	Username    string  `json:"username"`
	NickName    string  `json:"nick_name,omitempty"`
	Headline    *string `json:"headline,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	User UserResponse `json:"user"`
}

type UserResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	FullName    string  `json:"full_name"`
	FullSurname string  `json:"full_surname"`
	Username    string  `json:"username"`
	Role        string  `json:"role"`
	Headline    *string `json:"headline,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type PublicUserResponse struct {
	ID          string  `json:"id"`
	FullName    string  `json:"full_name"`
	FullSurname string  `json:"full_surname"`
	Username    string  `json:"username"`
	Headline    *string `json:"headline,omitempty"`
}

type RegisterResponse struct {
	User UserResponse `json:"user"`
}

type UpdateUsernameRequest struct {
	Username string `json:"username"`
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type UpdatePasswordResponse struct {
	UpdatedAt string `json:"updated_at"`
}

type UserSessionResponse struct {
	ID         string  `json:"id"`
	Current    bool    `json:"current"`
	UserAgent  *string `json:"user_agent"`
	IP         *string `json:"ip"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at"`
}

type UserSessionsResponse struct {
	Sessions               []UserSessionResponse `json:"sessions"`
	CanManageOtherSessions bool                  `json:"can_manage_other_sessions"`
	AvailableAt            *string               `json:"available_at,omitempty"`
	RetryAfterSeconds      int                   `json:"retry_after_seconds,omitempty"`
}

type UpdateProfileRequest struct {
	FullName    *string `json:"full_name"`
	FullSurname *string `json:"full_surname"`
	Headline    *string `json:"headline"`
	Phone       *string `json:"phone"`
	Timezone    *string `json:"timezone"`
}

type AvatarResponse struct {
	AvatarURL string `json:"avatar_url"`
	UpdatedAt string `json:"updated_at"`
}

type PreferencesDateRange struct {
	From *string `json:"from,omitempty"`
	To   *string `json:"to,omitempty"`
}

type UserPreferencesResponse struct {
	ActiveCompanyUUID *string              `json:"active_company_uuid"`
	Theme             string               `json:"theme"`
	DateRange         PreferencesDateRange `json:"date_range"`
	// InvitationsMuted is the "do not disturb" switch for invitations.
	InvitationsMuted bool `json:"invitations_muted"`
}

type UpdatePreferencesRequest struct {
	ActiveCompanyUUID *string               `json:"active_company_uuid"`
	Theme             *string               `json:"theme"`
	DateRange         *PreferencesDateRange `json:"date_range"`
	InvitationsMuted  *bool                 `json:"invitations_muted"`
}
