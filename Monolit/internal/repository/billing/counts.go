package billing

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) CountOwnerCompanies(ctx context.Context, ownerID uuid.UUID) (int, error) {
	query := `
	SELECT COUNT(*)
	FROM companies
	WHERE manager_user_uuid = $1
	  AND deleted_at IS NULL
	`

	return r.count(ctx, query, ownerID)
}

func (r *Repository) CountCompanyDepartments(ctx context.Context, companyID uuid.UUID) (int, error) {
	query := `
	SELECT COUNT(*)
	FROM departments
	WHERE company_uuid = $1
	  AND deleted_at IS NULL
	`

	return r.count(ctx, query, companyID)
}

func (r *Repository) CountCompanyMembers(ctx context.Context, companyID uuid.UUID) (int, error) {
	query := `
	SELECT COUNT(*)
	FROM company_members
	WHERE company_uuid = $1
	  AND status = 'active'
	`

	return r.count(ctx, query, companyID)
}

func (r *Repository) CountActiveInstructions(ctx context.Context, input models.ListAnalysisInstructionsInput) (int, error) {
	query := `
	SELECT COUNT(*)
	FROM analysis_instructions
	WHERE is_active = true
	  AND scope = $1
	  AND (
	      ($1 = 'personal' AND user_uuid = $2)
	      OR
	      ($1 = 'company' AND company_uuid = $3 AND department_uuid IS NULL)
	      OR
	      ($1 = 'department' AND company_uuid = $3 AND department_uuid = $4)
	  )
	`

	return r.count(ctx, query, input.Scope, input.UserUUID, input.CompanyUUID, input.DepartmentUUID)
}

func (r *Repository) count(ctx context.Context, query string, args ...any) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count billing resource: %w", err)
	}

	return count, nil
}
