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

func (r *Repository) GetManagedCompanyByUserUUID(ctx context.Context, userID uuid.UUID) (model.Company, error) {
	query := `
	SELECT company_uuid,
	       name,
	       tag,
	       manager_user_uuid,
	       member_limit,
	       created_at,
	       deleted_at
	FROM companies
	WHERE manager_user_uuid = $1
	  AND deleted_at IS NULL
	`

	row := r.db.QueryRowContext(ctx, query, userID)

	var repoCompany repoModel.Company
	repoCompany, err := scaner.ScanCompany(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Company{}, model.ErrCompanyNotFound
		}

		return model.Company{}, fmt.Errorf("get managed company by user uuid: %w", err)
	}

	return converter.RepoCompanyToModel(repoCompany)
}
