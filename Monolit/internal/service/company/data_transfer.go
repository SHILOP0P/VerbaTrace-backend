package company

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// dataTransferRepository is the part of the company repository this file needs.
type dataTransferRepository interface {
	TransferCompanyData(ctx context.Context, input models.TransferCompanyDataInput) (models.TransferCompanyDataResult, error)
	ListCompanyDataTransfers(ctx context.Context, ownerID uuid.UUID, limit int) ([]models.TransferCompanyDataResult, error)
}

// TransferCompanyData moves data between two of the owner's companies.
//
// Only the owner may do it, and only between companies they own: the operation
// changes who can reach a recording, which is the one thing a deputy is not
// trusted with either.
func (s *Service) TransferCompanyData(ctx context.Context, input models.TransferCompanyDataInput) (models.TransferCompanyDataResult, error) {
	repository, ok := s.companyRepository.(dataTransferRepository)
	if !ok {
		return models.TransferCompanyDataResult{}, models.ErrInvalidCompanyInput
	}
	if input.OwnerUserUUID == uuid.Nil {
		return models.TransferCompanyDataResult{}, models.ErrInvalidCompanyInput
	}

	// The source may be frozen — that is the usual reason for moving out of it —
	// so only the owner's title to it is checked here. The repository refuses a
	// frozen destination.
	if err := s.requireCompanyOwner(ctx, input.SourceCompanyUUID, input.OwnerUserUUID); err != nil {
		return models.TransferCompanyDataResult{}, err
	}
	if err := s.requireCompanyOwner(ctx, input.TargetCompanyUUID, input.OwnerUserUUID); err != nil {
		return models.TransferCompanyDataResult{}, err
	}

	input.Reason = strings.TrimSpace(input.Reason)

	result, err := repository.TransferCompanyData(ctx, input)
	if err != nil {
		s.log.Warn(ctx, "failed to transfer company data",
			zap.String("source_company_id", input.SourceCompanyUUID.String()),
			zap.String("target_company_id", input.TargetCompanyUUID.String()),
			zap.Error(err))
		return models.TransferCompanyDataResult{}, err
	}

	s.log.Info(ctx, "company data transferred",
		zap.String("source_company_id", input.SourceCompanyUUID.String()),
		zap.String("target_company_id", input.TargetCompanyUUID.String()),
		zap.Int64("calls", result.Calls),
		zap.Int64("folders", result.Folders))

	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}

	return result, nil
}

// ListCompanyDataTransfers shows the owner what they have already moved.
func (s *Service) ListCompanyDataTransfers(ctx context.Context, ownerID uuid.UUID, limit int) ([]models.TransferCompanyDataResult, error) {
	repository, ok := s.companyRepository.(dataTransferRepository)
	if !ok || ownerID == uuid.Nil {
		return nil, models.ErrInvalidCompanyInput
	}

	return repository.ListCompanyDataTransfers(ctx, ownerID, limit)
}
