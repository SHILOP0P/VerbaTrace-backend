package bitrix24

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// A call's summary goes into the CRM card it came from: the deal, lead, contact
// or company its Bitrix24 activity belongs to. One comment per call and
// connection; a new analysis or a published QA revision updates it.
const (
	CRMNoteModeOff  = "off"
	CRMNoteModeAuto = "auto"

	crmNoteMaxAttempts = 5
	crmNoteLease       = 2 * time.Minute
	// criticalMissBelow matches the analytics facts.
	criticalMissBelow = 13
	crmOutcomeRunes   = 300
)

var (
	// Bitrix24 CRM owner types of an activity.
	crmEntityTypes     = map[string]string{"1": "lead", "2": "deal", "3": "contact", "4": "company"}
	crmSpeakerMarker   = regexp.MustCompile(`\{\{speaker:([^{}]*)\}\}`)
	errCRMNoteSkipped  = errors.New("crm note is not wanted")
	errCRMNoteNotFound = errors.New("crm comment not found")
)

// SetAppURL is where the link in a CRM comment points.
func (s *Service) SetAppURL(url string) { s.appURL = strings.TrimRight(url, "/") }

// QueueCRMNote asks for the call's summary in its CRM card when the call came
// from a connection that writes notes. It never fails the caller.
func (s *Service) QueueCRMNote(ctx context.Context, callID uuid.UUID) {
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO integration_crm_notes (call_uuid, connection_uuid, status, available_at)
		SELECT c.call_uuid, i.connection_uuid, 'pending', now()
		FROM calls c
		JOIN ingest_items i ON i.ingest_item_uuid = c.ingest_item_uuid
		JOIN integration_connections ic ON ic.connection_uuid = i.connection_uuid AND ic.provider = 'bitrix24'
		WHERE c.call_uuid = $1 AND COALESCE(ic.settings->>'crm_note_mode', 'off') = 'auto'
		  AND COALESCE(i.metadata_redacted->>'crm_activity_id', '') NOT IN ('', '0')
		ON CONFLICT (call_uuid, connection_uuid) DO UPDATE
		SET status = 'pending', attempts = 0, available_at = now(), last_error = NULL, lease_until = NULL, updated_at = now()`, callID)
}

type crmNote struct {
	call, connection uuid.UUID
	attempts         int
	commentID        string
}

// processCRMNotes writes the notes that are due. Several replicas may run it:
// a note is leased before it is written.
func (s *Service) processCRMNotes(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		UPDATE integration_crm_notes n SET status = 'writing', attempts = n.attempts + 1, lease_until = now() + make_interval(secs => $1), updated_at = now()
		WHERE (n.call_uuid, n.connection_uuid) IN (
			SELECT call_uuid, connection_uuid FROM integration_crm_notes
			WHERE (status = 'pending' AND available_at <= now()) OR (status = 'writing' AND lease_until < now())
			ORDER BY available_at FOR UPDATE SKIP LOCKED LIMIT 10)
		RETURNING n.call_uuid, n.connection_uuid, n.attempts, COALESCE(n.external_comment_id, '')`, crmNoteLease.Seconds())
	if err != nil {
		return
	}
	var notes []crmNote
	for rows.Next() {
		var note crmNote
		if rows.Scan(&note.call, &note.connection, &note.attempts, &note.commentID) == nil {
			notes = append(notes, note)
		}
	}
	_ = rows.Close()
	for _, note := range notes {
		if ctx.Err() != nil {
			return
		}
		s.finishCRMNote(ctx, note, s.writeCRMNote(ctx, note))
	}
}

type crmWrite struct {
	entityType, entityID, commentID string
	analysis                        uuid.UUID
}

