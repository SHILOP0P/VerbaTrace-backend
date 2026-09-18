//go:build integration

package callsubject

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
	callRepo "verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type world struct {
	t       *testing.T
	db      *sql.DB
	calls   *callRepo.Repository
	service *Service
	owner   uuid.UUID
	olga    uuid.UUID
	anna    uuid.UUID
	client  uuid.UUID
	company uuid.UUID
	call    uuid.UUID
}

func newWorld(t *testing.T) *world {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	w := &world{t: t, db: db, calls: callRepo.NewRepository(db), service: NewService(db, nil)}
	w.owner = w.user("Павел", "Орлов")
	w.olga = w.user("Ольга", "Смирнова")
	w.anna = w.user("Анна", "Котова")
	w.client = w.user("Кирилл", "Клиентов")
	w.company = uuid.New()
	w.exec(`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Ромашка',$2,$3,now())`, w.company, "t"+w.company.String()[:8], w.owner)
	repositorytest.InsertCompanyMember(t, db, w.company, w.owner, "company_manager", "active")
	repositorytest.InsertCompanyMember(t, db, w.company, w.olga, "employee", "active")
	repositorytest.InsertCompanyMember(t, db, w.company, w.anna, "employee", "active")
	w.call = uuid.New()
	w.exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at)
		VALUES ($1, 'Первый звонок', 'transcribed', 'a.mp3', 'a.mp3', 'audio/mpeg', 1, $2, $3, 'company', now())`, w.call, w.owner, w.company)
	var words []models.TranscriptionWord
	for i := 0; i < 60; i++ {
		words = append(words, models.TranscriptionWord{Text: "слово", Speaker: "A"})
	}
	for i := 0; i < 40; i++ {
		words = append(words, models.TranscriptionWord{Text: "слово", Speaker: "B"})
	}
	raw, _ := json.Marshal(words)
	segments, _ := json.Marshal([]models.TranscriptionSegment{{Speaker: "A", Text: "Добрый день"}, {Speaker: "B", Text: "Здравствуйте"}})
	w.exec(`INSERT INTO call_transcriptions (transcription_uuid, call_uuid, status, text, provider, segments, words) VALUES ($1,$2,'transcribed','текст','mock',$3::jsonb,$4::jsonb)`, uuid.New(), w.call, segments, raw)
	return w
}

func (w *world) user(first, last string) uuid.UUID {
	id := repositorytest.CreateUser(w.t, w.db)
	w.exec(`UPDATE user_profiles SET full_name = $2, full_surname = $3 WHERE user_uuid = $1`, id, first, last)
	return id
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	_, err := w.db.Exec(query, args...)
	require.NoError(w.t, err)
}

func (w *world) assign(rows ...string) {
	w.t.Helper()
	w.exec(`DELETE FROM call_transcription_speaker_assignments WHERE call_uuid = $1`, w.call)
	for _, row := range rows {
		parts := strings.Split(row, "|")
		var contact any
		if parts[3] != "" {
			contact = parts[3]
		}
		w.exec(`INSERT INTO call_transcription_speaker_assignments (call_uuid, speaker_key, display_name, role, contact_user_uuid, updated_by_user_uuid) VALUES ($1,$2,$3,$4,$5,$6)`,
			w.call, parts[0], parts[1], parts[2], contact, w.owner)
	}
	w.service.Refresh(context.Background(), w.call, uuid.NullUUID{UUID: w.owner, Valid: true}, models.CallSubjectCauseSpeakerAssignments)
}

func (w *world) sees(user uuid.UUID) bool {
	_, err := w.calls.GetByUUID(context.Background(), w.call, user)
	if err != nil {
		require.ErrorIs(w.t, err, models.ErrCallNotFound)
	}
	return err == nil
}

func TestAMarkedEmployeeReadsTheCallAndNothingMore(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	w.service.Refresh(ctx, w.call, uuid.NullUUID{}, models.CallSubjectCauseTranscription)
	subjects, err := w.service.Get(ctx, w.call)
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 1)
	require.Equal(t, w.owner, subjects.Subjects[0].UserID, "nothing known about the speakers: the uploader")
	require.False(t, w.sees(w.olga))

	w.assign("A|Ольга Смирнова|manager|"+w.olga.String(), "B|Кирилл|client|"+w.client.String())
	subjects, err = w.service.Get(ctx, w.call)
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 1)
	require.Equal(t, w.olga, subjects.Subjects[0].UserID)
	require.True(t, subjects.Subjects[0].GrantsAccess)
	require.InDelta(t, 60.0, *subjects.Subjects[0].TalkShare, 0.01)
	require.True(t, subjects.SubjectsChangedManually, "the uploader changed who the call counts for")

	require.True(t, w.sees(w.olga), "marked: she reads the call")
	require.False(t, w.sees(w.client), "a client who is a platform user gets nothing")
	_, err = w.calls.GetEditableByUUID(ctx, w.call, w.olga)
	require.ErrorIs(t, err, models.ErrForbidden, "and cannot change it")
	_, err = w.calls.UpdateCallTitle(ctx, w.call, w.olga, "Мой звонок")
	require.ErrorIs(t, err, models.ErrForbidden)
	access, err := w.calls.GetAccess(ctx, w.call, w.olga)
	require.NoError(t, err)
	require.Equal(t, models.CallAccess{CanEdit: false, Via: models.CallAccessViaSubject}, access)
	access, err = w.calls.GetAccess(ctx, w.call, w.owner)
	require.NoError(t, err)
	require.Equal(t, models.CallAccess{CanEdit: true, Via: models.CallAccessViaUploader}, access)
	list, err := w.calls.List(ctx, w.olga)
	require.NoError(t, err)
	require.Len(t, list, 1, "the calls list uses the same predicate")

	// Leaving the company takes the call away at once.
	w.exec(`UPDATE company_members SET status = 'left' WHERE company_uuid = $1 AND user_uuid = $2`, w.company, w.olga)
	require.False(t, w.sees(w.olga))
	w.exec(`UPDATE company_members SET status = 'active' WHERE company_uuid = $1 AND user_uuid = $2`, w.company, w.olga)
	require.True(t, w.sees(w.olga))

	// A name read from the label counts for statistics and shares nothing.
	w.assign("A|Анна Котова|manager|", "B|Кирилл|client|")
	subjects, err = w.service.Get(ctx, w.call)
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 1)
	require.Equal(t, w.anna, subjects.Subjects[0].UserID)
	require.False(t, subjects.Subjects[0].GrantsAccess)
	require.False(t, w.sees(w.anna))
	require.False(t, w.sees(w.olga), "taking the mark off takes the call away")
}

func TestOnlyManagementSetsSubjectsByHandAndItHolds(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.service.Refresh(ctx, w.call, uuid.NullUUID{}, models.CallSubjectCauseTranscription)

	_, err := w.service.SetManual(ctx, models.SetCallSubjectsInput{CallID: w.call, ActorID: w.olga, UserIDs: []uuid.UUID{w.olga}})
	require.ErrorIs(t, err, models.ErrForbidden)
	_, err = w.service.SetManual(ctx, models.SetCallSubjectsInput{CallID: w.call, ActorID: w.owner, UserIDs: []uuid.UUID{w.client}})
	require.ErrorIs(t, err, models.ErrInvalidCallSubjects, "only employees of the call's company")

	subjects, err := w.service.SetManual(ctx, models.SetCallSubjectsInput{CallID: w.call, ActorID: w.owner, UserIDs: []uuid.UUID{w.anna, w.olga}, Primary: w.olga})
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 2)
	require.Equal(t, w.olga, subjects.Subjects[0].UserID, "primary first")
	require.True(t, subjects.IsShared)
	require.True(t, w.sees(w.anna))

	// Automatic resolution leaves a hand-made composition alone.
	w.assign("A|Ольга Смирнова|manager|"+w.olga.String(), "B|Кирилл|client|")
	subjects, err = w.service.Get(ctx, w.call)
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 2)

	// An empty list gives the call back to automatic resolution.
	subjects, err = w.service.SetManual(ctx, models.SetCallSubjectsInput{CallID: w.call, ActorID: w.owner})
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 1)
	require.Equal(t, w.olga, subjects.Subjects[0].UserID)
	require.False(t, w.sees(w.anna))

	var events int
	require.NoError(t, w.db.QueryRow(`SELECT count(*) FROM call_subject_events WHERE call_uuid = $1 AND cause = 'manual'`, w.call).Scan(&events))
	require.Equal(t, 2, events)
}
