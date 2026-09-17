package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/planbundle"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ownershipColumns reads the stay list as a joined string rather than as a
// Postgres array: the project talks to the database through database/sql, where
// array parameters are not portable, and a comma-separated list of uuids needs
// no driver support on either side.
const ownershipColumns = `
	transfer_uuid,
	scope,
	company_uuid,
	COALESCE(array_to_string(stay_company_uuids, ','), ''),
	from_user_uuid,
	to_user_uuid,
	status,
	reason,
	decided_at,
	lock_version,
	created_at,
	expires_at
`

// CreateOwnershipTransfer offers the company, or every company under the
// owner's plan, to another person. Nothing changes until that person accepts.
func (r *Repository) CreateOwnershipTransfer(ctx context.Context, transfer model.CompanyOwnershipTransfer) (model.CompanyOwnershipTransfer, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO company_ownership_transfers (
			transfer_uuid, scope, company_uuid, stay_company_uuids,
			from_user_uuid, to_user_uuid, status, reason, created_at, expires_at
		)
		VALUES ($1, $2, $3, COALESCE(string_to_array(NULLIF($4, ''), ','), '{}')::uuid[], $5, $6, 'pending', $7, $8, $9)
		RETURNING `+ownershipColumns,
		transfer.ID,
		string(transfer.Scope),
		transfer.CompanyUUID,
		joinUUIDs(transfer.StayCompanyUUIDs),
		transfer.FromUserUUID,
		transfer.ToUserUUID,
		transfer.Reason,
		transfer.CreatedAt,
		transfer.ExpiresAt,
	)

	created, err := scanOwnership(row)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return model.CompanyOwnershipTransfer{}, model.ErrCompanyOwnershipTransferPending
		}
		return model.CompanyOwnershipTransfer{}, fmt.Errorf("create ownership transfer: %w", err)
	}

	created.CompanyUUIDs, err = r.ownershipCompanies(ctx, r.db, created)
	if err != nil {
		return model.CompanyOwnershipTransfer{}, err
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

	transfer.CompanyUUIDs, err = r.ownershipCompanies(ctx, r.db, transfer)
	if err != nil {
		return model.CompanyOwnershipTransfer{}, err
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

// AcceptOwnershipTransfer hands everything over in one transaction: the
// companies, the business plan that covers them and the personal plan that
// comes with it. The previous owner stays only in the companies they asked to
// stay in, and leaves the rest the same way any member leaves.
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

	// The recipient's situation may have changed while the offer was waiting, so
	// the conditions are checked again rather than trusted from creation time.
	if err := ensureCanReceiveOwnership(ctx, tx, transfer.ToUserUUID); err != nil {
		return model.CompanyOwnershipTransfer{}, err
	}

	companies, err := r.ownershipCompanies(ctx, tx, transfer)
	if err != nil {
		return model.CompanyOwnershipTransfer{}, err
	}
	if len(companies) == 0 {
		return model.CompanyOwnershipTransfer{}, model.ErrCompanyNotFound
	}
	transfer.CompanyUUIDs = companies

	stay := make(map[uuid.UUID]bool, len(transfer.StayCompanyUUIDs))
	for _, companyID := range transfer.StayCompanyUUIDs {
		stay[companyID] = true
	}

	for _, companyID := range companies {
		if err := handOverCompany(ctx, tx, companyID, transfer.FromUserUUID, transfer.ToUserUUID, stay[companyID], now); err != nil {
			return model.CompanyOwnershipTransfer{}, err
		}
	}

	if _, _, err := planbundle.Transfer(ctx, tx, transfer.FromUserUUID, transfer.ToUserUUID, now); err != nil {
		return model.CompanyOwnershipTransfer{}, err
	}

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

// handOverCompany moves one company to its new owner. The new owner takes the
// manager seat whether or not they were already a member, and the previous owner
// either becomes an ordinary member or leaves with every access the company gave
// them, exactly as an excluded member would.
func handOverCompany(ctx context.Context, tx *sql.Tx, companyID, fromUser, toUser uuid.UUID, previousOwnerStays bool, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status, created_at)
		VALUES ($1, $2, 'company_manager', 'active', $3)
		ON CONFLICT (company_uuid, user_uuid)
		DO UPDATE SET role = 'company_manager', status = 'active'
	`, companyID, toUser, now); err != nil {
		return fmt.Errorf("seat new owner: %w", err)
	}

	if previousOwnerStays {
		if _, err := tx.ExecContext(ctx, `
			UPDATE company_members SET role = 'employee'
			WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'
		`, companyID, fromUser); err != nil {
			return fmt.Errorf("keep previous owner as a member: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE company_members SET role = 'employee', status = 'left'
			WHERE company_uuid = $1 AND user_uuid = $2
		`, companyID, fromUser); err != nil {
			return fmt.Errorf("remove previous owner: %w", err)
		}
		if err := cleanupCompanyAccess(ctx, tx, companyID, fromUser, now); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE companies SET manager_user_uuid = $2 WHERE company_uuid = $1
	`, companyID, toUser); err != nil {
		return fmt.Errorf("update company owner: %w", err)
	}

	return nil
}

// ensureCanReceiveOwnership refuses to hand a company to somebody who already
// runs one or already pays for a business plan: one person holds one business
// plan, and merging two of them has no answer the product could give.
func ensureCanReceiveOwnership(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error {
	var ownsCompany bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM companies WHERE manager_user_uuid = $1 AND deleted_at IS NULL)
	`, userID).Scan(&ownsCompany); err != nil {
		return fmt.Errorf("check recipient companies: %w", err)
	}
	if ownsCompany {
		return model.ErrOwnershipRecipientBusy
	}

	holdsPlan, err := planbundle.HoldsBusinessPlan(ctx, tx, userID)
	if err != nil {
		return err
	}
	if holdsPlan {
		return model.ErrOwnershipRecipientBusy
	}

	return nil
}

// ownershipCompanies resolves what an offer covers. A single-company offer
// covers that company; an "all" offer covers whatever the owner still has when
// the offer is answered, which is the only correct answer for a plan that lives
// on the owner rather than on a company.
func (r *Repository) ownershipCompanies(ctx context.Context, q queryContexter, transfer model.CompanyOwnershipTransfer) ([]uuid.UUID, error) {
	if transfer.Scope == model.CompanyOwnershipTransferScopeCompany {
		if !transfer.CompanyUUID.Valid {
			return nil, model.ErrInvalidCompanyInput
		}
		return []uuid.UUID{transfer.CompanyUUID.UUID}, nil
	}

	rows, err := q.QueryContext(ctx, `
		SELECT company_uuid FROM companies
		WHERE manager_user_uuid = $1 AND deleted_at IS NULL
		ORDER BY created_at
	`, transfer.FromUserUUID)
	if err != nil {
		return nil, fmt.Errorf("list companies of the previous owner: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan company of the previous owner: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// ListCompaniesByManager returns the companies a person owns, which is what the
// "all companies" offer covers.
func (r *Repository) ListCompaniesByManager(ctx context.Context, ownerID uuid.UUID) ([]model.Company, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT company_uuid, name, tag, manager_user_uuid, member_limit, created_at, deleted_at
		FROM companies
		WHERE manager_user_uuid = $1 AND deleted_at IS NULL
		ORDER BY created_at
	`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list companies by manager: %w", err)
	}
	defer func() { _ = rows.Close() }()

	companies := []model.Company{}
	for rows.Next() {
		repoCompany, err := scaner.ScanCompany(rows)
		if err != nil {
			return nil, fmt.Errorf("scan company by manager: %w", err)
		}
		company, err := converter.RepoCompanyToModel(repoCompany)
		if err != nil {
			return nil, err
		}
		companies = append(companies, company)
	}

	return companies, rows.Err()
}

