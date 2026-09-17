//go:build integration

package admin_test

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	adminRepo "verbatrace/monolit/internal/repository/admin"
	billingRepo "verbatrace/monolit/internal/repository/billing"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestGrantedBusinessSubscriptionIsVisibleToTheProduct is the test whose absence
// let the two halves of billing drift apart: the administrator's grant wrote the
// plan against the company while everything else looked it up through the owner,
// so granting appeared to succeed and the company stayed without a subscription.
func TestGrantedBusinessSubscriptionIsVisibleToTheProduct(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	ctx := context.Background()

	admins := adminRepo.NewRepository(db)
	billing := billingRepo.NewRepository(db)

	superAdminID := repositorytest.CreateUser(t, db)
	_, err := db.ExecContext(ctx, `UPDATE users SET role='superadmin' WHERE user_uuid=$1`, superAdminID)
	require.NoError(t, err)

	ownerID := repositorytest.CreateUser(t, db)
	companyID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Acme',$2,$3,now())`,
		companyID, "acme"+companyID.String()[:8], ownerID)
	require.NoError(t, err)
	repositorytest.InsertCompanyMember(t, db, companyID, ownerID, "company_manager", "active")

	granted, err := admins.GrantAdminSubscription(ctx, models.GrantAdminSubscriptionInput{
		ActorUserUUID: superAdminID,
		CompanyUUID:   companyID,
		PlanCode:      models.PlanCodeBusinessPlus,
		StartsAt:      time.Now().UTC().Add(-time.Hour),
		EndsAt:        time.Now().UTC().Add(30 * 24 * time.Hour),
		Metadata:      models.AdminMutationMetadata{Reason: "integration test"},
	})
	require.NoError(t, err)
	require.True(t, granted.UserUUID.Valid, "a business plan is stored against the owner")
	require.Equal(t, ownerID, granted.UserUUID.UUID)

	covering, err := billing.GetActiveBusinessSubscription(ctx, companyID)
	require.NoError(t, err, "the company must be covered by the plan the administrator just granted")
	require.Equal(t, granted.ID, covering.ID)

	// The administrator's own screen has to find it back by the company too.
	shown, err := admins.GetAdminCompanySubscription(ctx, companyID)
	require.NoError(t, err)
	require.Equal(t, granted.ID, shown.ID)

	canceled, err := admins.CancelAdminSubscription(ctx, models.CancelAdminSubscriptionInput{
		ActorUserUUID: superAdminID,
		CompanyUUID:   companyID,
		Metadata:      models.AdminMutationMetadata{Reason: "integration test"},
	})
	require.NoError(t, err)
	require.Equal(t, granted.ID, canceled.ID)

	_, err = billing.GetActiveBusinessSubscription(ctx, companyID)
	require.ErrorIs(t, err, models.ErrSubscriptionNotFound)
}
