package company

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) GetCompanyMembersOverview(ctx context.Context, companyID uuid.UUID) (model.CompanyMembersOverview, error) {
	companyMembers, err := r.listActiveCompanyMembers(ctx, companyID)
	if err != nil {
		return model.CompanyMembersOverview{}, err
	}

	departments, err := r.listCompanyDepartments(ctx, companyID)
	if err != nil {
		return model.CompanyMembersOverview{}, err
	}

	departmentMembers, err := r.listActiveDepartmentMembers(ctx, companyID)
	if err != nil {
		return model.CompanyMembersOverview{}, err
	}

	membersByDepartment := make(map[uuid.UUID][]model.DepartmentMember, len(departments))
	for _, member := range departmentMembers {
		membersByDepartment[member.DepartmentUUID] = append(membersByDepartment[member.DepartmentUUID], member)
	}

	overview := model.CompanyMembersOverview{
		CompanyUUID: companyID,
		Departments: make([]model.DepartmentMembersOverview, 0, len(departments)),
	}

	for _, member := range companyMembers {
		switch member.Role {
		case model.CompanyMemberRoleManager:
			memberCopy := member
			overview.Manager = &memberCopy
		case model.CompanyMemberRoleEmployee:
			overview.CompanyEmployees = append(overview.CompanyEmployees, member)
		}
	}

	for _, department := range departments {
		overview.Departments = append(overview.Departments, model.DepartmentMembersOverview{
			Department: department,
			Members:    membersByDepartment[department.ID],
		})
	}

	return overview, nil
}

func (r *Repository) listActiveCompanyMembers(ctx context.Context, companyID uuid.UUID) ([]model.CompanyMember, error) {
	query := `
	SELECT cm.company_uuid,
	       cm.user_uuid,
	       p.username,
	       p.full_name,
	       p.full_surname,
	       cm.job_title,
	       cm.role,
	       cm.status,
	       cm.created_at
	FROM company_members cm
	JOIN users u ON u.user_uuid = cm.user_uuid
	JOIN user_profiles p ON p.user_uuid = u.user_uuid
	WHERE cm.company_uuid = $1
	  AND cm.status = 'active'
	ORDER BY cm.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, companyID)
	if err != nil {
		return nil, fmt.Errorf("list active company members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []model.CompanyMember
	for rows.Next() {
		var member model.CompanyMember
		if err = rows.Scan(
			&member.CompanyUUID,
			&member.UserUUID,
			&member.Username,
			&member.FullName,
			&member.FullSurname,
			&member.JobTitle,
			&member.Role,
			&member.Status,
			&member.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list active company members: %w", err)
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active company members: %w", err)
	}

	return members, nil
}

func (r *Repository) listCompanyDepartments(ctx context.Context, companyID uuid.UUID) ([]model.Department, error) {
	query := `
	SELECT department_uuid,
	       company_uuid,
	       name,
	       created_at,
	       deleted_at
	FROM departments
	WHERE company_uuid = $1
	  AND deleted_at IS NULL
	ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, companyID)
	if err != nil {
		return nil, fmt.Errorf("list company departments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var departments []model.Department
	for rows.Next() {
		var repoDepartment repoModel.Department
		repoDepartment, err = scaner.ScanDepartment(rows)
		if err != nil {
			return nil, fmt.Errorf("list company departments: %w", err)
		}

		departments = append(departments, model.Department{
			ID:          repoDepartment.ID,
			CompanyUUID: repoDepartment.CompanyUUID,
			Name:        repoDepartment.Name,
			CreatedAt:   repoDepartment.CreatedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list company departments: %w", err)
	}

	return departments, nil
}

func (r *Repository) listActiveDepartmentMembers(ctx context.Context, companyID uuid.UUID) ([]model.DepartmentMember, error) {
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
	  AND d.deleted_at IS NULL
	  AND dm.status = 'active'
	ORDER BY dm.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, companyID)
	if err != nil {
		return nil, fmt.Errorf("list active department members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []model.DepartmentMember
	for rows.Next() {
		var member model.DepartmentMember
		if err = rows.Scan(
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
			return nil, fmt.Errorf("list active department members: %w", err)
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active department members: %w", err)
	}

	return members, nil
}
