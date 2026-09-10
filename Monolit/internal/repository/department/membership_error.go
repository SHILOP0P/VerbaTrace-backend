package department

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"verbatrace/monolit/internal/models"
)

func membershipWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "uq_department_members_one_active_company" {
		return models.ErrDepartmentMembershipConflict
	}
	return err
}
