package converter

import (
	"database/sql"
	"time"

	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func ModelInvitationToRepoInvitation(invitation model.MembershipInvitation) (repoModel.MembershipInvitation, error) {
	departmentRole := sql.NullString{}
	if invitation.DepartmentRole != nil {
		departmentRole = sql.NullString{String: string(*invitation.DepartmentRole), Valid: true}
	}

	respondedAt := sql.NullTime{}
	if invitation.RespondedAt != nil {
		respondedAt = sql.NullTime{Time: *invitation.RespondedAt, Valid: true}
	}

	return repoModel.MembershipInvitation{
		ID:                invitation.ID,
		CompanyUUID:       invitation.CompanyUUID,
		DepartmentUUID:    invitation.DepartmentUUID,
		InvitedUserUUID:   invitation.InvitedUserUUID,
		InvitedByUserUUID: invitation.InvitedByUserUUID,
		CompanyRole:       string(invitation.CompanyRole),
		DepartmentRole:    departmentRole,
		Status:            string(invitation.Status),
		ExpiresAt:         invitation.ExpiresAt,
		RespondedAt:       respondedAt,
		CreatedAt:         invitation.CreatedAt,
		UpdatedAt:         invitation.UpdatedAt,
	}, nil
}

func RepoInvitationToModel(invitation repoModel.MembershipInvitation) (model.MembershipInvitation, error) {
	var departmentRole *model.DepartmentMemberRole
	if invitation.DepartmentRole.Valid {
		role := model.DepartmentMemberRole(invitation.DepartmentRole.String)
		departmentRole = &role
	}

	var respondedAt *time.Time
	if invitation.RespondedAt.Valid {
		respondedAt = &invitation.RespondedAt.Time
	}

	return model.MembershipInvitation{
		ID:                invitation.ID,
		CompanyUUID:       invitation.CompanyUUID,
		DepartmentUUID:    invitation.DepartmentUUID,
		InvitedUserUUID:   invitation.InvitedUserUUID,
		InvitedByUserUUID: invitation.InvitedByUserUUID,
		CompanyRole:       model.CompanyMemberRole(invitation.CompanyRole),
		DepartmentRole:    departmentRole,
		Status:            model.InvitationStatus(invitation.Status),
		ExpiresAt:         invitation.ExpiresAt,
		RespondedAt:       respondedAt,
		CreatedAt:         invitation.CreatedAt,
		UpdatedAt:         invitation.UpdatedAt,
	}, nil
}

func RepoInvitationsToModels(invitations []repoModel.MembershipInvitation) ([]model.MembershipInvitation, error) {
	result := make([]model.MembershipInvitation, 0, len(invitations))
	for _, invitation := range invitations {
		item, err := RepoInvitationToModel(invitation)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	return result, nil
}
