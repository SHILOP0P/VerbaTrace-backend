package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const resetSecondApprovalThreshold = 100

func (r *Repository) PreviewAllowanceResetBatch(ctx context.Context, actor uuid.UUID, ownerType string, owners []uuid.UUID) (int, error) {
	if actor == uuid.Nil || len(owners) == 0 || len(owners) > 5000 || (ownerType != "user" && ownerType != "company") {
		return 0, models.ErrInvalidAdminInput
	}
	var role string
	if err := r.db.QueryRowContext(ctx, `SELECT role FROM users WHERE user_uuid=$1`, actor).Scan(&role); err != nil || role != string(models.UserRoleSuperAdmin) {
		return 0, models.ErrForbidden
	}
	column := "user_uuid"
	if ownerType == "company" {
		column = "company_uuid"
	}
	var count int
	err := r.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(DISTINCT %s) FROM subscriptions WHERE %s=ANY($1) AND status='active'`, column, column), owners).Scan(&count)
	return count, err
}

func (r *Repository) CreateAllowanceResetBatch(ctx context.Context, actor uuid.UUID, ownerType string, owners []uuid.UUID, reason, idempotency string) (models.AllowanceResetBatch, error) {
	if actor == uuid.Nil || len(owners) == 0 || len(owners) > 5000 || len(reason) < 3 || idempotency == "" || (ownerType != "user" && ownerType != "company") {
		return models.AllowanceResetBatch{}, models.ErrInvalidAdminInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	admin, err := getAdminUserForUpdate(ctx, tx, actor)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	if admin.Role != models.UserRoleSuperAdmin {
		return models.AllowanceResetBatch{}, models.ErrForbidden
	}
	var existing uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT allowance_reset_batch_uuid FROM allowance_reset_batches WHERE idempotency_key=$1`, idempotency).Scan(&existing)
	if err == nil {
		return getResetBatchTx(ctx, tx, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.AllowanceResetBatch{}, err
	}
	snapshot, _ := json.Marshal(map[string]any{"owner_type": ownerType, "owner_uuids": owners})
	id, _ := uuid.NewV7()
	status := "approved"
	requires := len(owners) > resetSecondApprovalThreshold
	if requires {
		status = "draft"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO allowance_reset_batches(allowance_reset_batch_uuid,filter_snapshot,status,reason,idempotency_key,requested_by_user_uuid,total_items) VALUES($1,$2,$3,$4,$5,$6,0)`, id, snapshot, status, reason, idempotency, actor)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	column := "user_uuid"
	if ownerType == "company" {
		column = "company_uuid"
	}
	for _, owner := range owners {
		var subscription uuid.UUID
		err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT subscription_uuid FROM subscriptions WHERE %s=$1 AND status='active' ORDER BY starts_at DESC LIMIT 1`, column), owner).Scan(&subscription)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return models.AllowanceResetBatch{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO allowance_reset_items(allowance_reset_item_uuid,allowance_reset_batch_uuid,subscription_uuid,status) VALUES(gen_random_uuid(),$1,$2,'pending') ON CONFLICT DO NOTHING`, id, subscription)
		if err != nil {
			return models.AllowanceResetBatch{}, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE allowance_reset_batches SET total_items=(SELECT count(*) FROM allowance_reset_items WHERE allowance_reset_batch_uuid=$1) WHERE allowance_reset_batch_uuid=$1`, id)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	if err = insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor, ActorRole: admin.Role, Action: "credit_allowance.reset_batch_created", TargetType: "allowance_reset_batch", TargetUUID: uuid.NullUUID{UUID: id, Valid: true}, AfterData: snapshot, Reason: &reason, CreatedAt: time.Now().UTC()}); err != nil {
		return models.AllowanceResetBatch{}, err
	}
	batch, err := getResetBatchTx(ctx, tx, id)
	if err != nil {
		return batch, err
	}
	batch.RequiresSecondApproval = requires
	if err = tx.Commit(); err != nil {
		return batch, err
	}
	return batch, nil
}

func (r *Repository) ApproveAllowanceResetBatch(ctx context.Context, id, actor uuid.UUID) (models.AllowanceResetBatch, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	admin, err := getAdminUserForUpdate(ctx, tx, actor)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	if admin.Role != models.UserRoleSuperAdmin && admin.Role != models.UserRoleAdmin {
		return models.AllowanceResetBatch{}, models.ErrForbidden
	}
	var requester uuid.UUID
	var status string
	err = tx.QueryRowContext(ctx, `SELECT requested_by_user_uuid,status FROM allowance_reset_batches WHERE allowance_reset_batch_uuid=$1 FOR UPDATE`, id).Scan(&requester, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AllowanceResetBatch{}, models.ErrIntegrationNotFound
	}
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	if status != "draft" || requester == actor {
		return models.AllowanceResetBatch{}, models.ErrForbidden
	}
	_, err = tx.ExecContext(ctx, `UPDATE allowance_reset_batches SET status='approved',approved_by_user_uuid=$2 WHERE allowance_reset_batch_uuid=$1`, id, actor)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	batch, err := getResetBatchTx(ctx, tx, id)
	if err != nil {
		return batch, err
	}
	if err = tx.Commit(); err != nil {
		return batch, err
	}
	return batch, nil
}

