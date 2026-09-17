//go:build integration

package notification_test

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestEveryNotificationTypeIsAccepted compares the constants the code writes
// with the CHECK constraint the table enforces. The project has already shipped
// a type that was missing from the constraint twice: the insert fails at
// runtime, and because most call sites ignore that error nobody ever hears
// about it. Reading both lists by eye is what let it through, so a test reads
// them instead.
func TestEveryNotificationTypeIsAccepted(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()

	userID := repositorytest.CreateUser(t, db)

	for _, notificationType := range models.NotificationTypes() {
		t.Run(string(notificationType), func(t *testing.T) {
			_, err := db.ExecContext(ctx,
				`INSERT INTO notifications(notification_uuid,user_uuid,type,title,body) VALUES($1,$2,$3,'title','body')`,
				uuid.New(), userID, notificationType)
			require.NoErrorf(t, err, "notification type %q is written by the code but rejected by notifications_type_check", notificationType)
		})
	}
}
