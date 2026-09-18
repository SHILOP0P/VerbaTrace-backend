// Package growth keeps growth areas: recurring shortcomings outside the
// scorecard that the summary step matches between an employee's calls. It is
// the approximate layer of the work on mistakes and is labelled as such.
package growth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// maxOpenAreas bounds what the summary step is told about, and so the
	// tokens growth areas add to one request.
	maxOpenAreas  = 12
	maxNewAreas   = 3
	maxTitleRunes = 60
	resolveStreak = 3
	maxReasonLen  = 500
)

type Service struct {
	db  *sql.DB
	log logger.Logger
}

func NewService(db *sql.DB, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, log: log}
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// shape is what decides whether a call keeps growth areas and for whom.
type shape struct {
	company          uuid.NullUUID
	shared, internal bool
	subjects         int
	subject          uuid.NullUUID
	speaker          string
	enabled          bool
	personalPaid     bool
}

func readShape(ctx context.Context, q querier, callID uuid.UUID) (shape, error) {
	var sh shape
	err := q.QueryRowContext(ctx, `
		SELECT c.company_uuid, COALESCE(st.is_shared, false), COALESCE(st.is_internal, false),
		       (SELECT count(*) FROM call_subjects cs WHERE cs.call_uuid = c.call_uuid),
		       s.user_uuid, COALESCE(s.speaker_key, ''),
		       CASE WHEN c.company_uuid IS NULL THEN COALESCE(up.growth_areas_enabled, true)
		            ELSE COALESCE(cas.growth_areas_enabled, true) END,
		       EXISTS (SELECT 1 FROM subscriptions sub JOIN plans p ON p.plan_uuid = sub.plan_uuid
		               WHERE sub.type = 'personal' AND sub.user_uuid = c.uploaded_by_user_uuid AND sub.status = 'active'
		                 AND sub.starts_at <= now() AND (sub.ends_at IS NULL OR sub.ends_at > now()) AND p.personal_progress_enabled)
		FROM calls c
		LEFT JOIN call_subject_states st ON st.call_uuid = c.call_uuid
		LEFT JOIN LATERAL (SELECT user_uuid, speaker_key FROM call_subjects WHERE call_uuid = c.call_uuid ORDER BY is_primary DESC, user_uuid LIMIT 1) s ON true
		LEFT JOIN user_preferences up ON up.user_uuid = c.uploaded_by_user_uuid
		LEFT JOIN company_analytics_settings cas ON cas.company_uuid = c.company_uuid
		WHERE c.call_uuid = $1 AND c.deleted_at IS NULL`, callID).
		Scan(&sh.company, &sh.shared, &sh.internal, &sh.subjects, &sh.subject, &sh.speaker, &sh.enabled, &sh.personalPaid)
	if errors.Is(err, sql.ErrNoRows) {
		return shape{}, models.ErrCallNotFound
	}
	if err != nil {
		return shape{}, fmt.Errorf("read growth shape: %w", err)
	}
	return sh, nil
}

// oneEmployee is a call whose mistakes belong to one person: not shared, not
// internal, one subject.
func (sh shape) oneEmployee() bool {
	return !sh.shared && !sh.internal && sh.subjects == 1 && sh.subject.Valid
}

// keeps says whether a new analysis of the call should look at growth areas:
// the switch is on, the only employee is bound to a speaker the model can
// judge, and a personal account pays for its own progress.
func (sh shape) keeps() bool {
	return sh.oneEmployee() && sh.speaker != "" && sh.enabled && (sh.company.Valid || sh.personalPaid)
}

