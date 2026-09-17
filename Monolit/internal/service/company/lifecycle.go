package company

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// lifecycleRepository is implemented by the company repository.
type lifecycleRepository interface {
	FreezeCompany(context.Context, uuid.UUID, models.CompanyFreezeReason, time.Time) error
	ActivateCompany(context.Context, uuid.UUID, time.Time) error
	GetCompanyLifecycle(context.Context, uuid.UUID) (models.CompanyLifecycle, error)
	SoftDeleteExpiredFrozenCompanies(context.Context, time.Time) (int64, error)
	ClaimCompaniesForPurge(context.Context, time.Time, int) ([]uuid.UUID, error)
	PurgeCompany(context.Context, uuid.UUID, time.Time) error
	RestoreSoftDeletedCompany(context.Context, uuid.UUID, time.Time) error
	CancelCompanyDeletion(context.Context, uuid.UUID, time.Time) error
}

// FreezeCompany stops a company on the owner's own decision — usually because
// the plan no longer covers all of them.
func (s *Service) FreezeCompany(ctx context.Context, companyID uuid.UUID, requestUser uuid.UUID) error {
	lifecycle, ok := s.companyRepository.(lifecycleRepository)
	if !ok || companyID == uuid.Nil || requestUser == uuid.Nil {
		return models.ErrInvalidCompanyInput
	}
	if err := s.requireCompanyOwner(ctx, companyID, requestUser); err != nil {
		return err
	}

	return lifecycle.FreezeCompany(ctx, companyID, models.CompanyFreezeReasonDowngrade, time.Now().UTC())
}

// ActivateCompany is the other half of the choice: which companies keep working
// when the plan covers fewer than the owner has.
func (s *Service) ActivateCompany(ctx context.Context, companyID uuid.UUID, requestUser uuid.UUID) error {
	lifecycle, ok := s.companyRepository.(lifecycleRepository)
	if !ok || companyID == uuid.Nil || requestUser == uuid.Nil {
		return models.ErrInvalidCompanyInput
	}
	if err := s.requireCompanyOwner(ctx, companyID, requestUser); err != nil {
		return err
	}

	return lifecycle.ActivateCompany(ctx, companyID, time.Now().UTC())
}

// CancelCompanyDeletion calls off a deletion the owner has started. The company
// stays frozen afterwards: switching it back on is a separate step, and it only
// works while the plan still covers one more company.
func (s *Service) CancelCompanyDeletion(ctx context.Context, companyID uuid.UUID, requestUser uuid.UUID) error {
	lifecycle, ok := s.companyRepository.(lifecycleRepository)
	if !ok || companyID == uuid.Nil || requestUser == uuid.Nil {
		return models.ErrInvalidCompanyInput
	}
	if err := s.requireCompanyOwner(ctx, companyID, requestUser); err != nil {
		return err
	}

	return lifecycle.CancelCompanyDeletion(ctx, companyID, time.Now().UTC())
}

// GetCompanyLifecycle tells the interface what state a company is in and how
// long it has left.
func (s *Service) GetCompanyLifecycle(ctx context.Context, companyID uuid.UUID, requestUser uuid.UUID) (models.CompanyLifecycle, error) {
	lifecycle, ok := s.companyRepository.(lifecycleRepository)
	if !ok || companyID == uuid.Nil || requestUser == uuid.Nil {
		return models.CompanyLifecycle{}, models.ErrInvalidCompanyInput
	}
	if _, err := s.companyRepository.GetCompanyByUUID(ctx, companyID, requestUser); err != nil {
		return models.CompanyLifecycle{}, err
	}

	return lifecycle.GetCompanyLifecycle(ctx, companyID)
}

// RunLifecycleMaintenance moves companies along their lifecycle: a freeze that
// ran out becomes a soft deletion, and a soft deletion that ran out is purged.
func (s *Service) RunLifecycleMaintenance(ctx context.Context, limit int) (int64, int, error) {
	lifecycle, ok := s.companyRepository.(lifecycleRepository)
	if !ok {
		return 0, 0, nil
	}

	now := time.Now().UTC()
	softDeleted, err := lifecycle.SoftDeleteExpiredFrozenCompanies(ctx, now)
	if err != nil {
		return 0, 0, err
	}

	companies, err := lifecycle.ClaimCompaniesForPurge(ctx, now, limit)
	if err != nil {
		return softDeleted, 0, err
	}

	purged := 0
	for _, companyID := range companies {
		err := lifecycle.PurgeCompany(ctx, companyID, now)
		switch {
		case err == nil:
			purged++
		case errors.Is(err, models.ErrCompanyPurgePending):
			// The retention worker is still removing this company's calls and
			// their files. Expected on the first passes, so it is not an error.
			s.log.Info(ctx, "company purge waiting for its calls", zap.String("company_id", companyID.String()))
		default:
			s.log.Error(ctx, "failed to purge company", zap.String("company_id", companyID.String()), zap.Error(err))
		}
	}

	return softDeleted, purged, nil
}

// RestoreSoftDeletedCompany is the superadmin's one-time rescue: the company
// returns to a freeze, without anybody getting access to its content.
func (s *Service) RestoreSoftDeletedCompany(ctx context.Context, companyID uuid.UUID) error {
	lifecycle, ok := s.companyRepository.(lifecycleRepository)
	if !ok || companyID == uuid.Nil {
		return models.ErrInvalidCompanyInput
	}

	return lifecycle.RestoreSoftDeletedCompany(ctx, companyID, time.Now().UTC())
}
