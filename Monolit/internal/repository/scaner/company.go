package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanCompany(row rowScanner) (repoModel.Company, error) {
	var company repoModel.Company

	err := row.Scan(
		&company.ID,
		&company.Name,
		&company.Tag,
		&company.ManagerUserUUID,
		&company.MemberLimit,
		&company.CreatedAt,
		&company.DeletedAt,
	)
	if err != nil {
		return repoModel.Company{}, err
	}

	return company, nil
}

func ScanCompanyMember(row rowScanner) (repoModel.CompanyMember, error) {
	var member repoModel.CompanyMember

	err := row.Scan(
		&member.CompanyUUID,
		&member.UserUUID,
		&member.Role,
		&member.Status,
		&member.CreatedAt,
	)
	if err != nil {
		return repoModel.CompanyMember{}, err
	}

	return member, nil
}
