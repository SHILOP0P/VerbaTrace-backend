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

func (r *Repository) UpdateCompanyMemberRole(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, role model.CompanyMemberRole) (model.CompanyMember, error) {
	query := `
	UPDATE company_members
	SET role = $3
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

	row := r.db.QueryRowContext(ctx, query, companyID, userID, string(role))

	var repoMember repoModel.CompanyMember
	repoMember, err := scaner.ScanCompanyMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.CompanyMember{}, model.ErrCompanyNotFound
		}

		return model.CompanyMember{}, fmt.Errorf("update company member role: %w", err)
	}

	return converter.RepoCompanyMemberToModel(repoMember)
}
