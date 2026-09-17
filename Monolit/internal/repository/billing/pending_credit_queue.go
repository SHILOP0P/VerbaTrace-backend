package billing

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CountCallsAwaitingCredits counts the calls already parked for lack of budget
// in the scope an upload would join: the department when it has a cap of its
// own, the company otherwise, and a person's own calls outside any company.
func (r *Repository) CountCallsAwaitingCredits(ctx context.Context, companyID, departmentID uuid.NullUUID, userID uuid.UUID) (int, error) {
	var query string
	var arg any

	switch {
	case departmentID.Valid && companyID.Valid:
		var departmentCapped bool
		if err := r.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM department_credit_limits WHERE department_uuid=$1)`,
			departmentID.UUID).Scan(&departmentCapped); err != nil {
			return 0, fmt.Errorf("count calls awaiting credits: %w", err)
		}
		if departmentCapped {
			query, arg = `SELECT count(*) FROM calls WHERE department_uuid=$1 AND status='awaiting_credits' AND deleted_at IS NULL`, departmentID.UUID
		} else {
			query, arg = `SELECT count(*) FROM calls WHERE company_uuid=$1 AND status='awaiting_credits' AND deleted_at IS NULL`, companyID.UUID
		}
	case companyID.Valid:
		query, arg = `SELECT count(*) FROM calls WHERE company_uuid=$1 AND status='awaiting_credits' AND deleted_at IS NULL`, companyID.UUID
	default:
		query, arg = `SELECT count(*) FROM calls WHERE uploaded_by_user_uuid=$1 AND company_uuid IS NULL AND status='awaiting_credits' AND deleted_at IS NULL`, userID
	}

	var waiting int
	if err := r.db.QueryRowContext(ctx, query, arg).Scan(&waiting); err != nil {
		return 0, fmt.Errorf("count calls awaiting credits: %w", err)
	}

	return waiting, nil
}
