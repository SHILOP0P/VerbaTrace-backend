//go:build integration

package admin_test

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/models"
	adminRepo "verbatrace/monolit/internal/repository/admin"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Every trail has to be readable. They are assembled from different tables with
// different columns, so a typo in one of them would only ever show up here.
func TestEveryAuditTrailCanBeRead(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	ctx := context.Background()

	admins := adminRepo.NewRepository(db)

	for _, trail := range models.AdminAuditTrails() {
		t.Run(string(trail), func(t *testing.T) {
			result, err := admins.ListAuditTrail(ctx, models.ListAdminAuditTrailInput{Trail: trail, Limit: 10})
			require.NoErrorf(t, err, "trail %q cannot be read", trail)
			require.Equal(t, trail, result.Trail)
			require.NotNil(t, result.Items)
		})
	}

	_, err := admins.ListAuditTrail(ctx, models.ListAdminAuditTrailInput{Trail: "made_up", Limit: 10})
	require.ErrorIs(t, err, models.ErrInvalidAdminInput)
}

// The panel has to be able to name who acted. It used to print the actor's uuid
// because that is all the trail stores, so the handle is joined in here.
func TestAuditTrailNamesTheActorRatherThanTheirUUID(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	ctx := context.Background()

	admins := adminRepo.NewRepository(db)

	actorID := repositorytest.CreateUser(t, db)
	_, err := db.ExecContext(ctx, `UPDATE users SET role='admin' WHERE user_uuid=$1`, actorID)
	require.NoError(t, err)

	var username string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT username FROM user_profiles WHERE user_uuid=$1`, actorID).Scan(&username))

	_, err = db.ExecContext(ctx, `
		INSERT INTO admin_audit_logs (audit_uuid, actor_user_uuid, actor_role, action, target_type, target_uuid, reason)
		VALUES ($1, $2, 'admin', 'company.frozen', 'company', $3, 'integration test')`,
		uuid.New(), actorID, uuid.New())
	require.NoError(t, err)

	result, err := admins.ListAuditTrail(ctx, models.ListAdminAuditTrailInput{Trail: models.AdminAuditTrailAdminActions, Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, result.Items)

	found := false
	for _, entry := range result.Items {
		if entry.ActorUserUUID.Valid && entry.ActorUserUUID.UUID == actorID {
			found = true
			require.True(t, entry.ActorUsername.Valid, "the actor's handle has to come back with the row")
			require.Equal(t, username, entry.ActorUsername.String)
		}
	}
	require.True(t, found, "the row just written is not in the trail")
}

// Alerts are the one trail with a state. Closing one has to work, and closing
// the same one twice has to say so rather than pretend it did something.
func TestBillingAlertsCanBeResolvedOnce(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	ctx := context.Background()

	admins := adminRepo.NewRepository(db)

	alertID := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO billing_alerts(billing_alert_uuid, alert_type, severity, deduplication_key, details)
		VALUES ($1, 'provider_outcome_unknown', 'warning', $2, '{}'::jsonb)`, alertID, "test:"+alertID.String())
	require.NoError(t, err)

	require.NoError(t, admins.ResolveBillingAlert(ctx, alertID))

	var status string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM billing_alerts WHERE billing_alert_uuid=$1`, alertID).Scan(&status))
	require.Equal(t, "resolved", status)

	require.ErrorIs(t, admins.ResolveBillingAlert(ctx, alertID), models.ErrAdminRecordNotFound)
	require.ErrorIs(t, admins.ResolveBillingAlert(ctx, uuid.New()), models.ErrAdminRecordNotFound)
}