// ContextFor is the growth context of a call's next analysis, or nil when the
// call keeps no growth areas; the summary step then runs exactly as before.
func (s *Service) ContextFor(ctx context.Context, callID uuid.UUID) (*models.GrowthContext, error) {
	sh, err := readShape(ctx, s.db, callID)
	if err != nil {
		return nil, err
	}
	if !sh.keeps() {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT area_uuid, title, description FROM growth_areas
		WHERE subject_user_uuid = $1 AND company_uuid IS NOT DISTINCT FROM $2 AND status = 'open'
		ORDER BY occurrences DESC, last_seen_at DESC NULLS LAST, created_at, area_uuid
		LIMIT $3`, sh.subject.UUID, sh.company, maxOpenAreas)
	if err != nil {
		return nil, fmt.Errorf("read open growth areas: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := &models.GrowthContext{SubjectSpeaker: sh.speaker, OpenAreas: []models.GrowthAreaRef{}}
	for rows.Next() {
		var ref models.GrowthAreaRef
		var id uuid.UUID
		if err := rows.Scan(&id, &ref.Title, &ref.Description); err != nil {
			return nil, fmt.Errorf("scan open growth area: %w", err)
		}
		ref.ID = id.String()
		out.OpenAreas = append(out.OpenAreas, ref)
	}
	return out, rows.Err()
}

// Record stores what an analysis said about growth areas. A new analysis of
// the same call replaces what the previous one said.
func (s *Service) Record(ctx context.Context, callID uuid.UUID, outcome models.GrowthOutcome) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin growth record: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM calls WHERE call_uuid = $1 FOR UPDATE`, callID); err != nil {
		return fmt.Errorf("lock call for growth: %w", err)
	}
	sh, err := readShape(ctx, tx, callID)
	if err != nil {
		return err
	}
	affected, err := forget(ctx, tx, callID)
	if err != nil {
		return err
	}
	if sh.oneEmployee() {
		more, err := s.observe(ctx, tx, callID, sh, outcome)
		if err != nil {
			return err
		}
		affected = append(affected, more...)
	}
	if err := replayAll(ctx, tx, affected); err != nil {
		return err
	}
	// An area the previous analysis of this call opened, that nothing supports
	// any more, goes: the new analysis would otherwise duplicate it.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM growth_areas a WHERE a.first_call_uuid = $1
		  AND NOT EXISTS (SELECT 1 FROM growth_area_observations o WHERE o.area_uuid = a.area_uuid)`, callID); err != nil {
		return fmt.Errorf("drop orphaned growth areas: %w", err)
	}
	return tx.Commit()
}

func (s *Service) observe(ctx context.Context, tx *sql.Tx, callID uuid.UUID, sh shape, outcome models.GrowthOutcome) ([]uuid.UUID, error) {
	var affected []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, o := range outcome.Observations {
		areaID, err := uuid.Parse(o.AreaID)
		if err != nil || seen[areaID] {
			continue
		}
		var owned bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM growth_areas WHERE area_uuid = $1 AND subject_user_uuid = $2 AND company_uuid IS NOT DISTINCT FROM $3)`,
			areaID, sh.subject.UUID, sh.company).Scan(&owned); err != nil {
			return nil, fmt.Errorf("check growth area owner: %w", err)
		}
		if !owned {
			// The area was passed for another employee; the call changed hands
			// while it was analysed.
			continue
		}
		if err := insertObservation(ctx, tx, areaID, callID, o.Verdict, o.ItemIDs, o.Note); err != nil {
			return nil, err
		}
		seen[areaID] = true
		affected = append(affected, areaID)
	}
	for i, area := range outcome.NewAreas {
		if i == maxNewAreas {
			break
		}
		title := clip(strings.TrimSpace(area.Title), maxTitleRunes)
		if title == "" || strings.TrimSpace(area.Description) == "" {
			continue
		}
		// A shortcoming that was resolved and is now seen again comes back as the
		// same area, marked as returned, rather than as a new one.
		var existing uuid.UUID
		err := tx.QueryRowContext(ctx, `
			SELECT area_uuid FROM growth_areas
			WHERE subject_user_uuid = $1 AND company_uuid IS NOT DISTINCT FROM $2 AND status <> 'dismissed'
			  AND lower(regexp_replace(title, '\s+', ' ', 'g')) = lower(regexp_replace($3, '\s+', ' ', 'g'))
			ORDER BY status = 'open' DESC, created_at LIMIT 1`, sh.subject.UUID, sh.company, title).Scan(&existing)
		switch {
		case err == nil:
			if seen[existing] {
				continue
			}
			if err := insertObservation(ctx, tx, existing, callID, models.GrowthVerdictRepeated, area.ItemIDs, area.Description); err != nil {
				return nil, err
			}
			seen[existing] = true
			affected = append(affected, existing)
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return nil, fmt.Errorf("find growth area by title: %w", err)
		}
		id := uuid.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO growth_areas (area_uuid, subject_user_uuid, company_uuid, title, description, first_call_uuid)
			VALUES ($1, $2, $3, $4, $5, $6)`, id, sh.subject.UUID, sh.company, title, strings.TrimSpace(area.Description), callID); err != nil {
			return nil, fmt.Errorf("create growth area: %w", err)
		}
		if err := insertObservation(ctx, tx, id, callID, models.GrowthVerdictNew, area.ItemIDs, ""); err != nil {
			return nil, err
		}
		seen[id] = true
		affected = append(affected, id)
	}
	return affected, nil
}

func insertObservation(ctx context.Context, tx *sql.Tx, areaID, callID uuid.UUID, verdict string, itemIDs []string, note string) error {
	if itemIDs == nil {
		itemIDs = []string{}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO growth_area_observations (area_uuid, call_uuid, verdict, item_ids, note)
		VALUES ($1, $2, $3, $4::text[], $5)
		ON CONFLICT (area_uuid, call_uuid) DO UPDATE SET verdict = EXCLUDED.verdict, item_ids = EXCLUDED.item_ids, note = EXCLUDED.note, created_at = now()`,
		areaID, callID, verdict, textArray(itemIDs), note); err != nil {
		return fmt.Errorf("record growth observation: %w", err)
	}
	return nil
}

