package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanInvitation(row rowScanner) (repoModel.MembershipInvitation, error) {
	var invitation repoModel.MembershipInvitation

	err := row.Scan(
		&invitation.ID,
		&invitation.CompanyUUID,
		&invitation.DepartmentUUID,
		&invitation.InvitedUserUUID,
		&invitation.InvitedByUserUUID,
		&invitation.CompanyRole,
		&invitation.DepartmentRole,
		&invitation.Status,
		&invitation.ExpiresAt,
		&invitation.RespondedAt,
		&invitation.CreatedAt,
		&invitation.UpdatedAt,
	)
	if err != nil {
		return repoModel.MembershipInvitation{}, err
	}

	return invitation, nil
}
