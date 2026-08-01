package department

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) ListVisibleCompanyDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]model.Department, error) {
	query := `
	SELECT d.department_uuid,
	       d.company_uuid,
	       d.name,
	       d.created_at,
	       d.deleted_at
	FROM departments d
	WHERE d.company_uuid = $1
	  AND d.deleted_at IS NULL
	  AND (
	      EXISTS (
	          SELECT 1
	          FROM company_members cm
	          JOIN companies c ON c.company_uuid = cm.company_uuid
	          WHERE cm.company_uuid = d.company_uuid
	            AND cm.user_uuid = $2
	            AND cm.role = 'company_manager'
	            AND cm.status = 'active'
	            AND c.deleted_at IS NULL
	      )
	      OR EXISTS (
	          SELECT 1
	          FROM department_members dm
	          WHERE dm.department_uuid = d.department_uuid
	            AND dm.user_uuid = $2
	            AND dm.status = 'active'
	      )
	  )
	ORDER BY d.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, companyID, userID)
	if err != nil {
		return nil, fmt.Errorf("list visible company departments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var departments []repoModel.Department
	for rows.Next() {
		department, err := scaner.ScanDepartment(rows)
		if err != nil {
			return nil, fmt.Errorf("list visible company departments: %w", err)
		}

		departments = append(departments, department)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list visible company departments: %w", err)
	}

	return converter.RepoDepartmentsToModels(departments)
}
