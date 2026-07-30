package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type UserAccount struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

type UserProfile struct {
	UserID          uuid.UUID
	FullName        string
	FullSurname     string
	Username        string
	Headline        sql.NullString
	Phone           sql.NullString
	Timezone        sql.NullString
	AvatarPath      sql.NullString
	AvatarMime      sql.NullString
	AvatarSize      sql.NullInt64
	AvatarUpdatedAt sql.NullTime
}

type CurrentUserRecord struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	FullName        string
	FullSurname     string
	Username        string
	Role            string
	Post            sql.NullString
	Phone           sql.NullString
	Timezone        sql.NullString
	AvatarPath      sql.NullString
	AvatarMime      sql.NullString
	AvatarSize      sql.NullInt64
	AvatarUpdatedAt sql.NullTime
	CreatedAt       time.Time
}
