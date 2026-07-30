package models

import "github.com/google/uuid"

// PublicUser is the deliberately minimal representation used by lookup and contacts.
type PublicUser struct {
	ID          uuid.UUID
	FullName    string
	FullSurname string
	Username    string
	Headline    *string
}
