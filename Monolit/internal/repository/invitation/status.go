package invitation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) DeclineInvitation(ctx context.Context, id uuid.UUID, now time.Time) (model.MembershipInvitation, error) {
	return r.updatePendingStatus(ctx, id, model.InvitationStatusDeclined, now)
}

func (r *Repository) CancelInvitation(ctx context.Context, id uuid.UUID, now time.Time) (model.MembershipInvitation, error) {
	return r.updatePendingStatus(ctx, id, model.InvitationStatusCanceled, now)
}

func (r *Repository) updatePendingStatus(ctx context.Context, id uuid.UUID, status model.InvitationStatus, now time.Time) (model.MembershipInvitation, error) {
	query := `
	UPDATE membership_invitations
	SET status = $2,
	    responded_at = $3,
	    updated_at = $3
	WHERE invitation_uuid = $1
	  AND status = 'pending'
	RETURNING ` + invitationColumns

	row := r.db.QueryRowContext(ctx, query, id, string(status), now)
	repoInvitation, err := scaner.ScanInvitation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, getErr := r.GetInvitationByUUID(ctx, id)
			if errors.Is(getErr, model.ErrInvitationNotFound) {
				return model.MembershipInvitation{}, model.ErrInvitationNotFound
			}
			if getErr != nil {
				return model.MembershipInvitation{}, getErr
			}
			return model.MembershipInvitation{}, model.ErrInvitationNotPending
		}

		return model.MembershipInvitation{}, fmt.Errorf("update invitation status: %w", err)
	}

	return converter.RepoInvitationToModel(repoInvitation)
}
