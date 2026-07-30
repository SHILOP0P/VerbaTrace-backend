package models

import "github.com/google/uuid"

type ContactList struct {
	Users []PublicUser
}

type FavoriteCallList struct {
	Calls []Call
}

type AddContactInput struct {
	UserID    uuid.UUID
	ContactID uuid.UUID
}

type FavoriteCallInput struct {
	UserID uuid.UUID
	CallID uuid.UUID
}