// forget removes the observations of a call and says which areas they touched.
func forget(ctx context.Context, q querier, callID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.QueryContext(ctx, `DELETE FROM growth_area_observations WHERE call_uuid = $1 RETURNING area_uuid`, callID)
	if err != nil {
		return nil, fmt.Errorf("forget growth observations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// Reconcile follows a change of whom a call counts for. If the call now belongs
// to another employee, or became shared or internal, its observations go and the
// counters of the areas they touched are rebuilt from what is left. New
// observations appear only when someone analyses the call again: the paid step
// is never rerun on its own.
func (s *Service) Reconcile(ctx context.Context, callID uuid.UUID) {
	if err := s.reconcile(ctx, callID); err != nil {
		s.log.Warn(ctx, "growth areas not reconciled", zap.String("call_id", callID.String()), zap.Error(err))
	}
}

func (s *Service) reconcile(ctx context.Context, callID uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var owners []struct {
		subject uuid.UUID
		company uuid.NullUUID
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT a.subject_user_uuid, a.company_uuid FROM growth_area_observations o
		JOIN growth_areas a ON a.area_uuid = o.area_uuid WHERE o.call_uuid = $1`, callID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var owner struct {
			subject uuid.UUID
			company uuid.NullUUID
		}
		if rows.Scan(&owner.subject, &owner.company) == nil {
			owners = append(owners, owner)
		}
	}
	_ = rows.Close()
	if len(owners) == 0 {
		return nil
	}
	sh, err := readShape(ctx, tx, callID)
	if err != nil && !errors.Is(err, models.ErrCallNotFound) {
		return err
	}
	if err == nil && len(owners) == 1 && owners[0].company != sh.company {
		// The call moved to another company; its growth areas stay where they
		// were kept.
		return nil
	}
	stale := err != nil || !sh.oneEmployee() || len(owners) != 1 || owners[0].subject != sh.subject.UUID
	if !stale {
		return nil
	}
	affected, err := forget(ctx, tx, callID)
	if err != nil {
		return err
	}
	if err := replayAll(ctx, tx, affected); err != nil {
		return err
	}
	return tx.Commit()
}

func replayAll(ctx context.Context, tx *sql.Tx, areas []uuid.UUID) error {
	done := map[uuid.UUID]bool{}
	for _, id := range areas {
		if done[id] {
			continue
		}
		done[id] = true
		if err := replay(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// replay rebuilds an area's counters from its observations in call order:
// repeated resets the clean streak and counts an occurrence, improved extends
// the streak, three in a row resolve the area, and a repeat after that brings
// it back marked as returned. Not applicable changes nothing. A hidden area
// stays hidden.
func replay(ctx context.Context, tx *sql.Tx, areaID uuid.UUID) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT o.call_uuid, o.verdict, COALESCE(f.occurred_at, c.created_at) AS at
		FROM growth_area_observations o
		JOIN calls c ON c.call_uuid = o.call_uuid
		LEFT JOIN analytics_call_facts f ON f.call_uuid = o.call_uuid
		WHERE o.area_uuid = $1
		ORDER BY at, o.created_at, o.call_uuid`, areaID)
	if err != nil {
		return fmt.Errorf("read growth observations: %w", err)
	}
	var (
		occurrences, streak int
		status              = "open"
		returned            bool
		resolvedAt          *time.Time
		lastSeen            uuid.NullUUID
		lastSeenAt          *time.Time
	)
	for rows.Next() {
		var call uuid.UUID
		var verdict string
		var at time.Time
		if err := rows.Scan(&call, &verdict, &at); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan growth observation: %w", err)
		}
		switch verdict {
		case models.GrowthVerdictNew, models.GrowthVerdictRepeated:
			if status == "resolved" {
				status, returned, resolvedAt = "open", true, nil
			}
			occurrences++
			streak = 0
			lastSeen, lastSeenAt = uuid.NullUUID{UUID: call, Valid: true}, &at
		case models.GrowthVerdictImproved:
			streak++
			if streak >= resolveStreak && status == "open" {
				status, resolvedAt = "resolved", &at
			}
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE growth_areas SET occurrences = $2, clean_streak = $3, returned = $4,
		       status = CASE WHEN status = 'dismissed' THEN status ELSE $5 END,
		       resolved_at = CASE WHEN status = 'dismissed' THEN resolved_at ELSE $6 END,
		       last_seen_call_uuid = $7, last_seen_at = $8
		WHERE area_uuid = $1`, areaID, occurrences, streak, returned, status, resolvedAt, lastSeen, lastSeenAt); err != nil {
		return fmt.Errorf("update growth area: %w", err)
	}
	return nil
}

// Dismiss hides an area the model got wrong or that is not a mistake. The
// employee, the leader of their department, the deputy and the owner may; the
// reason is kept.
func (s *Service) Dismiss(ctx context.Context, actor, areaID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxReasonLen {
		return models.ErrInvalidGrowthAreaReason
	}
	return s.change(ctx, actor, areaID, func(tx *sql.Tx, status string) error {
		if status == "dismissed" {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE growth_areas SET status = 'dismissed', dismissed_by_user_uuid = $2, dismissed_at = now(), dismiss_reason = $3
			WHERE area_uuid = $1`, areaID, actor, reason); err != nil {
			return fmt.Errorf("dismiss growth area: %w", err)
		}
		return event(ctx, tx, areaID, actor, "dismiss", reason)
	})
}

// Reopen brings a hidden area back; its status follows its observations.
func (s *Service) Reopen(ctx context.Context, actor, areaID uuid.UUID) error {
	return s.change(ctx, actor, areaID, func(tx *sql.Tx, status string) error {
		if status != "dismissed" {
			return models.ErrGrowthAreaNotDismissed
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE growth_areas SET status = 'open', dismissed_by_user_uuid = NULL, dismissed_at = NULL, dismiss_reason = NULL
			WHERE area_uuid = $1`, areaID); err != nil {
			return fmt.Errorf("reopen growth area: %w", err)
		}
		if err := replay(ctx, tx, areaID); err != nil {
			return err
		}
		return event(ctx, tx, areaID, actor, "reopen", "")
	})
}

func (s *Service) change(ctx context.Context, actor, areaID uuid.UUID, apply func(*sql.Tx, string) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var (
		subject uuid.UUID
		company uuid.NullUUID
		status  string
	)
	err = tx.QueryRowContext(ctx, `SELECT subject_user_uuid, company_uuid, status FROM growth_areas WHERE area_uuid = $1 FOR UPDATE`, areaID).Scan(&subject, &company, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrGrowthAreaNotFound
	}
	if err != nil {
		return fmt.Errorf("read growth area: %w", err)
	}
	if actor != subject {
		allowed := false
		if company.Valid {
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS (SELECT 1 FROM company_members WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active' AND role IN ('company_manager','company_deputy'))
				    OR EXISTS (SELECT 1 FROM department_members l
				               JOIN department_members m ON m.department_uuid = l.department_uuid AND m.user_uuid = $3 AND m.status = 'active'
				               JOIN departments d ON d.department_uuid = l.department_uuid AND d.company_uuid = $1
				               WHERE l.user_uuid = $2 AND l.role = 'department_leader' AND l.status = 'active')`,
				company.UUID, actor, subject).Scan(&allowed); err != nil {
				return fmt.Errorf("check growth area rights: %w", err)
			}
		}
		if !allowed {
			// Someone who may not change the area does not learn it exists.
			return models.ErrGrowthAreaNotFound
		}
	}
	if err := apply(tx, status); err != nil {
		return err
	}
	return tx.Commit()
}

func event(ctx context.Context, tx *sql.Tx, areaID, actor uuid.UUID, action, reason string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO growth_area_events (event_uuid, area_uuid, actor_user_uuid, action, reason) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New(), areaID, actor, action, reason); err != nil {
		return fmt.Errorf("record growth area event: %w", err)
	}
	return nil
}

func clip(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return strings.TrimSpace(string([]rune(value)[:limit]))
}

// textArray is a PostgreSQL text[] literal.
func textArray(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}
