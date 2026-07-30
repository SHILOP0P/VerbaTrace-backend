package converter

import (
	"time"

	"calllens/monolit/internal/API/dto"
	"calllens/monolit/internal/models"
)

func CompanyModelToAPI(company models.Company) (dto.CompanyResponse, error) {
	tag := company.Tag
	if tag == "" {
		tag = "@" + company.ID.String()
	}
	return dto.CompanyResponse{
		ID:              company.ID.String(),
		Name:            company.Name,
		Tag:             tag,
		ManagerUserUUID: company.ManagerUserUUID.String(),
		MemberLimit:     company.MemberLimit,
		CreatedAt:       company.CreatedAt.Format(time.RFC3339),
	}, nil
}

func DepartmentModelToAPI(department models.Department) (dto.DepartmentResponse, error) {
	return dto.DepartmentResponse{
		ID:          department.ID.String(),
		CompanyUUID: department.CompanyUUID.String(),
		Name:        department.Name,
		CreatedAt:   department.CreatedAt.Format(time.RFC3339),
	}, nil
}

func CompanyMemberModelToAPI(member models.CompanyMember) (dto.CompanyMemberResponse, error) {
	return dto.CompanyMemberResponse{
		CompanyUUID: member.CompanyUUID.String(),
		UserUUID:    member.UserUUID.String(),
		Username:    member.Username,
		FullName:    member.FullName,
		FullSurname: member.FullSurname,
		JobTitle:    member.JobTitle,
		Role:        string(member.Role),
		Status:      string(member.Status),
		CreatedAt:   member.CreatedAt.Format(time.RFC3339),
	}, nil
}

func DepartmentMemberModelToAPI(member models.DepartmentMember) (dto.DepartmentMemberResponse, error) {
	return dto.DepartmentMemberResponse{
		DepartmentUUID: member.DepartmentUUID.String(),
		UserUUID:       member.UserUUID.String(),
		Username:       member.Username,
		FullName:       member.FullName,
		FullSurname:    member.FullSurname,
		JobTitle:       member.JobTitle,
		Role:           string(member.Role),
		Status:         string(member.Status),
		CreatedAt:      member.CreatedAt.Format(time.RFC3339),
	}, nil
}

func CompanyMembersOverviewModelToAPI(overview models.CompanyMembersOverview) (dto.CompanyMembersOverviewResponse, error) {
	resp := dto.CompanyMembersOverviewResponse{
		CompanyUUID:      overview.CompanyUUID.String(),
		CompanyEmployees: make([]dto.CompanyMemberResponse, 0, len(overview.CompanyEmployees)),
		Departments:      make([]dto.DepartmentMembersOverviewResponse, 0, len(overview.Departments)),
	}

	if overview.Manager != nil {
		manager, err := CompanyMemberModelToAPI(*overview.Manager)
		if err != nil {
			return dto.CompanyMembersOverviewResponse{}, err
		}
		resp.Manager = &manager
	}

	for _, member := range overview.CompanyEmployees {
		memberResponse, err := CompanyMemberModelToAPI(member)
		if err != nil {
			return dto.CompanyMembersOverviewResponse{}, err
		}
		resp.CompanyEmployees = append(resp.CompanyEmployees, memberResponse)
	}

	for _, department := range overview.Departments {
		departmentResponse, err := DepartmentModelToAPI(department.Department)
		if err != nil {
			return dto.CompanyMembersOverviewResponse{}, err
		}

		departmentOverview := dto.DepartmentMembersOverviewResponse{
			Department: departmentResponse,
			Members:    make([]dto.DepartmentMemberResponse, 0, len(department.Members)),
		}

		for _, member := range department.Members {
			memberResponse, err := DepartmentMemberModelToAPI(member)
			if err != nil {
				return dto.CompanyMembersOverviewResponse{}, err
			}
			departmentOverview.Members = append(departmentOverview.Members, memberResponse)
		}

		resp.Departments = append(resp.Departments, departmentOverview)
	}

	return resp, nil
}

func CompanyMembersResultModelToAPI(result models.CompanyMembersResult) (dto.CompanyMembersResponse, error) {
	resp := dto.CompanyMembersResponse{
		Members: make([]dto.CompanyMemberListItemResponse, 0, len(result.Members)),
		Total:   result.Total,
		Limit:   result.Limit,
		Offset:  result.Offset,
	}

	for _, member := range result.Members {
		item := dto.CompanyMemberListItemResponse{
			UserUUID:    member.UserUUID.String(),
			Email:       member.Email,
			Username:    member.Username,
			FullName:    member.FullName,
			FullSurname: member.FullSurname,
			JobTitle:    member.JobTitle,
			CompanyRole: string(member.CompanyRole),
			Status:      string(member.Status),
			Departments: make([]dto.CompanyMemberDepartmentResponse, 0, len(member.Departments)),
			CreatedAt:   member.CreatedAt.Format(time.RFC3339),
		}

		for _, department := range member.Departments {
			item.Departments = append(item.Departments, dto.CompanyMemberDepartmentResponse{
				DepartmentUUID: department.DepartmentUUID.String(),
				DepartmentName: department.DepartmentName,
				Role:           string(department.Role),
				Status:         string(department.Status),
			})
		}

		resp.Members = append(resp.Members, item)
	}

	return resp, nil
}

func InvitationModelToAPI(invitation models.MembershipInvitation) (dto.InvitationResponse, error) {
	var departmentUUID *string
	if invitation.DepartmentUUID.Valid {
		value := invitation.DepartmentUUID.UUID.String()
		departmentUUID = &value
	}

	var departmentRole *string
	if invitation.DepartmentRole != nil {
		value := string(*invitation.DepartmentRole)
		departmentRole = &value
	}

	var respondedAt *string
	if invitation.RespondedAt != nil {
		value := invitation.RespondedAt.Format(time.RFC3339)
		respondedAt = &value
	}

	return dto.InvitationResponse{
		ID:                invitation.ID.String(),
		CompanyUUID:       invitation.CompanyUUID.String(),
		DepartmentUUID:    departmentUUID,
		InvitedUserUUID:   invitation.InvitedUserUUID.String(),
		InvitedByUserUUID: invitation.InvitedByUserUUID.String(),
		CompanyRole:       string(invitation.CompanyRole),
		DepartmentRole:    departmentRole,
		Status:            string(invitation.Status),
		ExpiresAt:         invitation.ExpiresAt.Format(time.RFC3339),
		RespondedAt:       respondedAt,
		CreatedAt:         invitation.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         invitation.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func InvitationsModelToAPI(invitations []models.MembershipInvitation) ([]dto.InvitationResponse, error) {
	result := make([]dto.InvitationResponse, 0, len(invitations))
	for _, invitation := range invitations {
		item, err := InvitationModelToAPI(invitation)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	return result, nil
}
