package admin

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// auditTrailRepository is the read side of the append-only records. It is a
// separate interface so the admin service keeps working where the repository
// predates the trails — the handler answers "not available" instead of failing.
type auditTrailRepository interface {
	ListAuditTrail(ctx context.Context, input models.ListAdminAuditTrailInput) (models.ListAdminAuditTrailResult, error)
	ResolveBillingAlert(ctx context.Context, id uuid.UUID) error
}

// ListAuditTrail opens one of the records the system has always written and
// never shown. They name no customer content, so panel access is enough and no
// support grant is involved.
func (s *Service) ListAuditTrail(ctx context.Context, input models.ListAdminAuditTrailInput) (models.ListAdminAuditTrailResult, error) {
	if !input.Trail.Valid() {
		return models.ListAdminAuditTrailResult{}, models.ErrInvalidAdminInput
	}

	repository, ok := s.auditRepository.(auditTrailRepository)
	if !ok {
		return models.ListAdminAuditTrailResult{}, errAuditRepositoryNotConfigured
	}

	return repository.ListAuditTrail(ctx, input)
}

// ResolveBillingAlert closes an alert somebody has dealt with. Alerts are the
// only trail with a state: they are a to-do list the system writes for itself,
// and an unclosable list stops being read. Closing one is itself an admin
// action, so it goes into the audit log with its reason.
func (s *Service) ResolveBillingAlert(ctx context.Context, input models.ResolveBillingAlertInput, audit models.CreateAdminAuditLogInput) error {
	if input.AlertUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.ErrInvalidAdminInput
	}
	if normalizeOptionalString(&input.Reason) == nil {
		return models.ErrAdminReasonRequired
	}

	repository, ok := s.auditRepository.(auditTrailRepository)
	if !ok {
		return errAuditRepositoryNotConfigured
	}

	if err := repository.ResolveBillingAlert(ctx, input.AlertUUID); err != nil {
		return err
	}

	audit.ActorUserUUID = input.RequestUser
	audit.Action = "billing_alert_resolved"
	audit.TargetType = "billing_alert"
	audit.TargetUUID = uuid.NullUUID{UUID: input.AlertUUID, Valid: true}
	audit.Reason = &input.Reason
	if _, err := s.RecordAudit(ctx, audit); err != nil {
		return err
	}

	return nil
}
