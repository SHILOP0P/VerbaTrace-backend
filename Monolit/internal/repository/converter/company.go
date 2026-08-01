package converter

import (
	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoCompanyToModel(repoCompany repoModel.Company) (model.Company, error) {
	return model.Company{
		ID:              repoCompany.ID,
		Name:            repoCompany.Name,
		Tag:             repoCompany.Tag,
		ManagerUserUUID: repoCompany.ManagerUserUUID,
		MemberLimit:     repoCompany.MemberLimit,
		CreatedAt:       repoCompany.CreatedAt,
		DeletedAt:       repoCompany.DeletedAt,
	}, nil
}

func RepoCompaniesToModels(repoCompanies []repoModel.Company) ([]model.Company, error) {
	result := make([]model.Company, len(repoCompanies))
	for i, company := range repoCompanies {
		result[i], _ = RepoCompanyToModel(company)
	}

	return result, nil
}

func ModelCompanyToRepoCompany(modelCompany model.Company) (repoModel.Company, error) {
	return repoModel.Company{
		ID:              modelCompany.ID,
		Name:            modelCompany.Name,
		Tag:             modelCompany.Tag,
		ManagerUserUUID: modelCompany.ManagerUserUUID,
		MemberLimit:     modelCompany.MemberLimit,
		CreatedAt:       modelCompany.CreatedAt,
		DeletedAt:       modelCompany.DeletedAt,
	}, nil
}

func ModelCompanyMemberToRepoCompanyMember(modelMember model.CompanyMember) (repoModel.CompanyMember, error) {
	return repoModel.CompanyMember{
		CompanyUUID: modelMember.CompanyUUID,
		UserUUID:    modelMember.UserUUID,
		Role:        string(modelMember.Role),
		Status:      string(modelMember.Status),
		CreatedAt:   modelMember.CreatedAt,
	}, nil
}

func RepoCompanyMemberToModel(repoMember repoModel.CompanyMember) (model.CompanyMember, error) {
	return model.CompanyMember{
		CompanyUUID: repoMember.CompanyUUID,
		UserUUID:    repoMember.UserUUID,
		Role:        model.CompanyMemberRole(repoMember.Role),
		Status:      model.MembershipStatus(repoMember.Status),
		CreatedAt:   repoMember.CreatedAt,
	}, nil
}
