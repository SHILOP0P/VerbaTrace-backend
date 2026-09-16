package supportaccess

import (
	"context"
	"database/sql"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// CompanyJournal answers the question a customer is entitled to ask: who from
// support looked at our data, when, and why. It is the company's own record,
// not an admin tool, so any active member may read it.
func (s *Service) CompanyJournal(ctx context.Context, companyID, actor uuid.UUID, limit int) ([]models.SupportAccessJournalEntry, error) {
	if companyID == uuid.Nil || actor == uuid.Nil {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	var member bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, companyID, actor).Scan(&member); err != nil {
		return nil, fmt.Errorf("check company membership: %w", err)
	}
	if !member {
		return nil, ErrForbidden
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT e.event_uuid,
		       e.event_type,
		       COALESCE(e.resource,''),
		       COALESCE(e.command,''),
		       e.created_at,
		       COALESCE(p.username,''),
		       COALESCE(r.reason,''),
		       g.expires_at
		FROM support_access_events e
		LEFT JOIN support_access_requests r ON r.request_uuid = e.request_uuid
		LEFT JOIN support_access_grants g ON g.grant_uuid = e.grant_uuid
		LEFT JOIN user_profiles p ON p.user_uuid = e.actor_user_uuid
		WHERE COALESCE(r.subject_company_uuid, g.subject_company_uuid) = $1
		ORDER BY e.created_at DESC, e.event_uuid DESC
		LIMIT $2`, companyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list support access journal: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []models.SupportAccessJournalEntry{}
	for rows.Next() {
		var item models.SupportAccessJournalEntry
		var expiresAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.EventType, &item.Resource, &item.Command, &item.CreatedAt, &item.ActorUsername, &item.Reason, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan support access journal: %w", err)
		}
		if expiresAt.Valid {
			value := expiresAt.Time.UTC()
			item.AccessExpiresAt = &value
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
