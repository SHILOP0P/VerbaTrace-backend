// Package callsubject decides whom a call counts for: the employees of the
// company who spoke in it, found from speaker roles, names and hints. A call
// with two employees counts for both, and an employee a person marked in a call
// may read it.
package callsubject

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type NotificationSender interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Service struct {
	db            *sql.DB
	notifications NotificationSender
	log           logger.Logger
	// onChange re-projects the analytics facts of a call whose subjects changed.
	onChange func(ctx context.Context, callID uuid.UUID)
}

func NewService(db *sql.DB, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, log: log}
}

func (s *Service) SetNotificationService(sender NotificationSender) { s.notifications = sender }

// SetChangeHook runs after the subjects of a call changed and were committed.
func (s *Service) SetChangeHook(hook func(ctx context.Context, callID uuid.UUID)) { s.onChange = hook }

// Change is what one resolution did, for what has to happen after commit.
type Change struct {
	CallID      uuid.UUID
	Changed     bool
	Title       string
	OccurredAt  time.Time
	CompanyID   uuid.NullUUID
	Department  uuid.NullUUID
	Uploader    uuid.NullUUID
	Actor       uuid.NullUUID
	Granted     []uuid.UUID
	SelfRemoved bool
}

type callRow struct {
	Company    uuid.NullUUID
	Department uuid.NullUUID
	Uploader   uuid.NullUUID
	Title      string
	OccurredAt time.Time
}

// Refresh resolves the subjects of a call in its own transaction and does what
// follows a change. A failure is logged: who a call counts for must never stop
// the transcription, the edit or the analysis that asked.
func (s *Service) Refresh(ctx context.Context, callID uuid.UUID, actor uuid.NullUUID, cause string) {
	change, err := s.resolveInTx(ctx, callID, actor, cause)
	if err != nil {
		s.log.Warn(ctx, "call subjects not resolved", zap.String("call_id", callID.String()), zap.String("cause", cause), zap.Error(err))
		return
	}
	s.After(ctx, change)
}

