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

// ownershipRepository is the part of the company repository this file needs on
// top of the shared interface.
type ownershipRepository interface {
	CountOwnedCompanies(ctx context.Context, ownerID uuid.UUID) (int, error)
	ListCompaniesByManager(ctx context.Context, ownerID uuid.UUID) ([]models.Company, error)
}

// OfferOwnership proposes the company, or every company under the owner's plan,
// to another person. Ownership moves only when that person accepts.
//
// The scope is not a preference: a business plan belongs to the owner and covers
// several companies, so one of several cannot be cut out of it. A single company
// may be handed over on its own only when it is the only one the plan covers.
func (s *Service) OfferOwnership(ctx context.Context, input models.CreateCompanyOwnershipTransferInput) (models.CompanyOwnershipTransfer, error) {
	if input.RequestUser == uuid.Nil || input.ToUserUUID == uuid.Nil || input.RequestUser == input.ToUserUUID {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}

	scope := input.Scope
	if scope == "" {
		scope = models.CompanyOwnershipTransferScopeCompany
	}
	if scope != models.CompanyOwnershipTransferScopeCompany && scope != models.CompanyOwnershipTransferScopeAll {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}

	counter, ok := s.companyRepository.(ownershipRepository)
	if !ok {
		return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
	}
	owned, err := counter.CountOwnedCompanies(ctx, input.RequestUser)
	if err != nil {
		return models.CompanyOwnershipTransfer{}, err
	}
	if owned == 0 {
		return models.CompanyOwnershipTransfer{}, models.ErrCompanyNotFound
	}

	transfer := models.CompanyOwnershipTransfer{
		ID:           uuid.New(),
		Scope:        scope,
		FromUserUUID: input.RequestUser,
		ToUserUUID:   input.ToUserUUID,
	}

	switch scope {
	case models.CompanyOwnershipTransferScopeCompany:
		if input.CompanyUUID == uuid.Nil {
			return models.CompanyOwnershipTransfer{}, models.ErrInvalidCompanyInput
		}
		if owned > 1 {
			return models.CompanyOwnershipTransfer{}, models.ErrOwnershipScopeMismatch
		}
		if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
			return models.CompanyOwnershipTransfer{}, err
		}
		transfer.CompanyUUID = uuid.NullUUID{UUID: input.CompanyUUID, Valid: true}
	default:
		if owned == 1 {
			return models.CompanyOwnershipTransfer{}, models.ErrOwnershipScopeMismatch
		}
	}

	stay, err := s.validateStayCompanies(ctx, counter, input, scope, transfer.CompanyUUID)
	if err != nil {
		return models.CompanyOwnershipTransfer{}, err
	}
	transfer.StayCompanyUUIDs = stay

	now := time.Now().UTC()
	transfer.CreatedAt = now
	transfer.ExpiresAt = now.Add(ownershipTransferTTL)
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		transfer.Reason = &reason
	}

	created, err := s.companyRepository.CreateOwnershipTransfer(ctx, transfer)
	if err != nil {
		s.log.Error(ctx, "failed to offer company ownership", zap.String("to_user_id", input.ToUserUUID.String()), zap.Error(err))
		return models.CompanyOwnershipTransfer{}, err
	}

	s.notify(ctx, input.ToUserUUID, models.NotificationTypeCompanyOwnerTransferAsked, "Вам предлагают стать владельцем компании", "Подтвердите или отклоните передачу", "company", transfer.CompanyUUID.UUID)

	return created, nil
}

// validateStayCompanies keeps the previous owner's choice honest: they may only
// ask to stay in companies they are actually handing over.
func (s *Service) validateStayCompanies(ctx context.Context, repository ownershipRepository, input models.CreateCompanyOwnershipTransferInput, scope models.CompanyOwnershipTransferScope, single uuid.NullUUID) ([]uuid.UUID, error) {
	if len(input.StayCompanyUUIDs) == 0 {
		return []uuid.UUID{}, nil
	}

	allowed := map[uuid.UUID]bool{}
	if scope == models.CompanyOwnershipTransferScopeCompany {
		allowed[single.UUID] = true
	} else {
		companies, err := repository.ListCompaniesByManager(ctx, input.RequestUser)
		if err != nil {
			return nil, err
		}
		for _, company := range companies {
			allowed[company.ID] = true
		}
	}

	seen := map[uuid.UUID]bool{}
	stay := make([]uuid.UUID, 0, len(input.StayCompanyUUIDs))
	for _, companyID := range input.StayCompanyUUIDs {
		if !allowed[companyID] || seen[companyID] {
			return nil, models.ErrInvalidCompanyInput
		}
		seen[companyID] = true
		stay = append(stay, companyID)
	}

	return stay, nil
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
		s.notify(ctx, transfer.FromUserUUID, models.NotificationTypeCompanyOwnerTransferDecided, "Передача компании отклонена", "Сотрудник отказался стать владельцем", "company", transfer.CompanyUUID.UUID)
		return declined, nil
	}

	accepted, err := s.companyRepository.AcceptOwnershipTransfer(ctx, transfer.ID, now)
	if err != nil {
		s.log.Error(ctx, "failed to accept company ownership", zap.String("transfer_id", transfer.ID.String()), zap.Error(err))
		return models.CompanyOwnershipTransfer{}, err
	}

	s.notify(ctx, transfer.FromUserUUID, models.NotificationTypeCompanyOwnerTransferDecided, "Компания передана", ownershipHandoverBody(accepted), "company", transfer.CompanyUUID.UUID)
	s.log.Info(ctx, "company ownership transferred",
		zap.String("transfer_id", transfer.ID.String()),
		zap.Int("companies", len(accepted.CompanyUUIDs)),
		zap.String("from_user_id", transfer.FromUserUUID.String()),
		zap.String("to_user_id", transfer.ToUserUUID.String()))

	return accepted, nil
}

func ownershipHandoverBody(transfer models.CompanyOwnershipTransfer) string {
	if len(transfer.StayCompanyUUIDs) == 0 {
		return "Подписка и компании перешли новому владельцу, вы вышли из них"
	}

	return "Подписка и компании перешли новому владельцу, вы остались участником"
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
