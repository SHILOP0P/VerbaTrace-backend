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
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) UpdateCompany(ctx context.Context, companyID uuid.UUID, name string) (model.Company, error) {
	query := `
	UPDATE companies
	SET name = $2
	WHERE company_uuid = $1
	  AND deleted_at IS NULL
	RETURNING company_uuid,
	          name,
	          tag,
	          manager_user_uuid,
	          member_limit,
	          created_at,
	          deleted_at
	`

	row := r.db.QueryRowContext(ctx, query, companyID, name)

	var repoCompany repoModel.Company
	repoCompany, err := scaner.ScanCompany(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Company{}, model.ErrCompanyNotFound
		}

		return model.Company{}, fmt.Errorf("update company: %w", err)
	}

	return converter.RepoCompanyToModel(repoCompany)
}

func (r *Repository) UpdateCompanyTag(ctx context.Context, companyID uuid.UUID, tag string) (model.Company, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE companies SET tag=$2
		WHERE company_uuid=$1 AND deleted_at IS NULL
		RETURNING company_uuid,name,tag,manager_user_uuid,member_limit,created_at,deleted_at`, companyID, tag)
	company, err := scaner.ScanCompany(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Company{}, model.ErrCompanyNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.Company{}, model.ErrCompanyTagAlreadyExists
		}
		return model.Company{}, fmt.Errorf("update company tag: %w", err)
	}
	return converter.RepoCompanyToModel(company)
}

func (r *Repository) ArchiveCompany(ctx context.Context, companyID uuid.UUID) error {
	query := `
	UPDATE companies
	SET deleted_at = now()
	WHERE company_uuid = $1
	  AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, companyID)
	if err != nil {
		return fmt.Errorf("archive company: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("archive company rows affected: %w", err)
	}
	if rows == 0 {
		return model.ErrCompanyNotFound
	}

	return nil
}
