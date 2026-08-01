package invitation

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ListUserInvitations(ctx context.Context, input models.ListUserInvitationsInput) ([]models.MembershipInvitation, error) {
	if input.UserUUID == uuid.Nil || !validInvitationStatus(input.Status) {
		return nil, models.ErrInvalidInvitationInput
	}
	if input.Status == "" {
		input.Status = models.InvitationStatusPending
	}

	return s.invitationRepository.ListUserInvitations(ctx, input)
}
