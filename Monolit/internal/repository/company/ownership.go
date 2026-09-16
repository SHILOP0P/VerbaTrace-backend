package company

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

const ownershipColumns = `
	transfer_uuid,
	company_uuid,
	from_user_uuid,
	to_user_uuid,
	status,
	reason,
	decided_at,
	lock_version,
	created_at,
	expires_at
`

// CreateOwnershipTransfer offers the company to another member. Nothing changes
// until that person accepts.
func (r *Repository) CreateOwnershipTransfer(ctx context.Context, transfer model.CompanyOwnershipTransfer) (model.CompanyOwnershipTransfer, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO company_ownership_transfers (
			transfer_uuid, company_uuid, from_user_uuid, to_user_uuid, status, reason, created_at, expires_at
		)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7)
		RETURNING `+ownershipColumns,
		transfer.ID, transfer.CompanyUUID, transfer.FromUserUUID, transfer.ToUserUUID, transfer.Reason, transfer.CreatedAt, transfer.ExpiresAt)

	created, err := scanOwnership(row)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferPending
		}
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("create ownership transfer: %w", err)
	}

	return created, nil
}

func (r *Repository) GetOwnershipTransfer(ctx context.Context, id uuid.UUID) (model.CompanyOwnershipTransfer, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+ownershipColumns+` FROM company_ownership_transfers WHERE transfer_uuid = $1`, id)
	transfer, err := scanOwnership(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferNotFound
		}
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("get ownership transfer: %w", err)
	}

	return transfer, nil
}

// CloseOwnershipTransfer records a decline or a cancellation.
func (r *Repository) CloseOwnershipTransfer(ctx context.Context, id uuid.UUID, status model.CompanyOwnershipTransferStatus, now time.Time) (model.CompanyOwnershipTransfer, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE company_ownership_transfers
		SET status = $2, decided_at = $3, lock_version = lock_version + 1
		WHERE transfer_uuid = $1 AND status = 'pending'
		RETURNING `+ownershipColumns, id, string(status), now)

	transfer, err := scanOwnership(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferNotFound
		}
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("close ownership transfer: %w", err)
	}

	return transfer, nil
}

// AcceptOwnershipTransfer hands the company over. The previous owner stays in
// the company as the deputy, or as a regular member when the deputy seat is
// taken by somebody else.
func (r *Repository) AcceptOwnershipTransfer(ctx context.Context, id uuid.UUID, now time.Time) (model.CompanyOwnershipTransfer, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("begin accept ownership transfer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT `+ownershipColumns+`
		FROM company_ownership_transfers
		WHERE transfer_uuid = $1
		FOR UPDATE`, id)
	transfer, err := scanOwnership(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferNotFound
		}
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("lock ownership transfer: %w", err)
	}
	if transfer.Status != model.CompanyOwnershipTransferPending {
		return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferNotFound
	}
	if !transfer.ExpiresAt.After(now) {
		if _, err := tx.ExecContext(ctx, `UPDATE company_ownership_transfers SET status='expired', decided_at=$2 WHERE transfer_uuid=$1`, id, now); err != nil {
			return model.CompanyOwnershipTransfer{}, fmt.Errorf("expire ownership transfer: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return model.CompanyOwnershipTransfer{}, fmt.Errorf("commit expired ownership transfer: %w", err)
		}
		return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferNotFound
	}

	var deputyTaken bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM company_members
			WHERE company_uuid = $1 AND status = 'active' AND role = 'company_deputy' AND user_uuid <> $2
		)
	`, transfer.CompanyUUID, transfer.ToUserUUID).Scan(&deputyTaken); err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("check deputy seat: %w", err)
	}

	previousOwnerRole := model.CompanyMemberRoleDeputy
	if deputyTaken {
		previousOwnerRole = model.CompanyMemberRoleEmployee
	}

	// The new owner is promoted first, so the deputy seat they may have held is
	// free for the previous owner.
	if _, err := tx.ExecContext(ctx, `
		UPDATE company_members
		SET role = 'company_manager'
		WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'
	`, transfer.CompanyUUID, transfer.ToUserUUID); err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("promote new owner: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE company_members
		SET role = $3
		WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'
	`, transfer.CompanyUUID, transfer.FromUserUUID, string(previousOwnerRole)); err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("demote previous owner: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE companies SET manager_user_uuid = $2 WHERE company_uuid = $1
	`, transfer.CompanyUUID, transfer.ToUserUUID); err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("update company owner: %w", err)
	}

	// The subscription follows the company, so the new owner keeps the same
	// plan and the same remaining period.
	if _, err := tx.ExecContext(ctx, `
		UPDATE company_ownership_transfers
		SET status = 'accepted', decided_at = $2, lock_version = lock_version + 1
		WHERE transfer_uuid = $1
	`, id, now); err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("mark ownership transfer accepted: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("commit accept ownership transfer: %w", err)
	}

	transfer.Status = model.CompanyOwnershipTransferAccepted
	transfer.DecidedAt = &now

	return transfer, nil
}

// ListIncomingOwnershipTransfers returns the offers waiting for this user, so
// the interface can show them without a notification deep link.
func (r *Repository) ListIncomingOwnershipTransfers(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.CompanyOwnershipTransfer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ownershipColumns+`
		FROM company_ownership_transfers
		WHERE to_user_uuid = $1 AND status = 'pending' AND expires_at > $2
		ORDER BY created_at DESC
	`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("list incoming ownership transfers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []model.CompanyOwnershipTransfer{}
	for rows.Next() {
		transfer, err := scanOwnership(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ownership transfer: %w", err)
		}
		items = append(items, transfer)
	}

	return items, rows.Err()
}

// ExpireOwnershipTransfers closes offers nobody answered.
func (r *Repository) ExpireOwnershipTransfers(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE company_ownership_transfers
		SET status = 'expired', decided_at = $1, lock_version = lock_version + 1
		WHERE status = 'pending' AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire ownership transfers: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count expired ownership transfers: %w", err)
	}

	return affected, nil
}

func scanOwnership(row interface{ Scan(dest ...any) error }) (model.CompanyOwnershipTransfer, error) {
	var transfer model.CompanyOwnershipTransfer
	var reason sql.NullString
	var decidedAt sql.NullTime

	if err := row.Scan(
		&transfer.ID,
		&transfer.CompanyUUID,
		&transfer.FromUserUUID,
		&transfer.ToUserUUID,
		&transfer.Status,
		&reason,
		&decidedAt,
		&transfer.LockVersion,
		&transfer.CreatedAt,
		&transfer.ExpiresAt,
	); err != nil {
		return model.CompanyOwnershipTransfer{}, err
	}

	if reason.Valid {
		transfer.Reason = &reason.String
	}
	if decidedAt.Valid {
		transfer.DecidedAt = &decidedAt.Time
	}

	return transfer, nil
}
