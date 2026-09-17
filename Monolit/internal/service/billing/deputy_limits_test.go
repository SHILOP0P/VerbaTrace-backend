package billing

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	billingMocks "verbatrace/monolit/internal/service/billing/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Money is the owner's. The deputy distributes what the company was given
// between departments, but neither raises the company's own cap nor stops the
// subscription. Both rules were agreed and neither was covered by a test.
func TestDeputyMayShareTheCompanyCapButNotSetOrStopIt(t *testing.T) {
	ctx := context.Background()
	companyID, deputyID, departmentID := uuid.New(), uuid.New(), uuid.New()

	repository := billingMocks.NewRepository(t)
	companies := billingMocks.NewCompanyRepository(t)
	limits := &recordingCreditLimits{}

	service := NewService(repository)
	service.SetCompanyRepository(companies)
	service.creditLimits = limits

	companies.EXPECT().
		GetCompanyMember(mock.Anything, companyID, deputyID).
		Return(models.CompanyMember{
			CompanyUUID: companyID,
			UserUUID:    deputyID,
			Role:        models.CompanyMemberRoleDeputy,
			Status:      models.MembershipStatusActive,
		}, nil)

	cap := int64(1_000)

	t.Run("the company cap belongs to the owner", func(t *testing.T) {
		err := service.SetCompanyCreditLimit(ctx, models.SetCreditLimitInput{
			CompanyUUID: companyID, UserUUID: deputyID, LimitCredits: &cap,
		})
		require.ErrorIs(t, err, models.ErrOwnerOnlyAction)
	})

	t.Run("cancelling the subscription belongs to the owner", func(t *testing.T) {
		_, err := service.CancelCompanySubscription(ctx, models.CancelCompanySubscriptionInput{
			CompanyUUID: companyID, RequestUser: deputyID,
		})
		require.ErrorIs(t, err, models.ErrOwnerOnlyAction)
	})

	t.Run("splitting it between departments is the deputy's job", func(t *testing.T) {
		err := service.SetDepartmentCreditLimit(ctx, models.SetCreditLimitInput{
			CompanyUUID:    companyID,
			DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
			UserUUID:       deputyID,
			LimitCredits:   &cap,
		})
		require.NoError(t, err)
		require.Equal(t, 1, limits.departmentCalls, "the deputy's department cap has to reach the repository")
		require.Zero(t, limits.companyCalls, "the company cap must never be written by a deputy")
	})
}

// recordingCreditLimits counts what actually reached the repository, which is
// the difference between a refusal and a silent no-op.
type recordingCreditLimits struct {
	companyCalls    int
	departmentCalls int
}

func (r *recordingCreditLimits) SetCompanyCreditLimit(context.Context, models.SetCreditLimitInput) error {
	r.companyCalls++
	return nil
}

func (r *recordingCreditLimits) SetDepartmentCreditLimit(context.Context, models.SetCreditLimitInput) error {
	r.departmentCalls++
	return nil
}

func (r *recordingCreditLimits) CompanyCreditSpending(context.Context, uuid.UUID, models.CreditPeriod, time.Time) (models.CreditSpending, error) {
	return models.CreditSpending{}, nil
}

func (r *recordingCreditLimits) DepartmentCreditSpending(context.Context, uuid.UUID, models.CreditPeriod, time.Time) ([]models.CreditSpending, error) {
	return nil, nil
}
