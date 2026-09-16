package company

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ownershipTransferTTL keeps an unanswered offer from hanging over the company.
const ownershipTransferTTL = 7 * 24 * time.Hour

// OfferOwnership proposes the company to another active member. Ownership moves
// only when that person accepts, and the previous owner stays as the deputy.
func (s *Service) OfferOwnership(ctx context.Context, input models.CreateCompanyOwnershipTransferInput) (models.CompanyOwnershipTransfer, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.ToUserUUID == uuid.Nil {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}
	if input.RequestUser == input.ToUserUUID {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyOwnershipTransfer{}, err
	}

	target, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.ToUserUUID)
	if err != nil {
		return models.CompanyOwnershipTransfer{}, err
	}
	if target.Role == models.CompanyMemberRoleManager {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}

	now := time.Now().UTC()
	transfer := models.CompanyOwnershipTransfer{
		ID:           uuid.New(),
		CompanyUUID:  input.CompanyUUID,
		FromUserUUID: input.RequestUser,
		ToUserUUID:   input.ToUserUUID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ownershipTransferTTL),
	}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		transfer.Reason = &reason
	}

	created, err := s.companyRepository.CreateOwnershipTransfer(ctx, transfer)
	if err != nil {
		s.log.Error(ctx, "failed to offer company ownership", zap.String("company_id", input.CompanyUUID.String()), zap.Error(err))
		return models.CompanyOwnershipTransfer{}, err
	}

	s.notify(ctx, input.ToUserUUID, models.NotificationTypeCompanyOwnerTransferAsked, "Вам предлагают стать владельцем компании", "Подтвердите или отклоните передачу", "company", input.CompanyUUID)

	return created, nil
}

// DecideOwnership is the answer of the invited owner.
func (s *Service) DecideOwnership(ctx context.Context, input models.DecideCompanyOwnershipTransferInput) (models.CompanyOwnershipTransfer, error) {
	if input.TransferUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}

	transfer, err := s.companyRepository.GetOwnershipTransfer(ctx, input.TransferUUID)
	if err != nil {
		return models.CompanyOwnershipTransfer{}, err
	}
	if transfer.ToUserUUID != input.RequestUser {
		return models.CompanyOwnershipTransfer{}, models.ErrForbidden
	}
	if transfer.Status != models.CompanyOwnershipTransferPending {
		return models.CompanyOwnershipTransfer{}, models.ErrCompanyOwnershipTransferNotFound
	}

	now := time.Now().UTC()
	if !input.Accept {
		declined, err := s.companyRepository.CloseOwnershipTransfer(ctx, transfer.ID, models.CompanyOwnershipTransferDeclined, now)
		if err != nil {
			return models.CompanyOwnershipTransfer{}, err
		}
		s.notify(ctx, transfer.FromUserUUID, models.NotificationTypeCompanyOwnerTransferDecided, "Передача компании отклонена", "Сотрудник отказался стать владельцем", "company", transfer.CompanyUUID)
		return declined, nil
	}

	accepted, err := s.companyRepository.AcceptOwnershipTransfer(ctx, transfer.ID, now)
	if err != nil {
		s.log.Error(ctx, "failed to accept company ownership", zap.String("company_id", transfer.CompanyUUID.String()), zap.Error(err))
		return models.CompanyOwnershipTransfer{}, err
	}

	s.notify(ctx, transfer.FromUserUUID, models.NotificationTypeCompanyOwnerTransferDecided, "Компания передана", "Вы остаётесь в компании как заместитель", "company", transfer.CompanyUUID)
	s.log.Info(ctx, "company ownership transferred", zap.String("company_id", transfer.CompanyUUID.String()), zap.String("from_user_id", transfer.FromUserUUID.String()), zap.String("to_user_id", transfer.ToUserUUID.String()))

	return accepted, nil
}

// CancelOwnershipOffer withdraws an offer the owner changed their mind about.
func (s *Service) CancelOwnershipOffer(ctx context.Context, transferID uuid.UUID, requestUser uuid.UUID) (models.CompanyOwnershipTransfer, error) {
	if transferID == uuid.Nil || requestUser == uuid.Nil {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}

	transfer, err := s.companyRepository.GetOwnershipTransfer(ctx, transferID)
	if err != nil {
		return models.CompanyOwnershipTransfer{}, err
	}
	if transfer.FromUserUUID != requestUser {
		return models.CompanyOwnershipTransfer{}, models.ErrForbidden
	}

	return s.companyRepository.CloseOwnershipTransfer(ctx, transferID, models.CompanyOwnershipTransferCanceled, time.Now().UTC())
}

// ListIncomingOwnershipOffers shows a user the companies they are offered.
func (s *Service) ListIncomingOwnershipOffers(ctx context.Context, userID uuid.UUID) ([]models.CompanyOwnershipTransfer, error) {
	if userID == uuid.Nil {
		return nil, models.ErrInvalidCompanyInput
	}

	return s.companyRepository.ListIncomingOwnershipTransfers(ctx, userID, time.Now().UTC())
}

// ExpireOwnershipOffers is the worker entry point.
func (s *Service) ExpireOwnershipOffers(ctx context.Context) (int64, error) {
	return s.companyRepository.ExpireOwnershipTransfers(ctx, time.Now().UTC())
}

// CleanupMembershipRestrictions drops exclusion records that outlived their
// half-year window.
func (s *Service) CleanupMembershipRestrictions(ctx context.Context) (int64, error) {
	return s.companyRepository.DeleteExpiredMembershipRestrictions(ctx, time.Now().UTC())
}