func (r *Repository) ExecuteAllowanceResetBatch(ctx context.Context, id, actor uuid.UUID) (models.AllowanceResetBatch, error) {
	var ownerType, reason string
	var requester uuid.UUID
	var status string
	var snapshot []byte
	err := r.db.QueryRowContext(ctx, `SELECT filter_snapshot,status,reason,requested_by_user_uuid FROM allowance_reset_batches WHERE allowance_reset_batch_uuid=$1`, id).Scan(&snapshot, &status, &reason, &requester)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AllowanceResetBatch{}, models.ErrIntegrationNotFound
	}
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	if status != "approved" && status != "running" && status != "paused" {
		return models.AllowanceResetBatch{}, models.ErrIntegrationConflict
	}
	var parsed struct {
		OwnerType string `json:"owner_type"`
	}
	if err = json.Unmarshal(snapshot, &parsed); err != nil {
		return models.AllowanceResetBatch{}, err
	}
	ownerType = parsed.OwnerType
	var role string
	if err = r.db.QueryRowContext(ctx, `SELECT role FROM users WHERE user_uuid=$1`, actor).Scan(&role); err != nil || role != string(models.UserRoleSuperAdmin) {
		return models.AllowanceResetBatch{}, models.ErrForbidden
	}
	_, err = r.db.ExecContext(ctx, `UPDATE allowance_reset_batches SET status='running' WHERE allowance_reset_batch_uuid=$1 AND status IN ('approved','paused','running')`, id)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT i.allowance_reset_item_uuid,s.user_uuid,s.company_uuid FROM allowance_reset_items i JOIN subscriptions s USING(subscription_uuid) WHERE i.allowance_reset_batch_uuid=$1 AND i.status IN ('pending','failed') ORDER BY i.created_at LIMIT 500`, id)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	type target struct {
		id            uuid.UUID
		user, company uuid.NullUUID
	}
	var targets []target
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.id, &t.user, &t.company); err != nil {
			_ = rows.Close()
			return models.AllowanceResetBatch{}, err
		}
		targets = append(targets, t)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return models.AllowanceResetBatch{}, err
	}
	if err = rows.Close(); err != nil {
		return models.AllowanceResetBatch{}, err
	}
	for _, t := range targets {
		_, _ = r.db.ExecContext(ctx, `UPDATE allowance_reset_items SET status='running',attempts=attempts+1,updated_at=now() WHERE allowance_reset_item_uuid=$1`, t.id)
		input := models.ResetAdminUsageInput{ActorUserUUID: actor, Metadata: models.AdminMutationMetadata{Reason: reason}}
		if ownerType == "user" && t.user.Valid {
			input.UserUUID = t.user.UUID
		} else if ownerType == "company" && t.company.Valid {
			input.CompanyUUID = t.company.UUID
		}
		resetErr := r.ResetAdminUsage(ctx, input)
		if resetErr != nil {
			_, _ = r.db.ExecContext(ctx, `UPDATE allowance_reset_items SET status='failed',error_code='reset_failed',updated_at=now() WHERE allowance_reset_item_uuid=$1`, t.id)
		} else {
			_, _ = r.db.ExecContext(ctx, `UPDATE allowance_reset_items SET status='succeeded',error_code=NULL,updated_at=now() WHERE allowance_reset_item_uuid=$1`, t.id)
		}
	}
	_, err = r.db.ExecContext(ctx, `UPDATE allowance_reset_batches b SET succeeded_items=x.succeeded,failed_items=x.failed,status=CASE WHEN x.pending=0 THEN 'completed' ELSE 'paused' END,completed_at=CASE WHEN x.pending=0 THEN now() ELSE NULL END FROM(SELECT count(*)FILTER(WHERE status='succeeded') succeeded,count(*)FILTER(WHERE status='failed') failed,count(*)FILTER(WHERE status IN('pending','running','failed')) pending FROM allowance_reset_items WHERE allowance_reset_batch_uuid=$1)x WHERE b.allowance_reset_batch_uuid=$1`, id)
	if err != nil {
		return models.AllowanceResetBatch{}, err
	}
	return r.GetAllowanceResetBatch(ctx, id, actor)
}

func (r *Repository) GetAllowanceResetBatch(ctx context.Context, id, actor uuid.UUID) (models.AllowanceResetBatch, error) {
	var role string
	if err := r.db.QueryRowContext(ctx, `SELECT role FROM users WHERE user_uuid=$1`, actor).Scan(&role); err != nil || role != string(models.UserRoleSuperAdmin) {
		return models.AllowanceResetBatch{}, models.ErrForbidden
	}
	return getResetBatchRow(r.db.QueryRowContext(ctx, `SELECT allowance_reset_batch_uuid,filter_snapshot,status,reason,total_items,succeeded_items,failed_items,requested_by_user_uuid,approved_by_user_uuid,created_at,completed_at FROM allowance_reset_batches WHERE allowance_reset_batch_uuid=$1`, id))
}
func getResetBatchTx(ctx context.Context, tx *sql.Tx, id uuid.UUID) (models.AllowanceResetBatch, error) {
	return getResetBatchRow(tx.QueryRowContext(ctx, `SELECT allowance_reset_batch_uuid,filter_snapshot,status,reason,total_items,succeeded_items,failed_items,requested_by_user_uuid,approved_by_user_uuid,created_at,completed_at FROM allowance_reset_batches WHERE allowance_reset_batch_uuid=$1`, id))
}
func getResetBatchRow(row interface{ Scan(...any) error }) (models.AllowanceResetBatch, error) {
	var b models.AllowanceResetBatch
	var snapshot []byte
	var completed sql.NullTime
	err := row.Scan(&b.ID, &snapshot, &b.Status, &b.Reason, &b.Total, &b.Succeeded, &b.Failed, &b.RequestedBy, &b.ApprovedBy, &b.CreatedAt, &completed)
	if err != nil {
		return b, err
	}
	var parsed struct {
		OwnerType string `json:"owner_type"`
	}
	_ = json.Unmarshal(snapshot, &parsed)
	b.OwnerType = parsed.OwnerType
	b.RequiresSecondApproval = b.Total > resetSecondApprovalThreshold
	if completed.Valid {
		x := completed.Time
		b.CompletedAt = &x
	}
	return b, nil
}
