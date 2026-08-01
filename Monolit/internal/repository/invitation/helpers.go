package invitation

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/jackc/pgx/v5/pgconn"
)

const invitationColumns = `
	invitation_uuid,
	company_uuid,
	department_uuid,
	invited_user_uuid,
	invited_by_user_uuid,
	company_role,
	department_role,
	status,
	expires_at,
	responded_at,
	created_at,
	updated_at
`

func normalizeCreateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return models.ErrInvitationAlreadyExists
	}

	return err
}
