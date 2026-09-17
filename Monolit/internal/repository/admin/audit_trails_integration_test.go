//go:build integration

package admin_test

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/models"
	adminRepo "verbatrace/monolit/internal/repository/admin"
	"verbatrace/monolit/internal/repository/repositorytest"

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
