package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) ResetAdminUsage(ctx context.Context, input models.ResetAdminUsageInput) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset usage: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, input.ActorUserUUID)
	if err != nil {
		return err
	}
	if actor.Role != models.UserRoleAdmin && actor.Role != models.UserRoleSuperAdmin {
		return models.ErrForbidden
	}
	ownerColumn, owner, subjectType := "user_uuid", input.UserUUID, "user"
	if input.CompanyUUID != uuid.Nil {
		ownerColumn, owner, subjectType = "company_uuid", input.CompanyUUID, "company"
	}
	var subscriptionID uuid.UUID
	if err = tx.QueryRowContext(ctx, fmt.Sprintf("SELECT subscription_uuid FROM subscriptions WHERE %s=$1 AND status='active' ORDER BY starts_at DESC LIMIT 1", ownerColumn), owner).Scan(&subscriptionID); err == sql.ErrNoRows {
		return models.ErrSubscriptionNotFound
	} else if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM usage_counters WHERE subscription_uuid=$1", subscriptionID); err != nil {
		return fmt.Errorf("reset monthly usage: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM deep_analysis_usage_counters WHERE subject_type=$1 AND subject_uuid=$2", subjectType, owner); err != nil {
		return fmt.Errorf("reset deep usage: %w", err)
	}
	after, _ := json.Marshal(map[string]string{"scope": subjectType, "owner_uuid": owner.String(), "limits": "monthly_minutes,deep_analysis"})
	if err = insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: "usage.reset", TargetType: subjectType, TargetUUID: uuid.NullUUID{UUID: owner, Valid: true}, AfterData: after, Reason: &input.Metadata.Reason, RequestID: input.Metadata.RequestID, IPAddress: input.Metadata.IPAddress, UserAgent: input.Metadata.UserAgent, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return tx.Commit()
}
