package company

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	repoMocks "verbatrace/monolit/internal/repository/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// lifecycleCompanyRepository is the company repository plus the lifecycle
// methods. The service reaches those through a type assertion, so a plain mock
// would fall out before the permission check and hide what this test is for.
type lifecycleCompanyRepository struct {
	*repoMocks.CompanyRepository
}

func (lifecycleCompanyRepository) FreezeCompany(context.Context, uuid.UUID, models.CompanyFreezeReason, time.Time) error {
	return nil
}
func (lifecycleCompanyRepository) ActivateCompany(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (lifecycleCompanyRepository) GetCompanyLifecycle(context.Context, uuid.UUID) (models.CompanyLifecycle, error) {
	return models.CompanyLifecycle{}, nil
}
func (lifecycleCompanyRepository) SoftDeleteExpiredFrozenCompanies(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (lifecycleCompanyRepository) ClaimCompaniesForPurge(context.Context, time.Time, int) ([]uuid.UUID, error) {
	return nil, nil
}
func (lifecycleCompanyRepository) PurgeCompany(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (lifecycleCompanyRepository) RestoreSoftDeletedCompany(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (lifecycleCompanyRepository) CancelCompanyDeletion(context.Context, uuid.UUID, time.Time) error {
	return nil
}

// The deputy runs the company day to day and has the manager's reach almost
// everywhere. The exceptions decide whether the company keeps existing at all,
// and they were agreed but never covered by a test.
func TestDeputyCannotDecideTheCompanyLifecycle(t *testing.T) {
	ctx := context.Background()
	companyID, deputyID := uuid.New(), uuid.New()

	repository := repoMocks.NewCompanyRepository(t)
	repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, deputyID).
		Return(models.CompanyMember{
			CompanyUUID: companyID,
			UserUUID:    deputyID,
			Role:        models.CompanyMemberRoleDeputy,
			Status:      models.MembershipStatusActive,
		}, nil)

	service := NewService(lifecycleCompanyRepository{CompanyRepository: repository}, nil)

	for name, action := range map[string]func() error{
		"freeze the company":   func() error { return service.FreezeCompany(ctx, companyID, deputyID) },
		"activate the company": func() error { return service.ActivateCompany(ctx, companyID, deputyID) },
		"call off a deletion":  func() error { return service.CancelCompanyDeletion(ctx, companyID, deputyID) },
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, action(), models.ErrOwnerOnlyAction)
		})
	}
}

// The owner may do all of it.
func TestOwnerDecidesTheCompanyLifecycle(t *testing.T) {
	ctx := context.Background()
	companyID, ownerID := uuid.New(), uuid.New()

	repository := repoMocks.NewCompanyRepository(t)
	repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, ownerID).
		Return(models.CompanyMember{
			CompanyUUID: companyID,
			UserUUID:    ownerID,
			Role:        models.CompanyMemberRoleManager,
			Status:      models.MembershipStatusActive,
		}, nil)

	service := NewService(lifecycleCompanyRepository{CompanyRepository: repository}, nil)

	require.NoError(t, service.FreezeCompany(ctx, companyID, ownerID))
	require.NoError(t, service.ActivateCompany(ctx, companyID, ownerID))
	require.NoError(t, service.CancelCompanyDeletion(ctx, companyID, ownerID))
}
