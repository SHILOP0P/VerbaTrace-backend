package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) UpdateCompanyMemberStatus(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, status model.MembershipStatus) (model.CompanyMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CompanyMember{}, fmt.Errorf("begin update company member status transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `
	UPDATE company_members
	SET status = $3
	WHERE company_uuid = $1
	  AND user_uuid = $2
	  AND role <> 'company_manager'
	  AND status = 'active'
	RETURNING company_uuid,
	          user_uuid,
	          role,
	          status,
	          created_at
	`

	row := tx.QueryRowContext(ctx, query, companyID, userID, string(status))

	var repoMember repoModel.CompanyMember
	repoMember, err = scaner.ScanCompanyMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyMember{}, model.ErrCompanyNotFound
		}

		return model.CompanyMember{}, fmt.Errorf("update company member status: %w", err)
	}

	if status != model.MembershipStatusActive {
		departmentQuery := `
		UPDATE department_members dm
		SET status = $3
		FROM departments d
		WHERE d.department_uuid = dm.department_uuid
		  AND d.company_uuid = $1
		  AND dm.user_uuid = $2
		`
		if _, err := tx.ExecContext(ctx, departmentQuery, companyID, userID, string(status)); err != nil {
			return model.CompanyMember{}, fmt.Errorf("update company member department statuses: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.CompanyMember{}, fmt.Errorf("commit update company member status transaction: %w", err)
	}

	return converter.RepoCompanyMemberToModel(repoMember)
}

func (r *Repository) CountActiveCompanyManagers(ctx context.Context, companyID uuid.UUID, exceptUserID uuid.UUID) (int, error) {
	query := `
	SELECT COUNT(*)
	FROM company_members
	WHERE company_uuid = $1
	  AND role = 'company_manager'
	  AND status = 'active'
	  AND user_uuid <> $2
	`

	var count int
	if err := r.db.QueryRowContext(ctx, query, companyID, exceptUserID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active company managers: %w", err)
	}

	return count, nil
}
