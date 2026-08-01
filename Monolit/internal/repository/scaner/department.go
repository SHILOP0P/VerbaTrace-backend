package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanDepartment(row rowScanner) (repoModel.Department, error) {
	var department repoModel.Department

	err := row.Scan(
		&department.ID,
		&department.CompanyUUID,
		&department.Name,
		&department.CreatedAt,
		&department.DeletedAt,
	)
	if err != nil {
		return repoModel.Department{}, err
	}

	return department, nil
}

func ScanDepartmentMember(row rowScanner) (repoModel.DepartmentMember, error) {
	var member repoModel.DepartmentMember

	err := row.Scan(
		&member.DepartmentUUID,
		&member.UserUUID,
		&member.Role,
		&member.Status,
		&member.CreatedAt,
	)
	if err != nil {
		return repoModel.DepartmentMember{}, err
	}

	return member, nil
}
