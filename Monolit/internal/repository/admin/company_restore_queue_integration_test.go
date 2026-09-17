//go:build integration

package admin_test

import (
	"context"
	"testing"
	"time"

	adminRepo "verbatrace/monolit/internal/repository/admin"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The one-time rescue of a deleted company was unreachable: every company query
// filters deleted rows, so the card carrying the restore control could not be
// opened for the only companies that need it. The queue is that missing list.
func TestRestorableCompaniesAreTheOnesStillWorthRestoring(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	ctx := context.Background()
	admins := adminRepo.NewRepository(db)

	ownerID := repositorytest.CreateUser(t, db)
	deleted := createCompanyFor(t, db, ownerID, "deleted")
	rescuedBefore := createCompanyFor(t, db, ownerID, "rescued")
	frozen := createCompanyFor(t, db, ownerID, "frozen")
	alive := createCompanyFor(t, db, ownerID, "alive")

	softDeletedAt := time.Now().UTC().Add(-24 * time.Hour)
	purgeAfter := softDeletedAt.Add(30 * 24 * time.Hour)
	for _, id := range []uuid.UUID{deleted, rescuedBefore} {
		_, err := db.ExecContext(ctx, `
			UPDATE companies
			SET lifecycle_state='soft_deleted', soft_deleted_at=$2, purge_after=$3, deleted_at=$2
			WHERE company_uuid=$1
		`, id, softDeletedAt, purgeAfter)
		require.NoError(t, err)
	}

	// A company that has already used its one restore is not offered again.
	_, err := db.ExecContext(ctx, `UPDATE companies SET restore_used=true WHERE company_uuid=$1`, rescuedBefore)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		UPDATE companies SET lifecycle_state='frozen', frozen_at=now(), freeze_reason='downgrade' WHERE company_uuid=$1
	`, frozen)
	require.NoError(t, err)

	queue, err := admins.ListRestorableCompanies(ctx)
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, company := range queue {
		ids[company.ID.String()] = true
		if company.ID == deleted {
			require.Equal(t, "deleted", company.Name)
			require.WithinDuration(t, softDeletedAt, company.SoftDeletedAt, time.Second)
			require.NotNil(t, company.PurgeAfter, "the deadline is what the decision is made by")
			require.WithinDuration(t, purgeAfter, *company.PurgeAfter, time.Second)
		}
	}

	require.True(t, ids[deleted.String()], "a company inside its purge window has to be offered")
	require.False(t, ids[rescuedBefore.String()], "the restore is one-time")
	require.False(t, ids[frozen.String()], "a frozen company is not deleted; its owner switches it on")
	require.False(t, ids[alive.String()], "an active company has nothing to restore")
}