func (s *Service) writeCRMNote(ctx context.Context, note crmNote) error {
	var (
		mode, status, activity string
		active                 bool
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(ic.settings->>'crm_note_mode', 'off'), ic.status, COALESCE(i.metadata_redacted->>'crm_activity_id', ''),
		       NOT EXISTS (SELECT 1 FROM companies co WHERE co.company_uuid = c.company_uuid AND (co.lifecycle_state <> 'active' OR co.deleted_at IS NOT NULL))
		FROM calls c
		JOIN ingest_items i ON i.ingest_item_uuid = c.ingest_item_uuid
		JOIN integration_connections ic ON ic.connection_uuid = $2
		WHERE c.call_uuid = $1 AND c.deleted_at IS NULL`, note.call, note.connection).Scan(&mode, &status, &activity, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return errCRMNoteSkipped
	}
	if err != nil {
		return err
	}
	// The switch was turned off, the connection paused or the company frozen:
	// nothing is written, as with the rest of the connector.
	if mode != CRMNoteModeAuto || (status != "active" && status != "degraded") || !active || activity == "" {
		return errCRMNoteSkipped
	}
	text, analysis, err := s.crmNoteText(ctx, note.call)
	if err != nil {
		return err
	}
	info, token, err := s.tokenFor(ctx, note.connection)
	if err != nil {
		return err
	}
	written := crmWrite{analysis: analysis, commentID: note.commentID}
	if note.commentID != "" {
		err = s.updateCRMComment(ctx, info.Domain, token, note.commentID, text)
		if err == nil {
			return s.saveCRMNote(ctx, note, written)
		}
		if !errors.Is(err, errCRMNoteNotFound) {
			return err
		}
		// Somebody deleted the comment in the card: write a new one.
	}
	var owner struct {
		TypeID  any `json:"OWNER_TYPE_ID"`
		OwnerID any `json:"OWNER_ID"`
	}
	if err := s.portalCall(ctx, info.Domain, "crm.activity.get", token, map[string]any{"id": activity}, &owner); err != nil {
		return err
	}
	entityType, ok := crmEntityTypes[fmt.Sprint(owner.TypeID)]
	entityID := fmt.Sprint(owner.OwnerID)
	if !ok || entityID == "" || entityID == "0" || entityID == "<nil>" {
		return errCRMNoteSkipped
	}
	var created any
	if err := s.portalCall(ctx, info.Domain, "crm.timeline.comment.add", token, map[string]any{
		"fields": map[string]any{"ENTITY_ID": entityID, "ENTITY_TYPE": entityType, "COMMENT": text},
	}, &created); err != nil {
		return err
	}
	written.entityType, written.entityID, written.commentID = entityType, entityID, fmt.Sprint(created)
	return s.saveCRMNote(ctx, note, written)
}

func (s *Service) updateCRMComment(ctx context.Context, domain, token, commentID, text string) error {
	var result any
	err := s.portalCall(ctx, domain, "crm.timeline.comment.update", token, map[string]any{"id": commentID, "fields": map[string]any{"COMMENT": text}}, &result)
	// The portal says NOT_FOUND or "Not found" for a comment deleted in the card.
	if err != nil && strings.Contains(strings.ReplaceAll(strings.ToLower(err.Error()), "_", " "), "not found") {
		return errCRMNoteNotFound
	}
	return err
}

func (s *Service) saveCRMNote(ctx context.Context, note crmNote, written crmWrite) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE integration_crm_notes SET status = 'written', external_comment_id = $3,
		       entity_type = COALESCE(NULLIF($4, ''), entity_type), entity_id = COALESCE(NULLIF($5, ''), entity_id),
		       analysis_uuid = $6, last_error = NULL, lease_until = NULL, written_at = now(), updated_at = now()
		WHERE call_uuid = $1 AND connection_uuid = $2`, note.call, note.connection, written.commentID, written.entityType, written.entityID, written.analysis)
	return err
}

func (s *Service) finishCRMNote(ctx context.Context, note crmNote, err error) {
	switch {
	case err == nil:
	case errors.Is(err, errCRMNoteSkipped):
		_, _ = s.db.ExecContext(ctx, `UPDATE integration_crm_notes SET status = 'skipped', lease_until = NULL, updated_at = now() WHERE call_uuid = $1 AND connection_uuid = $2`, note.call, note.connection)
	case note.attempts >= crmNoteMaxAttempts:
		_, _ = s.db.ExecContext(ctx, `UPDATE integration_crm_notes SET status = 'failed', last_error = $3, lease_until = NULL, updated_at = now() WHERE call_uuid = $1 AND connection_uuid = $2`, note.call, note.connection, safeError(err))
	default:
		delay := time.Minute << (note.attempts - 1)
		_, _ = s.db.ExecContext(ctx, `UPDATE integration_crm_notes SET status = 'pending', last_error = $3, lease_until = NULL, available_at = now() + make_interval(secs => $4), updated_at = now() WHERE call_uuid = $1 AND connection_uuid = $2`,
			note.call, note.connection, safeError(err), delay.Seconds())
	}
}

func safeError(err error) string {
	message := err.Error()
	if utf8.RuneCountInString(message) > 300 {
		message = string([]rune(message)[:300])
	}
	return message
}

