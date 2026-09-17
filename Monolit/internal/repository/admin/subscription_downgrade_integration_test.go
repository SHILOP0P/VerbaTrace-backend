//go:build integration

package admin_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	adminRepo "verbatrace/monolit/internal/repository/admin"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func createCompanyFor(t *testing.T, db *sql.DB, ownerID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	companyID := uuid.New()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,$2,$3,$4,now())`,
		companyID, name, name+companyID.String()[:8], ownerID)
	require.NoError(t, err)
	repositorytest.InsertCompanyMember(t, db, companyID, ownerID, "company_manager", "active")

	return companyID
}

func lifecycleState(t *testing.T, db *sql.DB, companyID uuid.UUID) string {
	t.Helper()
	var state string
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT lifecycle_state FROM companies WHERE company_uuid=$1`, companyID).Scan(&state))

	return state
}

// Lowering a plan below the number of companies an owner runs used to do nothing
// at all: every company stayed active and kept working on a plan that no longer
// covered it. Now the grant refuses until somebody says which ones keep working.
func TestDowngradeAsksWhichCompaniesStayActive(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	ctx := context.Background()
	admins := adminRepo.NewRepository(db)

	adminID := repositorytest.CreateUser(t, db)
	_, err := db.ExecContext(ctx, `UPDATE users SET role='admin' WHERE user_uuid=$1`, adminID)
	require.NoError(t, err)

	ownerID := repositorytest.CreateUser(t, db)
	first := createCompanyFor(t, db, ownerID, "first")
	second := createCompanyFor(t, db, ownerID, "second")
	third := createCompanyFor(t, db, ownerID, "third")

	grant := func(plan models.PlanCode, active []uuid.UUID) (models.AdminSubscription, error) {
		return admins.GrantAdminSubscription(ctx, models.GrantAdminSubscriptionInput{
			ActorUserUUID:      adminID,
			CompanyUUID:        first,
			PlanCode:           plan,
			StartsAt:           time.Now().UTC().Add(-time.Hour),
			EndsAt:             time.Now().UTC().Add(30 * 24 * time.Hour),
			ActiveCompanyUUIDs: active,
			Metadata:           models.AdminMutationMetadata{Reason: "integration test"},
		})
	}

	// business_pro covers three companies, so nothing has to be chosen.
	_, err = grant(models.PlanCodeBusinessPro, nil)
	require.NoError(t, err)
	require.Equal(t, "active", lifecycleState(t, db, third))

	// business_start covers one. Without a choice the grant is refused, and the
	// answer carries what the interface needs to ask.
	_, err = grant(models.PlanCodeBusinessStart, nil)
	var selection *models.CompanySelectionRequired
	require.True(t, errors.As(err, &selection), "expected a company selection request, got %v", err)
	require.Equal(t, ownerID, selection.OwnerUserUUID)
	require.Equal(t, 1, selection.CompanyLimit)
	require.ElementsMatch(t, []uuid.UUID{first, second, third}, selection.CompanyUUIDs)

	// The plan is still the one that was there: a refused grant changes nothing.
	require.Equal(t, "active", lifecycleState(t, db, third))

	// Choosing more than the plan covers is refused too.
	_, err = grant(models.PlanCodeBusinessStart, []uuid.UUID{first, second})
	require.ErrorIs(t, err, models.ErrCompanyLimitExceeded)

	// With a valid choice the rest freeze in the same transaction.
	_, err = grant(models.PlanCodeBusinessStart, []uuid.UUID{second})
	require.NoError(t, err)
	require.Equal(t, "active", lifecycleState(t, db, second))
	require.Equal(t, "frozen", lifecycleState(t, db, first))
	require.Equal(t, "frozen", lifecycleState(t, db, third))

	var reason string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT freeze_reason FROM companies WHERE company_uuid=$1`, third).Scan(&reason))
	require.Equal(t, string(models.CompanyFreezeReasonDowngrade), reason)
}

// A business plan is sold together with a personal one, and granting half a
// package leaves the owner unable to use the product outside their companies.
func TestGrantingABusinessPlanGrantsTheBundledPersonalPlan(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	ctx := context.Background()
	admins := adminRepo.NewRepository(db)

	adminID := repositorytest.CreateUser(t, db)
	_, err := db.ExecContext(ctx, `UPDATE users SET role='admin' WHERE user_uuid=$1`, adminID)
	require.NoError(t, err)

	ownerID := repositorytest.CreateUser(t, db)
	companyID := createCompanyFor(t, db, ownerID, "bundle")

	_, err = admins.GrantAdminSubscription(ctx, models.GrantAdminSubscriptionInput{
		ActorUserUUID: adminID,
		CompanyUUID:   companyID,
		PlanCode:      models.PlanCodeBusinessPro,
		StartsAt:      time.Now().UTC().Add(-time.Hour),
		EndsAt:        time.Now().UTC().Add(30 * 24 * time.Hour),
		Metadata:      models.AdminMutationMetadata{Reason: "integration test"},
	})
	require.NoError(t, err)

	var code string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT p.code FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
		WHERE s.user_uuid=$1 AND s.type='personal' AND s.status='active'
	`, ownerID).Scan(&code))
	require.Equal(t, string(models.PlanCodePersonalPro), code)
}
