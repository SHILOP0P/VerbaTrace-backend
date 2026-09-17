package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// auditTrailSources is every append-only record the application keeps. Each was
// written diligently and read by nothing: there was no endpoint, no screen and
// no way to look at any of them short of opening the database by hand. They are
// listed here once so the admin panel can show them all through one query.
//
// The columns differ between them, so each source says how to present itself as
// the same four fields: when, who, what, and the details.
var auditTrailSources = map[models.AdminAuditTrail]string{
	models.AdminAuditTrailAdminActions: `SELECT created_at AS occurred_at, NULL::uuid AS entry_uuid, actor_user_uuid, action,
		jsonb_build_object('actor_role', actor_role, 'target_type', target_type, 'target_uuid', target_uuid, 'reason', reason, 'ip_address', ip_address) AS details
		FROM admin_audit_logs`,

	models.AdminAuditTrailBillingAlerts: `SELECT created_at AS occurred_at, billing_alert_uuid AS entry_uuid, NULL::uuid AS actor_user_uuid, alert_type AS action,
		jsonb_build_object('severity', severity, 'status', status, 'billing_account_uuid', billing_account_uuid, 'usage_operation_uuid', usage_operation_uuid, 'details', details, 'resolved_at', resolved_at) AS details
		FROM billing_alerts`,

	models.AdminAuditTrailCreditReconciliation: `SELECT created_at AS occurred_at, NULL::uuid AS entry_uuid, NULL::uuid AS actor_user_uuid, status AS action,
		jsonb_build_object('provider', provider, 'period_start', period_start, 'period_end', period_end, 'checked', checked_operations, 'mismatched', mismatched_operations, 'completed_at', completed_at, 'details', details) AS details
		FROM billing_reconciliation_runs`,

	models.AdminAuditTrailRetention: `SELECT created_at AS occurred_at, NULL::uuid AS entry_uuid, NULL::uuid AS actor_user_uuid, event_type AS action,
		jsonb_build_object('entity_type', entity_type, 'entity_uuid', entity_uuid, 'item_count', item_count, 'byte_count', byte_count, 'metadata', metadata_safe) AS details
		FROM retention_audit_events`,

	models.AdminAuditTrailTranscriptEdits: `SELECT created_at AS occurred_at, NULL::uuid AS entry_uuid, actor_user_uuid, operation AS action,
		jsonb_build_object('transcription_uuid', transcription_uuid, 'revision', revision, 'reason', reason) AS details
		FROM call_transcription_edit_audit`,

	models.AdminAuditTrailCommentRevisions: `SELECT created_at AS occurred_at, NULL::uuid AS entry_uuid, editor_user_uuid AS actor_user_uuid, 'comment_revised' AS action,
		jsonb_build_object('comment_uuid', comment_uuid, 'revision_uuid', revision_uuid) AS details
		FROM call_analysis_comment_revisions`,
}

// ListAuditTrail reads one of the append-only trails. They are written by
// several unrelated parts of the system, so the query is assembled from a fixed
// table of sources rather than taking a table name from the caller.
func (r *Repository) ListAuditTrail(ctx context.Context, input models.ListAdminAuditTrailInput) (models.ListAdminAuditTrailResult, error) {
	source, known := auditTrailSources[input.Trail]
	if !known {
		return models.ListAdminAuditTrailResult{}, models.ErrInvalidAdminInput
	}
	if input.Limit < 1 || input.Limit > 200 {
		input.Limit = 50
	}
	if input.Offset < 0 {
		input.Offset = 0
	}

	conditions := []string{"TRUE"}
	args := []any{}
	if input.From != nil {
		args = append(args, *input.From)
		conditions = append(conditions, fmt.Sprintf("source.occurred_at >= $%d", len(args)))
	}
	if input.To != nil {
		args = append(args, *input.To)
		conditions = append(conditions, fmt.Sprintf("source.occurred_at < $%d", len(args)))
	}
	args = append(args, input.Limit, input.Offset)

	// The actor's handle is joined in rather than left to the reader: the screen
	// has to name who acted, and the uuid the trail stores is not a name.
	query := fmt.Sprintf(`
		SELECT source.occurred_at, source.entry_uuid, source.actor_user_uuid, actor.username, source.action, source.details, COUNT(*) OVER() AS total
		FROM (%s) source
		LEFT JOIN user_profiles actor ON actor.user_uuid = source.actor_user_uuid
		WHERE %s
		ORDER BY source.occurred_at DESC
		LIMIT $%d OFFSET $%d`,
		source, strings.Join(conditions, " AND "), len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.ListAdminAuditTrailResult{}, fmt.Errorf("list audit trail %s: %w", input.Trail, err)
	}
	defer func() { _ = rows.Close() }()

	result := models.ListAdminAuditTrailResult{Trail: input.Trail, Items: []models.AdminAuditTrailEntry{}, Limit: input.Limit, Offset: input.Offset}
	for rows.Next() {
		var entry models.AdminAuditTrailEntry
		var details []byte
		var total int
		if err = rows.Scan(&entry.OccurredAt, &entry.EntryUUID, &entry.ActorUserUUID, &entry.ActorUsername, &entry.Action, &details, &total); err != nil {
			return models.ListAdminAuditTrailResult{}, fmt.Errorf("scan audit trail %s: %w", input.Trail, err)
		}
		entry.Details = json.RawMessage(details)
		result.Items = append(result.Items, entry)
		result.Total = total
	}

	return result, rows.Err()
}

// ResolveBillingAlert closes an alert an administrator has dealt with. Only
// alerts have a state: they are a to-do list the system writes for itself, and
// without a way to close one the list grows until nobody reads it.
func (r *Repository) ResolveBillingAlert(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `UPDATE billing_alerts SET status='resolved', resolved_at=now() WHERE billing_alert_uuid=$1 AND status<>'resolved'`, id)
	if err != nil {
		return fmt.Errorf("resolve billing alert: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve billing alert: %w", err)
	}
	if affected == 0 {
		return models.ErrAdminRecordNotFound
	}

	return nil
}
