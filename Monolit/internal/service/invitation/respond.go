package invitation

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) AcceptInvitation(ctx context.Context, input models.AcceptInvitationInput) (models.MembershipInvitation, error) {
	if input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.InvitedUserUUID != input.RequestUser {
		return models.MembershipInvitation{}, models.ErrForbidden
	}
	if invitation.Status != models.InvitationStatusPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}
	if !invitation.ExpiresAt.After(s.now()) {
		_, err = s.invitationRepository.AcceptInvitation(ctx, input.InvitationUUID, s.now())
		if errors.Is(err, models.ErrInvitationExpired) {
			return models.MembershipInvitation{}, err
		}
		return models.MembershipInvitation{}, models.ErrInvitationExpired
	}

	if !invitation.DepartmentUUID.Valid {
		active, err := s.isActiveCompanyMember(ctx, invitation.CompanyUUID, invitation.InvitedUserUUID)
		if err != nil {
			return models.MembershipInvitation{}, err
		}
		if !active && s.billingLimiter != nil {
			if err := s.billingLimiter.CanAddCompanyMember(ctx, invitation.CompanyUUID); err != nil {
				return models.MembershipInvitation{}, err
			}
		}
	}

	return s.invitationRepository.AcceptInvitation(ctx, input.InvitationUUID, s.now())
}

func (s *Service) DeclineInvitation(ctx context.Context, input models.DeclineInvitationInput) (models.MembershipInvitation, error) {
	if input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.InvitedUserUUID != input.RequestUser {
		return models.MembershipInvitation{}, models.ErrForbidden
	}
	if invitation.Status != models.InvitationStatusPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}

	return s.invitationRepository.DeclineInvitation(ctx, input.InvitationUUID, s.now())
}

func (s *Service) CancelInvitation(ctx context.Context, input models.CancelInvitationInput) (models.MembershipInvitation, error) {
	if input.CompanyUUID == uuid.Nil || input.InvitationUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.MembershipInvitation{}, models.ErrInvalidInvitationInput
	}

	invitation, err := s.invitationRepository.GetInvitationByUUID(ctx, input.InvitationUUID)
	if err != nil {
		return models.MembershipInvitation{}, err
	}
	if invitation.CompanyUUID != input.CompanyUUID {
		return models.MembershipInvitation{}, models.ErrInvitationNotFound
	}
	if input.DepartmentUUID.Valid && (!invitation.DepartmentUUID.Valid || invitation.DepartmentUUID.UUID != input.DepartmentUUID.UUID) {
		return models.MembershipInvitation{}, models.ErrInvitationNotFound
	}
	if invitation.Status != models.InvitationStatusPending {
		return models.MembershipInvitation{}, models.ErrInvitationNotPending
	}

	if invitation.DepartmentUUID.Valid {
		if err := s.requireDepartmentCancelPermission(ctx, invitation, input.RequestUser); err != nil {
			return models.MembershipInvitation{}, err
		}
	} else if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.MembershipInvitation{}, err
	}

	return s.invitationRepository.CancelInvitation(ctx, input.InvitationUUID, s.now())
}

func (s *Service) requireDepartmentCancelPermission(ctx context.Context, invitation models.MembershipInvitation, requestUser uuid.UUID) error {
	manager, err := s.companyRepository.GetCompanyMember(ctx, invitation.CompanyUUID, requestUser)
	if err == nil && manager.Role == models.CompanyMemberRoleManager {
		return nil
	}
	if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
		return err
	}

	member, err := s.departmentRepository.GetDepartmentMember(ctx, invitation.CompanyUUID, invitation.DepartmentUUID.UUID, requestUser)
	if err != nil {
		return err
	}
	if member.Role != models.DepartmentMemberRoleLeader || invitation.InvitedByUserUUID != requestUser {
		return models.ErrForbidden
	}

	return nil
}