// CountOwnedCompanies is what the service needs to pick the right operation:
// one company may be given away on its own, several only together.
func (r *Repository) CountOwnedCompanies(ctx context.Context, ownerID uuid.UUID) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `
		SELECT count(*) FROM companies WHERE manager_user_uuid = $1 AND deleted_at IS NULL
	`, ownerID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count owned companies: %w", err)
	}

	return count, nil
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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		companies, err := r.ownershipCompanies(ctx, r.db, items[i])
		if err != nil {
			return nil, err
		}
		items[i].CompanyUUIDs = companies
	}

	return items, nil
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

type queryContexter interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func scanOwnership(row interface{ Scan(dest ...any) error }) (model.CompanyOwnershipTransfer, error) {
	var transfer model.CompanyOwnershipTransfer
	var scope, stay string
	var reason sql.NullString
	var decidedAt sql.NullTime

	if err := row.Scan(
		&transfer.ID,
		&scope,
		&transfer.CompanyUUID,
		&stay,
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

	transfer.Scope = model.CompanyOwnershipTransferScope(scope)
	stayIDs, err := splitUUIDs(stay)
	if err != nil {
		return model.CompanyOwnershipTransfer{}, err
	}
	transfer.StayCompanyUUIDs = stayIDs
	if reason.Valid {
		transfer.Reason = &reason.String
	}
	if decidedAt.Valid {
		transfer.DecidedAt = &decidedAt.Time
	}

	return transfer, nil
}

func joinUUIDs(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return ""
	}

	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}

	return strings.Join(parts, ",")
}

func splitUUIDs(raw string) ([]uuid.UUID, error) {
	if raw == "" {
		return []uuid.UUID{}, nil
	}

	parts := strings.Split(raw, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		id, err := uuid.Parse(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parse stay company uuid: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}
