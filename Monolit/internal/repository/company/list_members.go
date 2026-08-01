package company

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) ListCompanyMembers(ctx context.Context, input model.ListCompanyMembersInput) (model.CompanyMembersResult, error) {
	where, args := companyMembersWhere(input)

	totalQuery := `
	SELECT COUNT(DISTINCT cm.user_uuid)
	FROM company_members cm
	JOIN companies c ON c.company_uuid = cm.company_uuid
	JOIN users u ON u.user_uuid = cm.user_uuid
	JOIN user_profiles p ON p.user_uuid = u.user_uuid
	` + where

	var total int
	if err := r.db.QueryRowContext(ctx, totalQuery, args...).Scan(&total); err != nil {
		return model.CompanyMembersResult{}, fmt.Errorf("count company members: %w", err)
	}

	listArgs := append(args, input.Limit, input.Offset)
	listQuery := `
	WITH filtered_members AS (
		SELECT cm.company_uuid,
		       cm.user_uuid,
		       u.email,
		       p.username,
		       p.full_name,
		       p.full_surname,
		       cm.job_title,
		       cm.role,
		       cm.status,
		       cm.created_at
		FROM company_members cm
		JOIN companies c ON c.company_uuid = cm.company_uuid
		JOIN users u ON u.user_uuid = cm.user_uuid
		JOIN user_profiles p ON p.user_uuid = u.user_uuid
		` + where + `
		ORDER BY cm.created_at ASC, cm.user_uuid ASC
		LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2) + `
	)
	SELECT fm.company_uuid,
	       fm.user_uuid,
	       fm.email,
	       fm.username,
	       fm.full_name,
	       fm.full_surname,
	       fm.job_title,
	       fm.role,
	       fm.status,
	       fm.created_at,
	       d.department_uuid,
	       d.name,
	       dm.role,
	       dm.status
	FROM filtered_members fm
	LEFT JOIN department_members dm ON dm.user_uuid = fm.user_uuid
	LEFT JOIN departments d ON d.department_uuid = dm.department_uuid
	    AND d.company_uuid = fm.company_uuid
	    AND d.deleted_at IS NULL
	ORDER BY fm.created_at ASC, fm.user_uuid ASC, d.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return model.CompanyMembersResult{}, fmt.Errorf("list company members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	members := make([]model.CompanyMemberListItem, 0, input.Limit)
	memberIndexes := make(map[string]int)
	for rows.Next() {
		var item model.CompanyMemberListItem
		var companyID uuid.UUID
		var departmentID sql.NullString
		var departmentName sql.NullString
		var departmentRole sql.NullString
		var departmentStatus sql.NullString

		if err := rows.Scan(
			&companyID,
			&item.UserUUID,
			&item.Email,
			&item.Username,
			&item.FullName,
			&item.FullSurname,
			&item.JobTitle,
			&item.CompanyRole,
			&item.Status,
			&item.CreatedAt,
			&departmentID,
			&departmentName,
			&departmentRole,
			&departmentStatus,
		); err != nil {
			return model.CompanyMembersResult{}, fmt.Errorf("list company members: %w", err)
		}

		key := item.UserUUID.String()
		index, ok := memberIndexes[key]
		if !ok {
			item.Departments = []model.CompanyMemberDepartment{}
			members = append(members, item)
			index = len(members) - 1
			memberIndexes[key] = index
		}

		if departmentID.Valid {
			parsedDepartmentID, err := uuid.Parse(departmentID.String)
			if err != nil {
				return model.CompanyMembersResult{}, fmt.Errorf("list company members department uuid: %w", err)
			}
			members[index].Departments = append(members[index].Departments, model.CompanyMemberDepartment{
				DepartmentUUID: parsedDepartmentID,
				DepartmentName: departmentName.String,
				Role:           model.DepartmentMemberRole(departmentRole.String),
				Status:         model.MembershipStatus(departmentStatus.String),
			})
		}
	}

	if err := rows.Err(); err != nil {
		return model.CompanyMembersResult{}, fmt.Errorf("list company members: %w", err)
	}

	return model.CompanyMembersResult{
		Members: members,
		Total:   total,
		Limit:   input.Limit,
		Offset:  input.Offset,
	}, nil
}

func companyMembersWhere(input model.ListCompanyMembersInput) (string, []any) {
	conditions := []string{
		"WHERE cm.company_uuid = $1",
		"AND c.deleted_at IS NULL",
	}
	args := []any{input.CompanyUUID}

	if input.Status != nil {
		args = append(args, string(*input.Status))
		conditions = append(conditions, fmt.Sprintf("AND cm.status = $%d", len(args)))
	}

	if input.Role != nil {
		args = append(args, *input.Role)
		if *input.Role == string(model.DepartmentMemberRoleLeader) {
			conditions = append(conditions, fmt.Sprintf(`AND EXISTS (
				SELECT 1
				FROM department_members role_dm
				JOIN departments role_d ON role_d.department_uuid = role_dm.department_uuid
				WHERE role_d.company_uuid = cm.company_uuid
				  AND role_d.deleted_at IS NULL
				  AND role_dm.user_uuid = cm.user_uuid
				  AND role_dm.role = $%d
				  AND role_dm.status = 'active'
			)`, len(args)))
		} else {
			conditions = append(conditions, fmt.Sprintf("AND cm.role = $%d", len(args)))
		}
	}

	if input.DepartmentUUID != uuid.Nil {
		args = append(args, input.DepartmentUUID)
		conditions = append(conditions, fmt.Sprintf(`AND EXISTS (
			SELECT 1
			FROM department_members filter_dm
			JOIN departments filter_d ON filter_d.department_uuid = filter_dm.department_uuid
			WHERE filter_d.company_uuid = cm.company_uuid
			  AND filter_d.deleted_at IS NULL
			  AND filter_dm.department_uuid = $%d
			  AND filter_dm.user_uuid = cm.user_uuid
			  AND filter_dm.status = 'active'
		)`, len(args)))
	}

	if input.Query != "" {
		args = append(args, "%"+strings.ToLower(input.Query)+"%")
		conditions = append(conditions, fmt.Sprintf(`AND (
			lower(p.username) LIKE $%d OR
			lower(p.full_name) LIKE $%d OR
			lower(p.full_surname) LIKE $%d OR
			lower(u.email) LIKE $%d
		)`, len(args), len(args), len(args), len(args)))
	}

	return strings.Join(conditions, "\n"), args
}
