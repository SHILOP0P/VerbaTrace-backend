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
