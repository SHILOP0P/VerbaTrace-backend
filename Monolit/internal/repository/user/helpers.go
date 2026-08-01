package user

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/jackc/pgx/v5/pgconn"
)

func normalizeUserError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return models.ErrUserAlreadyExists
	}

	return err
}
