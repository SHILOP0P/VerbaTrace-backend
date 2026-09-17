package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AdminAuditTrail names one of the append-only records the application keeps.
// All of them were written and none were read: nothing in the product could show
// them, so an incident had to be investigated by opening the database directly.
type AdminAuditTrail string

const (
	AdminAuditTrailAdminActions         AdminAuditTrail = "admin_actions"
	AdminAuditTrailBillingAlerts        AdminAuditTrail = "billing_alerts"
	AdminAuditTrailCreditReconciliation AdminAuditTrail = "credit_reconciliation"
	AdminAuditTrailRetention            AdminAuditTrail = "retention"
	AdminAuditTrailTranscriptEdits      AdminAuditTrail = "transcript_edits"
	AdminAuditTrailCommentRevisions     AdminAuditTrail = "comment_revisions"
)

// AdminAuditTrails lists them in the order the panel shows them.
func AdminAuditTrails() []AdminAuditTrail {
	return []AdminAuditTrail{
		AdminAuditTrailAdminActions,
		AdminAuditTrailBillingAlerts,
		AdminAuditTrailCreditReconciliation,
		AdminAuditTrailRetention,
		AdminAuditTrailTranscriptEdits,
		AdminAuditTrailCommentRevisions,
	}
}

func (t AdminAuditTrail) Valid() bool {
	for _, known := range AdminAuditTrails() {
		if known == t {
			return true
		}
	}

	return false
}

// AdminAuditTrailEntry is one record, reduced to what every trail can answer:
// when it happened, who did it if anybody, what it was, and the rest as details.
type AdminAuditTrailEntry struct {
	OccurredAt time.Time
	// EntryUUID identifies the record where that means something. Only billing
	// alerts can be acted on, and only they carry one.
	EntryUUID     uuid.NullUUID
	ActorUserUUID uuid.NullUUID
	Action        string
	Details       json.RawMessage
}

// ResolveBillingAlertInput closes one alert. Alerts are the only trail with a
// state of its own: every other record is history and cannot change.
type ResolveBillingAlertInput struct {
	AlertUUID   uuid.UUID
	RequestUser uuid.UUID
	Reason      string
}

type ListAdminAuditTrailInput struct {
	Trail  AdminAuditTrail
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

type ListAdminAuditTrailResult struct {
	Trail  AdminAuditTrail
	Items  []AdminAuditTrailEntry
	Total  int
	Limit  int
	Offset int
}
