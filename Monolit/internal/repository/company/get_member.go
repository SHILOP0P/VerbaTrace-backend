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

func (r *Repository) GetCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (model.CompanyMember, error) {
	query := `
	SELECT company_uuid,
	       user_uuid,
	       role,
	       status,
	       created_at
	FROM company_members
	WHERE company_uuid = $1
	  AND user_uuid = $2
	  AND status = 'active'
	`

	row := r.db.QueryRowContext(ctx, query, companyID, userID)

	var repoMember repoModel.CompanyMember
	repoMember, err := scaner.ScanCompanyMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyMember{}, model.ErrCompanyNotFound
		}

		return model.CompanyMember{}, fmt.Errorf("get company member: %w", err)
	}

	return converter.RepoCompanyMemberToModel(repoMember)
}
