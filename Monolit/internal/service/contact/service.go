package contact

import (
	"context"
	"strings"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

type Service struct {
	contacts repository.ContactRepository
	users    repository.UserRepository
	calls    repository.CallRepository
}

func NewService(contacts repository.ContactRepository, users repository.UserRepository, calls repository.CallRepository) *Service {
	return &Service{contacts: contacts, users: users, calls: calls}
}

func (s *Service) SearchContacts(ctx context.Context, userID uuid.UUID, value string) ([]models.PublicUser, error) {
	if userID == uuid.Nil {
		return nil, models.ErrInvalidContactInput
	}
	prefix := "@" + strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "@"))
	if len([]rune(prefix)) < 3 {
		return []models.PublicUser{}, nil
	}
	users, err := s.contacts.SearchPublicUsers(ctx, prefix, 10)
	if err != nil {
		return nil, err
	}
	result := make([]models.PublicUser, 0, len(users))
	for _, user := range users {
		if user.ID != userID {
			result = append(result, user)
		}
	}
	return result, nil
}

func (s *Service) AddContact(ctx context.Context, input models.AddContactInput) error {
	if input.UserID == uuid.Nil || input.ContactID == uuid.Nil || input.UserID == input.ContactID {
		return models.ErrInvalidContactInput
	}
	if _, err := s.users.GetUserByUUID(ctx, input.ContactID); err != nil {
		return err
	}
	return s.contacts.AddContact(ctx, input.UserID, input.ContactID)
}

func (s *Service) RemoveContact(ctx context.Context, input models.AddContactInput) error {
	if input.UserID == uuid.Nil || input.ContactID == uuid.Nil {
		return models.ErrInvalidContactInput
	}
	return s.contacts.RemoveContact(ctx, input.UserID, input.ContactID)
}

func (s *Service) ListContacts(ctx context.Context, userID uuid.UUID) (models.ContactList, error) {
	if userID == uuid.Nil {
		return models.ContactList{}, models.ErrInvalidContactInput
	}
	ids, err := s.contacts.ListContactIDs(ctx, userID)
	if err != nil {
		return models.ContactList{}, err
	}
	result := models.ContactList{Users: make([]models.PublicUser, 0, len(ids))}
	for _, id := range ids {
		user, err := s.contacts.GetPublicUserByUUID(ctx, id)
		if err != nil {
			return models.ContactList{}, err
		}
		result.Users = append(result.Users, user)
	}
	return result, nil
}

func (s *Service) AddFavoriteCall(ctx context.Context, input models.FavoriteCallInput) error {
	if input.UserID == uuid.Nil || input.CallID == uuid.Nil {
		return models.ErrInvalidContactInput
	}
	if _, err := s.calls.GetByUUID(ctx, input.CallID, input.UserID); err != nil {
		return err
	}
	return s.contacts.AddFavoriteCall(ctx, input.UserID, input.CallID)
}

func (s *Service) RemoveFavoriteCall(ctx context.Context, input models.FavoriteCallInput) error {
	if input.UserID == uuid.Nil || input.CallID == uuid.Nil {
		return models.ErrInvalidContactInput
	}
	return s.contacts.RemoveFavoriteCall(ctx, input.UserID, input.CallID)
}

// maxFavoriteCalls caps what one list returns. Favourites used to be read one
// call at a time with no limit at all, so a person with a thousand of them
// issued a thousand queries inside a single request.
const maxFavoriteCalls = 200

func (s *Service) ListFavoriteCalls(ctx context.Context, userID uuid.UUID) (models.FavoriteCallList, error) {
	if userID == uuid.Nil {
		return models.FavoriteCallList{}, models.ErrInvalidContactInput
	}

	// The calls list applies the same visibility rule the per-call read did, so
	// asking it once is both cheaper and exactly as safe.
	calls, err := s.calls.ListFiltered(ctx, models.ListCallsInput{
		UserID:       userID,
		FavoriteOnly: true,
		Limit:        maxFavoriteCalls,
	})
	if err != nil {
		return models.FavoriteCallList{}, err
	}

	return models.FavoriteCallList{Calls: calls.Items}, nil
}
