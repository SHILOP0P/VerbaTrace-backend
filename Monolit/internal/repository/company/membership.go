package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// CountActiveCompanyMembersExcept counts everybody who still works in the
// company besides the given user. Company deletion relies on it.
func (r *Repository) CountActiveCompanyMembersExcept(ctx context.Context, companyID uuid.UUID, exceptUserID uuid.UUID) (int, error) {
	const query = `
	SELECT COUNT(*)
	FROM company_members
	WHERE company_uuid = $1
	  AND user_uuid <> $2
	  AND status = 'active'
	`

	var count int
	if err := r.db.QueryRowContext(ctx, query, companyID, exceptUserID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active company members: %w", err)
	}

	return count, nil
}

// ActiveEmployerCompany returns the company where the user currently works as a
// regular member. Owners and deputies are not bound by the one-company rule, so
// their memberships are ignored here.
func (r *Repository) ActiveEmployerCompany(ctx context.Context, userID uuid.UUID) (model.Company, error) {
	const query = `
	SELECT c.company_uuid,
	       c.name,
	       c.tag,
	       c.manager_user_uuid,
	       c.member_limit,
	       c.created_at,
	       c.deleted_at
	FROM companies c
	JOIN company_members cm ON cm.company_uuid = c.company_uuid
	WHERE cm.user_uuid = $1
	  AND cm.status = 'active'
	  AND cm.role = 'employee'
	  AND c.deleted_at IS NULL
	LIMIT 1
	`

	row := r.db.QueryRowContext(ctx, query, userID)
	repoCompany, err := scaner.ScanCompany(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Company{}, model.ErrCompanyNotFound
		}
		return model.Company{}, fmt.Errorf("get active employer company: %w", err)
	}

	return converter.RepoCompanyToModel(repoCompany)
}

// AssignCompanyDeputy promotes an active member to the deputy seat. The unique
// index keeps a company to a single deputy.
func (r *Repository) AssignCompanyDeputy(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (model.CompanyMember, error) {
	const query = `
	UPDATE company_members
	SET role = 'company_deputy'
	WHERE company_uuid = $1
	  AND user_uuid = $2
	  AND status = 'active'
	  AND role = 'employee'
	RETURNING company_uuid, user_uuid, role, status, created_at
	`

	row := r.db.QueryRowContext(ctx, query, companyID, userID)
	repoMember, err := scaner.ScanCompanyMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyMember{}, model.ErrCompanyNotFound
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "uq_company_members_active_deputy" {
			return model.CompanyMember{}, model.ErrCompanyDeputyAlreadyAssigned
		}
		return model.CompanyMember{}, fmt.Errorf("assign company deputy: %w", err)
	}

	return converter.RepoCompanyMemberToModel(repoMember)
}

// RevokeCompanyDeputy returns the deputy back to a regular membership.
func (r *Repository) RevokeCompanyDeputy(ctx context.Context, companyID uuid.UUID) (model.CompanyMember, error) {
	const query = `
	UPDATE company_members
	SET role = 'employee'
	WHERE company_uuid = $1
	  AND status = 'active'
	  AND role = 'company_deputy'
	RETURNING company_uuid, user_uuid, role, status, created_at
	`

	row := r.db.QueryRowContext(ctx, query, companyID)
	repoMember, err := scaner.ScanCompanyMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyMember{}, model.ErrCompanyDeputyNotAssigned
		}
		return model.CompanyMember{}, fmt.Errorf("revoke company deputy: %w", err)
	}

	return converter.RepoCompanyMemberToModel(repoMember)
}