// crmNoteText is the comment, built by a template (spec 15.5): numbers, the
// outcome, what to work on and critical misses. No quotes of the conversation;
// speakers are named as the transcript's roles name them.
func (s *Service) crmNoteText(ctx context.Context, callID uuid.UUID) (string, uuid.UUID, error) {
	var (
		analysis                 uuid.UUID
		result                   string
		occurred                 time.Time
		overall, criteria, human sql.NullInt64
		missed                   sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT a.analysis_uuid, COALESCE(effective_call_analysis_json(a.analysis_uuid, a.result_json)::text, '{}'),
		       COALESCE(f.occurred_at, c.created_at), f.overall_score, f.criteria_score, f.human_overall_score,
		       (SELECT string_agg(sc.title, '; ' ORDER BY sc.position) FROM analytics_criterion_facts k
		          JOIN instruction_scorecard_criteria sc ON sc.criterion_key = k.criterion_key AND sc.scorecard_uuid = k.scorecard_uuid
		         WHERE k.call_uuid = c.call_uuid AND k.is_critical AND k.score < $2)
		FROM calls c
		JOIN call_analyses a ON a.call_uuid = c.call_uuid AND a.status = 'done'
		LEFT JOIN analytics_call_facts f ON f.call_uuid = c.call_uuid
		WHERE c.call_uuid = $1`, callID, criticalMissBelow).Scan(&analysis, &result, &occurred, &overall, &criteria, &human, &missed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", uuid.Nil, errCRMNoteSkipped
	}
	if err != nil {
		return "", uuid.Nil, err
	}
	var parsed struct {
		Outcome any   `json:"outcome"`
		WorkOn  []any `json:"work_on"`
	}
	_ = json.Unmarshal([]byte(result), &parsed)
	names, err := s.speakerNames(ctx, callID)
	if err != nil {
		return "", uuid.Nil, err
	}
	name := func(text string) string {
		return crmSpeakerMarker.ReplaceAllStringFunc(text, func(marker string) string {
			if display := names[crmSpeakerMarker.FindStringSubmatch(marker)[1]]; display != "" {
				return display
			}
			return "участник"
		})
	}
	moscow, _ := time.LoadLocation("Europe/Moscow")
	if moscow == nil {
		moscow = time.UTC
	}
	lines := []string{"VerbaTrace: разбор звонка от " + occurred.In(moscow).Format("02.01.2006 15:04")}
	if overall.Valid {
		line := "Оценка: " + strconv.FormatInt(overall.Int64, 10) + " из 100"
		if criteria.Valid {
			line += ", по критериям: " + strconv.FormatInt(criteria.Int64, 10)
		}
		lines = append(lines, line)
	}
	if outcome := strings.TrimSpace(name(textOf(parsed.Outcome))); outcome != "" {
		if utf8.RuneCountInString(outcome) > crmOutcomeRunes {
			outcome = strings.TrimSpace(string([]rune(outcome)[:crmOutcomeRunes])) + "…"
		}
		lines = append(lines, "Итог: "+outcome)
	}
	var work []string
	for _, item := range parsed.WorkOn {
		if value := strings.TrimSpace(name(textOf(item))); value != "" && len(work) < 2 {
			work = append(work, value)
		}
	}
	if len(work) > 0 {
		lines = append(lines, "Над чем поработать: "+strings.Join(work, "; "))
	}
	if missed.Valid && missed.String != "" {
		lines = append(lines, "Критичные пропуски: "+missed.String)
	}
	if human.Valid {
		lines = append(lines, "Оценку проверил человек.")
	}
	lines = append(lines, "Подробнее: "+s.appURL+"/app/calls?call="+callID.String())
	return strings.Join(lines, "\n"), analysis, nil
}

// textOf reads a summary field that is a string or an object with a text.
func textOf(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"text", "summary", "title"} {
			if text, ok := v[key].(string); ok {
				return text
			}
		}
	}
	return ""
}

func (s *Service) speakerNames(ctx context.Context, callID uuid.UUID) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT speaker_key, COALESCE(display_name, '') FROM call_transcription_speaker_assignments WHERE call_uuid = $1`, callID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	names := map[string]string{}
	for rows.Next() {
		var key, name string
		if rows.Scan(&key, &name) == nil {
			names[key] = strings.TrimSpace(name)
		}
	}
	return names, rows.Err()
}

// SetCRMNoteMode switches the notes of a connection on or off. The owner and
// the deputy of the connection's company may.
func (s *Service) SetCRMNoteMode(ctx context.Context, connectionID, actor uuid.UUID, mode string, expectedVersion int64) (models.BitrixConnectionHealth, error) {
	if mode != CRMNoteModeOff && mode != CRMNoteModeAuto {
		return models.BitrixConnectionHealth{}, ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE integration_connections c SET settings = jsonb_set(c.settings, '{crm_note_mode}', to_jsonb($4::text), true), lock_version = c.lock_version + 1, updated_at = now()
		WHERE c.connection_uuid = $1 AND c.provider = 'bitrix24' AND c.lock_version = $3 AND `+managerAccessSQL, connectionID, actor, expectedVersion, mode)
	if err != nil {
		return models.BitrixConnectionHealth{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return models.BitrixConnectionHealth{}, ErrConflict
	}
	return s.Health(ctx, connectionID, actor)
}
