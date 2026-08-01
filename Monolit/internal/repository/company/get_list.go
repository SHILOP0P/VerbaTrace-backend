package company

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) ListUserCompanies(ctx context.Context, userID uuid.UUID) ([]model.Company, error) {
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
	WHERE cm.user_uuid = $1
	  AND cm.status = 'active'
	  AND c.deleted_at IS NULL
	ORDER BY c.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list user companies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var companies []repoModel.Company
	for rows.Next() {
		company, err := scaner.ScanCompany(rows)
		if err != nil {
			return nil, fmt.Errorf("list user companies: %w", err)
		}

		companies = append(companies, company)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user companies: %w", err)
	}

	return converter.RepoCompaniesToModels(companies)
}