// RemoveCompanyMember ends a membership and everything the company gave the
// person: departments, folder grants, the selected company and pending
// invitations. Leaving and being excluded follow the same path.
func (r *Repository) RemoveCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, now time.Time) (model.CompanyMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CompanyMember{}, fmt.Errorf("begin remove company member: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const memberQuery = `
	UPDATE company_members
	SET status = 'left',
	    role = CASE WHEN role = 'company_deputy' THEN 'employee' ELSE role END
	WHERE company_uuid = $1
	  AND user_uuid = $2
	  AND status = 'active'
	  AND role <> 'company_manager'
	RETURNING company_uuid, user_uuid, role, status, created_at
	`

	row := tx.QueryRowContext(ctx, memberQuery, companyID, userID)
	repoMember, err := scaner.ScanCompanyMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyMember{}, model.ErrCompanyNotFound
		}
		return model.CompanyMember{}, fmt.Errorf("remove company member: %w", err)
	}

	if err := cleanupCompanyAccess(ctx, tx, companyID, userID, now); err != nil {
		return model.CompanyMember{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CompanyMember{}, fmt.Errorf("commit remove company member: %w", err)
	}

	return converter.RepoCompanyMemberToModel(repoMember)
}

// CleanupCompanyAccessTx lets another repository end a membership inside its own
// transaction, so accepting an invitation and leaving the previous company stay
// one atomic step.
func CleanupCompanyAccessTx(ctx context.Context, tx *sql.Tx, companyID uuid.UUID, userID uuid.UUID, now time.Time) error {
	return cleanupCompanyAccess(ctx, tx, companyID, userID, now)
}

func cleanupCompanyAccess(ctx context.Context, tx *sql.Tx, companyID uuid.UUID, userID uuid.UUID, now time.Time) error {
	statements := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "close department memberships",
			query: `UPDATE department_members
			        SET status = 'left'
			        WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'`,
			args: []any{companyID, userID},
		},
		{
			name: "clear selected company",
			query: `UPDATE user_preferences
			        SET active_company_uuid = NULL, updated_at = $3
			        WHERE user_uuid = $2 AND active_company_uuid = $1`,
			args: []any{companyID, userID, now},
		},
		{
			name: "cancel pending invitations",
			query: `UPDATE membership_invitations
			        SET status = 'canceled', updated_at = $3
			        WHERE company_uuid = $1 AND invited_user_uuid = $2 AND status = 'pending'`,
			args: []any{companyID, userID, now},
		},
		{
			name: "cancel pending department transfers",
			query: `UPDATE department_transfer_requests
			        SET status = 'canceled', decided_at = $3, lock_version = lock_version + 1
			        WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'pending'`,
			args: []any{companyID, userID, now},
		},
		{
			name: "unmap integration users",
			query: `UPDATE integration_external_user_mappings m
			        SET internal_user_uuid = NULL, department_uuid = NULL, status = 'unmapped',
			            lock_version = m.lock_version + 1, updated_at = $3
			        FROM integration_connections c
			        WHERE c.connection_uuid = m.connection_uuid
			          AND c.company_uuid = $1
			          AND m.internal_user_uuid = $2`,
			args: []any{companyID, userID, now},
		},
	}

	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("%s: %w", statement.name, err)
		}
	}

	return nil
}

// UpsertMembershipRestriction records that an owner or deputy excluded the user.
func (r *Repository) UpsertMembershipRestriction(ctx context.Context, restriction model.CompanyMembershipRestriction) error {
	const query = `
	INSERT INTO company_membership_restrictions (
		restriction_uuid, company_uuid, user_uuid, kind, reason, created_by_user_uuid, created_at, expires_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (company_uuid, user_uuid)
	DO UPDATE SET kind = EXCLUDED.kind,
	              reason = EXCLUDED.reason,
	              created_by_user_uuid = EXCLUDED.created_by_user_uuid,
	              created_at = EXCLUDED.created_at,
	              expires_at = EXCLUDED.expires_at
	`

	_, err := r.db.ExecContext(ctx, query,
		restriction.ID,
		restriction.CompanyUUID,
		restriction.UserUUID,
		restriction.Kind,
		restriction.Reason,
		restriction.CreatedByUserUUID,
		restriction.CreatedAt,
		restriction.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upsert membership restriction: %w", err)
	}

	return nil
}

// HasActiveMembershipRestriction reports whether a leader's invite for this user
// still needs the deputy's approval.
func (r *Repository) HasActiveMembershipRestriction(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, now time.Time) (bool, error) {
	const query = `
	SELECT EXISTS (
		SELECT 1
		FROM company_membership_restrictions
		WHERE company_uuid = $1 AND user_uuid = $2 AND expires_at > $3
	)
	`

	var exists bool
	if err := r.db.QueryRowContext(ctx, query, companyID, userID, now).Scan(&exists); err != nil {
		return false, fmt.Errorf("check membership restriction: %w", err)
	}

	return exists, nil
}

// DeleteExpiredMembershipRestrictions keeps the restriction list from growing
// forever; the product keeps such records for half a year.
func (r *Repository) DeleteExpiredMembershipRestrictions(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM company_membership_restrictions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired membership restrictions: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted membership restrictions: %w", err)
	}

	return affected, nil
}

// MembershipConflictError turns the database guarantees into product errors:
// one company per employee and one deputy per company.
func MembershipConflictError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "uq_company_members_single_active_employee":
			return model.ErrCompanyMembershipConflict
		case "uq_company_members_active_deputy":
			return model.ErrCompanyDeputyAlreadyAssigned
		}
	}
	return err
}