func (s *Service) resolveInTx(ctx context.Context, callID uuid.UUID, actor uuid.NullUUID, cause string) (Change, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Change{}, fmt.Errorf("begin call subjects: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	change, err := s.Resolve(ctx, tx, callID, actor, cause)
	if err != nil {
		return Change{}, err
	}
	if err := tx.Commit(); err != nil {
		return Change{}, fmt.Errorf("commit call subjects: %w", err)
	}
	return change, nil
}

// ResolveTx is Resolve for callers that hold a *sql.Tx.
func (s *Service) ResolveTx(ctx context.Context, tx *sql.Tx, callID uuid.UUID, actor uuid.NullUUID, cause string) (Change, error) {
	return s.Resolve(ctx, tx, callID, actor, cause)
}

// Resolve recomputes the subjects inside the caller's transaction, so a change
// of speaker roles and the composition it implies commit together. A call whose
// subjects were set by hand is left alone.
func (s *Service) Resolve(ctx context.Context, q querier, callID uuid.UUID, actor uuid.NullUUID, cause string) (Change, error) {
	call, err := lockCall(ctx, q, callID)
	if err != nil {
		return Change{}, err
	}
	before, err := loadRows(ctx, q, callID)
	if err != nil {
		return Change{}, err
	}
	change := Change{CallID: callID, Title: call.Title, OccurredAt: call.OccurredAt, CompanyID: call.Company, Department: call.Department, Uploader: call.Uploader, Actor: actor}
	for _, row := range before {
		if row.Source == models.CallSubjectSourceManual {
			return change, nil
		}
	}
	input, err := loadInput(ctx, q, callID, call)
	if err != nil {
		return Change{}, err
	}
	result := decide(input)
	return s.write(ctx, q, change, before, result, cause)
}

// write stores a composition and records it when it changed.
func (s *Service) write(ctx context.Context, q querier, change Change, before []subjectRow, result decision, cause string) (Change, error) {
	callID := change.CallID
	var wasShared, wasInternal, hadState bool
	err := q.QueryRowContext(ctx, `SELECT is_shared, is_internal FROM call_subject_states WHERE call_uuid = $1`, callID).Scan(&wasShared, &wasInternal)
	switch {
	case err == nil:
		hadState = true
	case !errors.Is(err, sql.ErrNoRows):
		return Change{}, fmt.Errorf("read call subject state: %w", err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM call_subjects WHERE call_uuid = $1`, callID); err != nil {
		return Change{}, fmt.Errorf("clear call subjects: %w", err)
	}
	for _, row := range result.Subjects {
		if _, err := q.ExecContext(ctx, `
			INSERT INTO call_subjects (call_uuid, user_uuid, source, is_primary, speaker_key, talk_share, match_signals, grants_access, set_by_user_uuid)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			callID, row.UserID, row.Source, row.IsPrimary, row.SpeakerKey, row.TalkShare, nonNilStrings(row.Signals), row.GrantsAccess, row.SetBy); err != nil {
			return Change{}, fmt.Errorf("insert call subject: %w", err)
		}
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO call_subject_states (call_uuid, is_shared, is_internal, resolved_at) VALUES ($1,$2,$3,now())
		ON CONFLICT (call_uuid) DO UPDATE SET is_shared = EXCLUDED.is_shared, is_internal = EXCLUDED.is_internal, resolved_at = now()`,
		callID, result.Shared, result.Internal); err != nil {
		return Change{}, fmt.Errorf("store call subject state: %w", err)
	}
	if sameComposition(before, result.Subjects) {
		// The same people, but the call may have turned internal or shared: the
		// facts still need the new flags.
		change.Changed = !hadState || wasShared != result.Shared || wasInternal != result.Internal
		return change, nil
	}
	change.Changed = true
	if len(before) > 0 {
		beforeJSON, _ := json.Marshal(snapshot(before))
		afterJSON, _ := json.Marshal(snapshot(result.Subjects))
		if _, err := q.ExecContext(ctx, `
			INSERT INTO call_subject_events (event_uuid, call_uuid, actor_user_uuid, before, after, cause) VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,$6)`,
			uuid.New(), callID, change.Actor, beforeJSON, afterJSON, cause); err != nil {
			return Change{}, fmt.Errorf("record call subjects change: %w", err)
		}
	}
	hadAccess := map[uuid.UUID]bool{}
	wasSubject := map[uuid.UUID]bool{}
	for _, row := range before {
		wasSubject[row.UserID] = true
		hadAccess[row.UserID] = row.GrantsAccess
	}
	isSubject := map[uuid.UUID]bool{}
	for _, row := range result.Subjects {
		isSubject[row.UserID] = true
		marked := row.GrantsAccess && !hadAccess[row.UserID]
		self := (change.Uploader.Valid && change.Uploader.UUID == row.UserID) || (change.Actor.Valid && change.Actor.UUID == row.UserID)
		if marked && !self && change.CompanyID.Valid {
			change.Granted = append(change.Granted, row.UserID)
		}
	}
	change.SelfRemoved = change.Actor.Valid && wasSubject[change.Actor.UUID] && !isSubject[change.Actor.UUID]
	return change, nil
}

// After tells the people concerned and re-projects the analytics of the call.
func (s *Service) After(ctx context.Context, change Change) {
	if !change.Changed {
		return
	}
	if s.notifications != nil {
		entity := "call"
		for _, userID := range change.Granted {
			s.notify(ctx, models.CreateNotificationInput{
				UserUUID: userID, Type: models.NotificationTypeCallSubjectMarked, Title: "Вас отметили в звонке",
				Body:       fmt.Sprintf("«%s» от %s. Звонок доступен вам для просмотра", change.Title, change.OccurredAt.Format("02.01.2006")),
				EntityType: &entity, EntityUUID: uuid.NullUUID{UUID: change.CallID, Valid: true},
			})
		}
		if change.SelfRemoved {
			s.notifySelfRemoval(ctx, change)
		}
	}
	if s.onChange != nil {
		s.onChange(ctx, change.CallID)
	}
}

// notifySelfRemoval warns the leader of the call's department that an employee
// took themselves out of a call: the way a bad call would leave one's numbers.
func (s *Service) notifySelfRemoval(ctx context.Context, change Change) {
	actorName := "Сотрудник"
	_ = s.db.QueryRowContext(ctx, `SELECT btrim(full_name || ' ' || full_surname) FROM user_profiles WHERE user_uuid = $1`, change.Actor.UUID).Scan(&actorName)
	rows, err := s.db.QueryContext(ctx, `
		SELECT dm.user_uuid FROM department_members dm
		WHERE $1::uuid IS NOT NULL AND dm.department_uuid = $1 AND dm.role = 'department_leader' AND dm.status = 'active' AND dm.user_uuid <> $3
		UNION
		SELECT cm.user_uuid FROM company_members cm
		WHERE $1::uuid IS NULL AND cm.company_uuid = $2 AND cm.role IN ('company_manager','company_deputy') AND cm.status = 'active' AND cm.user_uuid <> $3`,
		change.Department, change.CompanyID, change.Actor.UUID)
	if err != nil {
		s.log.Warn(ctx, "call subject change recipients not read", zap.Error(err))
		return
	}
	var recipients []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			recipients = append(recipients, id)
		}
	}
	_ = rows.Close()
	entity := "call"
	for _, id := range recipients {
		s.notify(ctx, models.CreateNotificationInput{
			UserUUID: id, Type: models.NotificationTypeCallSubjectsChanged, Title: "Состав сотрудников звонка изменён",
			Body:       fmt.Sprintf("%s убрал себя из звонка «%s»", actorName, change.Title),
			EntityType: &entity, EntityUUID: uuid.NullUUID{UUID: change.CallID, Valid: true},
		})
	}
}

func (s *Service) notify(ctx context.Context, input models.CreateNotificationInput) {
	input.CreatedAt = time.Now().UTC()
	if _, err := s.notifications.Create(ctx, input); err != nil {
		s.log.Warn(ctx, "call subject notification failed", zap.String("type", string(input.Type)), zap.Error(err))
	}
}

// Get returns the subjects of a call for a caller that already checked access.
func (s *Service) Get(ctx context.Context, callID uuid.UUID) (models.CallSubjects, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cs.user_uuid, COALESCE(btrim(p.full_name || ' ' || p.full_surname), ''), cs.source, cs.is_primary, cs.speaker_key,
		       cs.talk_share::float8, to_json(cs.match_signals)::text, cs.grants_access, cs.set_by_user_uuid, cs.created_at
		FROM call_subjects cs LEFT JOIN user_profiles p ON p.user_uuid = cs.user_uuid
		WHERE cs.call_uuid = $1
		ORDER BY cs.is_primary DESC, cs.talk_share DESC NULLS LAST, cs.user_uuid`, callID)
	if err != nil {
		return models.CallSubjects{}, fmt.Errorf("list call subjects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := models.CallSubjects{Subjects: []models.CallSubject{}}
	for rows.Next() {
		var item models.CallSubject
		var signals stringArray
		if err := rows.Scan(&item.UserID, &item.FullName, &item.Source, &item.IsPrimary, &item.SpeakerKey, &item.TalkShare, &signals, &item.GrantsAccess, &item.SetBy, &item.CreatedAt); err != nil {
			return models.CallSubjects{}, fmt.Errorf("scan call subject: %w", err)
		}
		item.MatchSignals = []string(signals)
		result.Subjects = append(result.Subjects, item)
	}
	if err := rows.Err(); err != nil {
		return models.CallSubjects{}, err
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT is_shared FROM call_subject_states WHERE call_uuid = $1), false),
		       COALESCE((SELECT is_internal FROM call_subject_states WHERE call_uuid = $1), false),
		       EXISTS (SELECT 1 FROM call_subject_events WHERE call_uuid = $1 AND actor_user_uuid IS NOT NULL)`, callID).
		Scan(&result.IsShared, &result.IsInternal, &result.SubjectsChangedManually)
	if err != nil {
		return models.CallSubjects{}, fmt.Errorf("read call subject state: %w", err)
	}
	return result, nil
}

func lockCall(ctx context.Context, q querier, callID uuid.UUID) (callRow, error) {
	var call callRow
	err := q.QueryRowContext(ctx, `
		SELECT company_uuid, department_uuid, uploaded_by_user_uuid, title, COALESCE(occurred_at, created_at)
		FROM calls WHERE call_uuid = $1 FOR UPDATE`, callID).
		Scan(&call.Company, &call.Department, &call.Uploader, &call.Title, &call.OccurredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return callRow{}, models.ErrCallNotFound
	}
	if err != nil {
		return callRow{}, fmt.Errorf("lock call: %w", err)
	}
	return call, nil
}

func loadRows(ctx context.Context, q querier, callID uuid.UUID) ([]subjectRow, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT user_uuid, source, is_primary, speaker_key, talk_share::float8, to_json(match_signals)::text, grants_access, set_by_user_uuid
		FROM call_subjects WHERE call_uuid = $1 ORDER BY user_uuid`, callID)
	if err != nil {
		return nil, fmt.Errorf("read call subjects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []subjectRow
	for rows.Next() {
		var row subjectRow
		var signals stringArray
		if err := rows.Scan(&row.UserID, &row.Source, &row.IsPrimary, &row.SpeakerKey, &row.TalkShare, &signals, &row.GrantsAccess, &row.SetBy); err != nil {
			return nil, fmt.Errorf("scan call subject: %w", err)
		}
		row.Signals = []string(signals)
		result = append(result, row)
	}
	return result, rows.Err()
}

func loadInput(ctx context.Context, q querier, callID uuid.UUID, call callRow) (decideInput, error) {
	input := decideInput{Personal: !call.Company.Valid, Uploader: call.Uploader, Assignments: map[string]assignment{}, Members: map[uuid.UUID]member{}}
	if input.Personal {
		return input, nil
	}
	var segmentsRaw, wordsRaw []byte
	err := q.QueryRowContext(ctx, `SELECT COALESCE(segments, '[]'::jsonb), COALESCE(words, '[]'::jsonb) FROM call_transcriptions WHERE call_uuid = $1 AND status = 'transcribed'`, callID).Scan(&segmentsRaw, &wordsRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return decideInput{}, fmt.Errorf("read transcript speakers: %w", err)
	}
	var segments []models.TranscriptionSegment
	var transcriptWords []models.TranscriptionWord
	_ = json.Unmarshal(segmentsRaw, &segments)
	_ = json.Unmarshal(wordsRaw, &transcriptWords)
	input.Speakers = speakersOf(segments, transcriptWords)

	rows, err := q.QueryContext(ctx, `SELECT speaker_key, display_name, role, contact_user_uuid FROM call_transcription_speaker_assignments WHERE call_uuid = $1`, callID)
	if err != nil {
		return decideInput{}, fmt.Errorf("read speaker assignments: %w", err)
	}
	for rows.Next() {
		var key string
		var item assignment
		if err := rows.Scan(&key, &item.DisplayName, &item.Role, &item.Contact); err != nil {
			_ = rows.Close()
			return decideInput{}, fmt.Errorf("scan speaker assignment: %w", err)
		}
		input.Assignments[key] = item
	}
	_ = rows.Close()

	rows, err = q.QueryContext(ctx, `SELECT participant_name, user_uuid, role FROM call_speaker_hints WHERE call_uuid = $1 ORDER BY position`, callID)
	if err != nil {
		return decideInput{}, fmt.Errorf("read speaker hints: %w", err)
	}
	for rows.Next() {
		var item hint
		if err := rows.Scan(&item.Name, &item.UserID, &item.Role); err != nil {
			_ = rows.Close()
			return decideInput{}, fmt.Errorf("scan speaker hint: %w", err)
		}
		input.Hints = append(input.Hints, item)
	}
	_ = rows.Close()

	rows, err = q.QueryContext(ctx, `
		SELECT m.user_uuid, COALESCE(p.full_name, ''), COALESCE(p.full_surname, ''), COALESCE(p.username, '')
		FROM company_members m LEFT JOIN user_profiles p ON p.user_uuid = m.user_uuid
		WHERE m.company_uuid = $1 AND m.status = 'active'`, call.Company)
	if err != nil {
		return decideInput{}, fmt.Errorf("read company members: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.UserID, &m.FirstName, &m.LastName, &m.Username); err != nil {
			return decideInput{}, fmt.Errorf("scan company member: %w", err)
		}
		input.Members[m.UserID] = m
	}
	return input, rows.Err()
}

// openingSegments is how far into a speaker's turns an introduction is looked for.
const openingSegments = 3

func speakersOf(segments []models.TranscriptionSegment, transcriptWords []models.TranscriptionWord) []speaker {
	byKey := map[string]*speaker{}
	var order []string
	get := func(key string) *speaker {
		if s, ok := byKey[key]; ok {
			return s
		}
		byKey[key] = &speaker{Key: key}
		order = append(order, key)
		return byKey[key]
	}
	for _, w := range transcriptWords {
		if strings.TrimSpace(w.Speaker) != "" {
			get(w.Speaker).Words++
		}
	}
	seen := map[string]int{}
	for _, segment := range segments {
		key := strings.TrimSpace(segment.Speaker)
		if key == "" {
			continue
		}
		s := get(key)
		if len(transcriptWords) == 0 {
			s.Words += len(strings.Fields(segment.Text))
		}
		if seen[key] < openingSegments {
			s.Opening += " " + segment.Text
			seen[key]++
		}
	}
	result := make([]speaker, 0, len(order))
	sort.Strings(order)
	for _, key := range order {
		result = append(result, *byKey[key])
	}
	return result
}

func sameComposition(before, after []subjectRow) bool {
	if len(before) != len(after) {
		return false
	}
	index := map[uuid.UUID]subjectRow{}
	for _, row := range before {
		index[row.UserID] = row
	}
	for _, row := range after {
		old, ok := index[row.UserID]
		if !ok || old.IsPrimary != row.IsPrimary || old.GrantsAccess != row.GrantsAccess || old.Source != row.Source {
			return false
		}
	}
	return true
}

type snapshotRow struct {
	UserID       uuid.UUID `json:"user_uuid"`
	Source       string    `json:"source"`
	IsPrimary    bool      `json:"is_primary"`
	GrantsAccess bool      `json:"grants_access"`
}

func snapshot(rows []subjectRow) []snapshotRow {
	result := make([]snapshotRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, snapshotRow{UserID: row.UserID, Source: row.Source, IsPrimary: row.IsPrimary, GrantsAccess: row.GrantsAccess})
	}
	return result
}

// stringArray reads a TEXT[] selected as JSON.
type stringArray []string

func (a *stringArray) Scan(src any) error {
	var raw []byte
	switch value := src.(type) {
	case nil:
		*a = []string{}
		return nil
	case string:
		raw = []byte(value)
	case []byte:
		raw = value
	default:
		return fmt.Errorf("unexpected text array %T", src)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	if values == nil {
		values = []string{}
	}
	*a = values
	return nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
