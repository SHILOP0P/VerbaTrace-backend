package invitation

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) ListUserInvitations(ctx context.Context, input model.ListUserInvitationsInput) ([]model.MembershipInvitation, error) {
	query := `
	SELECT ` + invitationColumns + `
	FROM membership_invitations
	WHERE invited_user_uuid = $1
	  AND ($2 = '' OR status = $2)
	  AND ($2 <> 'pending' OR expires_at > now())
	ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, input.UserUUID, string(input.Status))
	if err != nil {
		return nil, fmt.Errorf("list user invitations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	invitations, err := scanInvitations(rows)
	if err != nil {
		return nil, fmt.Errorf("list user invitations: %w", err)
	}

	return converter.RepoInvitationsToModels(invitations)
}

func (r *Repository) ListCompanyInvitations(ctx context.Context, companyID uuid.UUID, status model.InvitationStatus) ([]model.MembershipInvitation, error) {
	query := `
	SELECT ` + invitationColumns + `
	FROM membership_invitations
	WHERE company_uuid = $1
	  AND ($2 = '' OR status = $2)
	ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, companyID, string(status))
	if err != nil {
		return nil, fmt.Errorf("list company invitations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	invitations, err := scanInvitations(rows)
	if err != nil {
		return nil, fmt.Errorf("list company invitations: %w", err)
	}

	return converter.RepoInvitationsToModels(invitations)
}

func scanInvitations(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]repoModel.MembershipInvitation, error) {
	var invitations []repoModel.MembershipInvitation
	for rows.Next() {
		invitation, err := scaner.ScanInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}

	return invitations, rows.Err()
}
