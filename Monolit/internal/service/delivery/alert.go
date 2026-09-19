package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// defaultAlertThreshold is the score under which a call counts as failed when
// nobody changed the setting.
const defaultAlertThreshold = 50

// criticalMissBelow matches the facts: a critical criterion under this score
// is a miss.
const criticalMissBelow = 13

// CallAnalyzed raises the alert about a failed call, once per call: a second
// analysis of the same call does not raise it again. It never fails the caller.
func (s *Service) CallAnalyzed(ctx context.Context, callID uuid.UUID) {
	if err := s.alert(ctx, callID); err != nil {
		s.log.Warn(ctx, "critical call alert not delivered", zap.String("call_id", callID.String()), zap.Error(err))
	}
}

func (s *Service) alert(ctx context.Context, callID uuid.UUID) error {
	var (
		title               string
		company, department uuid.NullUUID
		uploader            uuid.NullUUID
		overall             sql.NullInt64
		criticalMissed      int
		internal, active    bool
		threshold           int
		missedCriterion     sql.NullString
		employee            sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT c.title, c.company_uuid, c.department_uuid, c.uploaded_by_user_uuid, f.overall_score, f.critical_missed, f.is_internal,
		       c.company_uuid IS NULL OR EXISTS (SELECT 1 FROM companies co WHERE co.company_uuid = c.company_uuid AND co.lifecycle_state = 'active' AND co.deleted_at IS NULL),
		       CASE WHEN c.company_uuid IS NULL THEN COALESCE((SELECT critical_alert_threshold FROM user_preferences WHERE user_uuid = c.uploaded_by_user_uuid), $2)
		            ELSE COALESCE((SELECT critical_alert_threshold FROM company_analytics_settings WHERE company_uuid = c.company_uuid), $2) END,
		       (SELECT sc.title FROM analytics_criterion_facts k
		          JOIN instruction_scorecard_criteria sc ON sc.criterion_key = k.criterion_key AND sc.scorecard_uuid = k.scorecard_uuid
		         WHERE k.call_uuid = c.call_uuid AND k.is_critical AND k.score < $3 ORDER BY k.weight DESC, sc.position LIMIT 1),
		       (SELECT COALESCE(btrim(p.full_name || ' ' || p.full_surname), '') FROM call_subjects cs LEFT JOIN user_profiles p ON p.user_uuid = cs.user_uuid
		         WHERE cs.call_uuid = c.call_uuid ORDER BY cs.is_primary DESC, cs.user_uuid LIMIT 1)
		FROM calls c JOIN analytics_call_facts f ON f.call_uuid = c.call_uuid
		WHERE c.call_uuid = $1 AND c.deleted_at IS NULL`, callID, defaultAlertThreshold, criticalMissBelow).
		Scan(&title, &company, &department, &uploader, &overall, &criticalMissed, &internal, &active, &threshold, &missedCriterion, &employee)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read call for alert: %w", err)
	}
	// An internal call is not scored against a sales checklist, and a frozen
	// company sends nothing.
	if internal || !active {
		return nil
	}
	var reason string
	switch {
	case criticalMissed > 0:
		name := missedCriterion.String
		if name == "" {
			name = "критичный критерий"
		}
		reason = "Пропущен критичный критерий: " + name
	case overall.Valid && int(overall.Int64) < threshold:
		// A call without an overall score never trips the threshold.
		reason = "Оценка ниже порога " + strconv.Itoa(threshold)
	default:
		return nil
	}
	recipients, err := s.alertRecipients(ctx, callID, company, department, uploader)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}
	who := employee.String
	if who == "" {
		who = "Сотрудник"
	}
	score := "—"
	if overall.Valid {
		score = strconv.FormatInt(overall.Int64, 10)
	}
	body := fmt.Sprintf("%s: «%s», оценка %s. %s", who, title, score, reason)
	text := fmt.Sprintf("Звонок требует внимания\n%s: оценка %s. %s\nОткрыть: %s", who, score, reason, s.Link("/app/calls?call="+callID.String()))
	events := make([]Event, 0, len(recipients))
	for _, user := range recipients {
		events = append(events, Event{
			Kind: KindCriticalCallAlert, User: user, Notification: models.NotificationTypeCriticalCallAlert,
			Title: "Звонок требует внимания", Body: body, EntityType: "call", EntityID: uuid.NullUUID{UUID: callID, Valid: true},
			Company: company, Text: text,
		})
	}
	_, err = s.Deliver(ctx, "alert:"+callID.String(), events)
	return err
}

// alertRecipients are the one who answers for the call — the leader of its
// department, else the deputy, else the owner — and the employees of the call,
// who learn about their own call in the bell. A personal call alerts its owner.
func (s *Service) alertRecipients(ctx context.Context, callID uuid.UUID, company, department, uploader uuid.NullUUID) ([]uuid.UUID, error) {
	if !company.Valid {
		if uploader.Valid {
			return []uuid.UUID{uploader.UUID}, nil
		}
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH leaders AS (
			SELECT dm.user_uuid FROM department_members dm
			JOIN company_members cm ON cm.company_uuid = $1 AND cm.user_uuid = dm.user_uuid AND cm.status = 'active'
			WHERE $2::uuid IS NOT NULL AND dm.department_uuid = $2 AND dm.role = 'department_leader' AND dm.status = 'active'
		), deputies AS (
			SELECT user_uuid FROM company_members WHERE company_uuid = $1 AND role = 'company_deputy' AND status = 'active'
		), owners AS (
			SELECT user_uuid FROM company_members WHERE company_uuid = $1 AND role = 'company_manager' AND status = 'active'
		), responsible AS (
			SELECT user_uuid FROM leaders
			UNION ALL SELECT user_uuid FROM deputies WHERE NOT EXISTS (SELECT 1 FROM leaders)
			UNION ALL SELECT user_uuid FROM owners WHERE NOT EXISTS (SELECT 1 FROM leaders) AND NOT EXISTS (SELECT 1 FROM deputies)
		)
		SELECT user_uuid FROM responsible
		UNION
		SELECT cs.user_uuid FROM call_subjects cs
		JOIN company_members m ON m.company_uuid = $1 AND m.user_uuid = cs.user_uuid AND m.status = 'active'
		WHERE cs.call_uuid = $3`, company.UUID, department, callID)
	if err != nil {
		return nil, fmt.Errorf("read alert recipients: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var users []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			users = append(users, id)
		}
	}
	return users, rows.Err()
}
