package department

import (
	"context"
	"fmt"

	model "calllens/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) ListDepartmentMembers(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) ([]model.DepartmentMember, error) {
	exists, err := r.departmentExists(ctx, companyID, departmentID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, model.ErrDepartmentNotFound
	}

	query := `
	SELECT dm.department_uuid,
	       dm.user_uuid,
	       p.username,
	       p.full_name,
	       p.full_surname,
	       cm.job_title,
	       dm.role,
	       dm.status,
	       dm.created_at
	FROM department_members dm
	JOIN departments d ON d.department_uuid = dm.department_uuid
	JOIN users u ON u.user_uuid = dm.user_uuid
	JOIN user_profiles p ON p.user_uuid = u.user_uuid
	JOIN company_members cm ON cm.company_uuid = d.company_uuid
		AND cm.user_uuid = dm.user_uuid
		AND cm.status = 'active'
	WHERE d.company_uuid = $1
	  AND dm.department_uuid = $2
	  AND dm.status = 'active'
	ORDER BY dm.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, companyID, departmentID)
	if err != nil {
		return nil, fmt.Errorf("list department members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []model.DepartmentMember
	for rows.Next() {
		var member model.DepartmentMember
		if err := rows.Scan(
			&member.DepartmentUUID,
			&member.UserUUID,
			&member.Username,
			&member.FullName,
			&member.FullSurname,
			&member.JobTitle,
			&member.Role,
			&member.Status,
			&member.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list department members: %w", err)
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list department members: %w", err)
	}

	return members, nil
}

func (r *Repository) departmentExists(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID) (bool, error) {
	query := `
	SELECT EXISTS (
		SELECT 1
		FROM departments
		WHERE company_uuid = $1
		  AND department_uuid = $2
		  AND deleted_at IS NULL
	)
	`

	var exists bool
	if err := r.db.QueryRowContext(ctx, query, companyID, departmentID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check department exists: %w", err)
	}

	return exists, nil
}
