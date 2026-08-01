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

func (r *Repository) GetCompanyByUUID(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (model.Company, error) {
	query := `
	SELECT c.company_uuid,
	       c.name,
	       c.tag,
	       c.manager_user_uuid,
	       c.member_limit,
	       c.created_at,
	       c.deleted_at
	FROM companies c
	JOIN company_members cm ON cm.company_uuid = c.company_uuid
	WHERE c.company_uuid = $1
	  AND cm.user_uuid = $2
	  AND cm.status = 'active'
	  AND c.deleted_at IS NULL
	`

	row := r.db.QueryRowContext(ctx, query, companyID, userID)

	var repoCompany repoModel.Company
	repoCompany, err := scaner.ScanCompany(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Company{}, model.ErrCompanyNotFound
		}

		return model.Company{}, fmt.Errorf("get company by uuid: %w", err)
	}

	return converter.RepoCompanyToModel(repoCompany)
}
