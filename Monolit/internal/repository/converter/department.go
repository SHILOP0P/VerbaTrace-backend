package converter

import (
	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoDepartmentToModel(repoDepartment repoModel.Department) (model.Department, error) {
	return model.Department{
		ID:          repoDepartment.ID,
		CompanyUUID: repoDepartment.CompanyUUID,
		Name:        repoDepartment.Name,
		CreatedAt:   repoDepartment.CreatedAt,
		DeletedAt:   repoDepartment.DeletedAt,
	}, nil
}

func RepoDepartmentsToModels(repoDepartments []repoModel.Department) ([]model.Department, error) {
	result := make([]model.Department, len(repoDepartments))
	for i, department := range repoDepartments {
		result[i], _ = RepoDepartmentToModel(department)
	}

	return result, nil
}

func ModelDepartmentToRepoDepartment(modelDepartment model.Department) (repoModel.Department, error) {
	return repoModel.Department{
		ID:          modelDepartment.ID,
		CompanyUUID: modelDepartment.CompanyUUID,
		Name:        modelDepartment.Name,
		CreatedAt:   modelDepartment.CreatedAt,
		DeletedAt:   modelDepartment.DeletedAt,
	}, nil
}

func RepoDepartmentMemberToModel(repoMember repoModel.DepartmentMember) (model.DepartmentMember, error) {
	return model.DepartmentMember{
		DepartmentUUID: repoMember.DepartmentUUID,
		UserUUID:       repoMember.UserUUID,
		Role:           model.DepartmentMemberRole(repoMember.Role),
		Status:         model.MembershipStatus(repoMember.Status),
		CreatedAt:      repoMember.CreatedAt,
	}, nil
}

func ModelDepartmentMemberToRepoDepartmentMember(modelMember model.DepartmentMember) (repoModel.DepartmentMember, error) {
	return repoModel.DepartmentMember{
		DepartmentUUID: modelMember.DepartmentUUID,
		UserUUID:       modelMember.UserUUID,
		Role:           string(modelMember.Role),
		Status:         string(modelMember.Status),
		CreatedAt:      modelMember.CreatedAt,
	}, nil
}
