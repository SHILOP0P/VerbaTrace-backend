package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) CreateAdminAuditLog(ctx context.Context, audit models.AdminAuditLog) (models.AdminAuditLog, error) {
	created, err := insertAuditReturning(ctx, r.db, audit)
	if err != nil {
		return models.AdminAuditLog{}, fmt.Errorf("create admin audit log: %w", err)
	}
	return created, nil
}

type queryRowerAudit interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func insertAuditReturning(ctx context.Context, db queryRowerAudit, audit models.AdminAuditLog) (models.AdminAuditLog, error) {
	query := `
	INSERT INTO admin_audit_logs (
	    audit_uuid,
	    actor_user_uuid,
	    actor_role,
	    action,
	    target_type,
	    target_uuid,
	    before_data,
	    after_data,
	    reason,
	    request_id,
	    ip_address,
	    user_agent,
	    created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7::JSONB, $8::JSONB, $9, $10, $11::INET, $12, $13)
	RETURNING audit_uuid,
	          actor_user_uuid,
	          actor_role,
	          action,
	          target_type,
	          target_uuid,
	          before_data,
	          after_data,
	          reason,
	          request_id,
	          ip_address::TEXT,
	          user_agent,
	          created_at
	`

	created, err := scanAdminAuditLog(db.QueryRowContext(
		ctx,
		query,
		audit.ID,
		audit.ActorUserUUID,
		audit.ActorRole,
		audit.Action,
		audit.TargetType,
		nullableUUID(audit.TargetUUID),
		nullableJSON(audit.BeforeData),
		nullableJSON(audit.AfterData),
		audit.Reason,
		audit.RequestID,
		audit.IPAddress,
		audit.UserAgent,
		audit.CreatedAt,
	))
	return created, err
}

func insertAudit(ctx context.Context, tx *sql.Tx, audit models.AdminAuditLog) error {
	_, err := insertAuditReturning(ctx, tx, audit)
	return err
}

type auditRowScanner interface {
	Scan(dest ...any) error
}

func scanAdminAuditLog(row auditRowScanner) (models.AdminAuditLog, error) {
	var audit models.AdminAuditLog
	var actorRole string
	var beforeData []byte
	var afterData []byte
	var reason sql.NullString
	var requestID sql.NullString
	var ipAddress sql.NullString
	var userAgent sql.NullString

	if err := row.Scan(
		&audit.ID,
		&audit.ActorUserUUID,
		&actorRole,
		&audit.Action,
		&audit.TargetType,
		&audit.TargetUUID,
		&beforeData,
		&afterData,
		&reason,
		&requestID,
		&ipAddress,
		&userAgent,
		&audit.CreatedAt,
	); err != nil {
		return models.AdminAuditLog{}, err
	}

	audit.ActorRole = models.UserRole(actorRole)
	audit.BeforeData = json.RawMessage(beforeData)
	audit.AfterData = json.RawMessage(afterData)
	audit.Reason = nullStringPtr(reason)
	audit.RequestID = nullStringPtr(requestID)
	audit.IPAddress = nullStringPtr(ipAddress)
	audit.UserAgent = nullStringPtr(userAgent)

	return audit, nil
}

func nullableUUID(value uuid.NullUUID) any {
	if !value.Valid {
		return nil
	}

	return value.UUID
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}

	return []byte(value)
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	return &value.String
}
