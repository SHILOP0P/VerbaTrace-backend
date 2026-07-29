package contact

import (
	"context"
	"fmt"
	"strings"

	"calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	"calllens/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) SearchUsers(ctx context.Context, usernamePrefix string, limit int) ([]models.User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_uuid, email, password_hash, full_name, full_surname, username, role, post, phone, timezone, avatar_path, avatar_mime_type, avatar_size_bytes, avatar_updated_at, created_at FROM users WHERE username ILIKE $1 ESCAPE E'\\' ORDER BY username ASC LIMIT $2`, escapeLike(usernamePrefix)+"%", limit)
	if err != nil {
		return nil, wrap("search users", err)
	}
	defer func() { _ = rows.Close() }()
	users := make([]models.User, 0)
	for rows.Next() {
		item, err := scaner.ScanUser(rows)
		if err != nil {
			return nil, wrap("scan user search", err)
		}
		user, err := converter.RepoUserToModel(item)
		if err != nil {
			return nil, wrap("convert user search", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate user search", err)
	}
	return users, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func (r *Repository) AddContact(ctx context.Context, userID, contactID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_contacts (user_uuid, contact_user_uuid) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, contactID)
	return wrap("add contact", err)
}

func (r *Repository) RemoveContact(ctx context.Context, userID, contactID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_contacts WHERE user_uuid = $1 AND contact_user_uuid = $2`, userID, contactID)
	return wrap("remove contact", err)
}

func (r *Repository) ListContactIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return r.listIDs(ctx, `SELECT contact_user_uuid FROM user_contacts WHERE user_uuid = $1 ORDER BY created_at DESC`, userID)
}

func (r *Repository) AddFavoriteCall(ctx context.Context, userID, callID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_favorite_calls (user_uuid, call_uuid) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, callID)
	return wrap("add favorite call", err)
}

func (r *Repository) RemoveFavoriteCall(ctx context.Context, userID, callID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_favorite_calls WHERE user_uuid = $1 AND call_uuid = $2`, userID, callID)
	return wrap("remove favorite call", err)
}

func (r *Repository) ListFavoriteCallIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return r.listIDs(ctx, `SELECT call_uuid FROM user_favorite_calls WHERE user_uuid = $1 ORDER BY created_at DESC`, userID)
}

func (r *Repository) listIDs(ctx context.Context, query string, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, wrap("list", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, wrap("scan list", err)
		}
		ids = append(ids, id)
	}
	return ids, wrap("iterate list", rows.Err())
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
