package department

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const transferColumns = `
	request_uuid,
	company_uuid,
	user_uuid,
	from_department_uuid,
	to_department_uuid,
	requested_by_user_uuid,
	reason,
	status,
	decided_by_user_uuid,
	decided_at,
	decision_comment,
	lock_version,
	created_at,
	expires_at
`

// CreateDepartmentTransfer records a leader's request to take a colleague from
// another department. The move itself happens only after approval.
func (r *Repository) CreateDepartmentTransfer(ctx context.Context, request model.DepartmentTransferRequest) (model.DepartmentTransferRequest, error) {
	query := `
	INSERT INTO department_transfer_requests (
		request_uuid, company_uuid, user_uuid, from_department_uuid, to_department_uuid,
		requested_by_user_uuid, reason, status, created_at, expires_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9)
	RETURNING ` + transferColumns

	row := r.db.QueryRowContext(ctx, query,
		request.ID,
		request.CompanyUUID,
		request.UserUUID,
		request.FromDepartmentUUID,
		request.ToDepartmentUUID,
		request.RequestedByUserUUID,
		request.Reason,
		request.CreatedAt,
		request.ExpiresAt,
	)

	created, err := scanTransfer(row)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return model.DepartmentTransferRequest{}, model.ErrDepartmentTransferPending
		}
		return model.DepartmentTransferRequest{}, fmt.Errorf("create department transfer: %w", err)
	}

	return created, nil
}

func (r *Repository) GetDepartmentTransfer(ctx context.Context, id uuid.UUID) (model.DepartmentTransferRequest, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+transferColumns+` FROM department_transfer_requests WHERE request_uuid = $1`, id)
	request, err := scanTransfer(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DepartmentTransferRequest{}, model.ErrDepartmentTransferNotFound
		}
		return model.DepartmentTransferRequest{}, fmt.Errorf("get department transfer: %w", err)
	}

	return request, nil
}

func (r *Repository) ListDepartmentTransfers(ctx context.Context, companyID uuid.UUID, status model.DepartmentTransferStatus) ([]model.DepartmentTransferRequest, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+transferColumns+`
		FROM department_transfer_requests
		WHERE company_uuid = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
	`, companyID, string(status))
	if err != nil {
		return nil, fmt.Errorf("list department transfers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []model.DepartmentTransferRequest{}
	for rows.Next() {
		request, err := scanTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan department transfer: %w", err)
		}
		items = append(items, request)
	}

	return items, rows.Err()
}

// DecideDepartmentTransfer closes the request. The caller performs the move
// itself only when the decision is an approval.
func (r *Repository) DecideDepartmentTransfer(ctx context.Context, id uuid.UUID, decidedBy uuid.UUID, approve bool, comment string, now time.Time) (model.DepartmentTransferRequest, error) {
	status := model.DepartmentTransferStatusRejected
	if approve {
		status = model.DepartmentTransferStatusApproved
	}

	row := r.db.QueryRowContext(ctx, `
		UPDATE department_transfer_requests
		SET status = $2,
		    decided_by_user_uuid = $3,
		    decided_at = $4,
		    decision_comment = NULLIF($5, ''),
		    lock_version = lock_version + 1
		WHERE request_uuid = $1 AND status = 'pending'
		RETURNING `+transferColumns, id, string(status), decidedBy, now, comment)

	request, err := scanTransfer(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DepartmentTransferRequest{}, model.ErrDepartmentTransferNotFound
		}
		return model.DepartmentTransferRequest{}, fmt.Errorf("decide department transfer: %w", err)
	}

	return request, nil
}

// ExpireDepartmentTransfers closes requests nobody answered in time.
func (r *Repository) ExpireDepartmentTransfers(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE department_transfer_requests
		SET status = 'expired', decided_at = $1, lock_version = lock_version + 1
		WHERE status = 'pending' AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire department transfers: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count expired department transfers: %w", err)
	}

	return affected, nil
}

func scanTransfer(row interface{ Scan(dest ...any) error }) (model.DepartmentTransferRequest, error) {
	var request model.DepartmentTransferRequest
	var reason sql.NullString
	var decidedBy uuid.NullUUID
	var decidedAt sql.NullTime
	var comment sql.NullString

	if err := row.Scan(
		&request.ID,
		&request.CompanyUUID,
		&request.UserUUID,
		&request.FromDepartmentUUID,
		&request.ToDepartmentUUID,
		&request.RequestedByUserUUID,
		&reason,
		&request.Status,
		&decidedBy,
		&decidedAt,
		&comment,
		&request.LockVersion,
		&request.CreatedAt,
		&request.ExpiresAt,
	); err != nil {
		return model.DepartmentTransferRequest{}, err
	}

	if reason.Valid {
		request.Reason = &reason.String
	}
	request.DecidedByUserUUID = decidedBy
	if decidedAt.Valid {
		request.DecidedAt = &decidedAt.Time
	}
	if comment.Valid {
		request.DecisionComment = &comment.String
	}

	return request, nil
}
