package invitation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) GetInvitationByUUID(ctx context.Context, id uuid.UUID) (model.MembershipInvitation, error) {
	query := `
	SELECT ` + invitationColumns + `
	FROM membership_invitations
	WHERE invitation_uuid = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	repoInvitation, err := scaner.ScanInvitation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.MembershipInvitation{}, model.ErrInvitationNotFound
		}

		return model.MembershipInvitation{}, fmt.Errorf("get invitation: %w", err)
	}

	return converter.RepoInvitationToModel(repoInvitation)
}
