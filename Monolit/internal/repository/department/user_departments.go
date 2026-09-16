package department

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// ListUserDepartments returns the active department memberships of a user inside
// one company. Membership rules and invitation alerts both need to know whether
// the person already belongs somewhere.
func (r *Repository) ListUserDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]model.CompanyMemberDepartment, error) {
	const query = `
	SELECT d.department_uuid,
	       d.name,
	       dm.role,
	       dm.status
	FROM department_members dm
	JOIN departments d ON d.department_uuid = dm.department_uuid
	WHERE d.company_uuid = $1
	  AND dm.user_uuid = $2
	  AND dm.status = 'active'
	  AND d.deleted_at IS NULL
	ORDER BY d.name
	`

	rows, err := r.db.QueryContext(ctx, query, companyID, userID)
	if err != nil {
		return nil, fmt.Errorf("list user departments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	departments := []model.CompanyMemberDepartment{}
	for rows.Next() {
		var item model.CompanyMemberDepartment
		if err := rows.Scan(&item.DepartmentUUID, &item.DepartmentName, &item.Role, &item.Status); err != nil {
			return nil, fmt.Errorf("scan user department: %w", err)
		}
		departments = append(departments, item)
	}

	return departments, rows.Err()
}
