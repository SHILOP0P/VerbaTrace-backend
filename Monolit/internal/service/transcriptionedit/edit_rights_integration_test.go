//go:build integration

package transcriptionedit

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"verbatrace/monolit/internal/models"
	callRepo "verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/repositorytest"
	transcriptionRepo "verbatrace/monolit/internal/repository/transcription"
	"verbatrace/monolit/internal/service/callsubject"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type rightsWorld struct {
	t        *testing.T
	db       *sql.DB
	service  *Service
	subjects *callsubject.Service
	owner    uuid.UUID
	olga     uuid.UUID
	stranger uuid.UUID
}

func newRightsWorld(t *testing.T) *rightsWorld {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	w := &rightsWorld{t: t, db: db, subjects: callsubject.NewService(db, nil)}
	w.service = NewService(db, callRepo.NewRepository(db), transcriptionRepo.NewRepository(db))
	w.service.SetSubjectResolver(w.subjects)
	w.owner = w.user("Павел", "Орлов")
	w.olga = w.user("Ольга", "Смирнова")
	w.stranger = w.user("Сергей", "Посторонний")
	return w
}

func (w *rightsWorld) user(first, last string) uuid.UUID {
	id := repositorytest.CreateUser(w.t, w.db)
	w.exec(`UPDATE user_profiles SET full_name = $2, full_surname = $3 WHERE user_uuid = $1`, id, first, last)
	return id
}

func (w *rightsWorld) exec(query string, args ...any) {
	w.t.Helper()
	_, err := w.db.Exec(query, args...)
	require.NoError(w.t, err)
}

// call inserts a transcribed call of two voices: A says 60 words, B says 40.
func (w *rightsWorld) call(company uuid.NullUUID) uuid.UUID {
	id := uuid.New()
	scope := "personal"
	if company.Valid {
		scope = "company"
	}
	w.exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at)
		VALUES ($1, 'Звонок', 'transcribed', 'a.mp3', 'a.mp3', 'audio/mpeg', 1, $2, $3, $4, now())`, id, w.owner, company, scope)
	var words []models.TranscriptionWord
	for i := 0; i < 100; i++ {
		speaker := "A"
		if i >= 60 {
			speaker = "B"
		}
		words = append(words, models.TranscriptionWord{Text: "слово", Speaker: speaker, StartSeconds: float64(i), EndSeconds: float64(i) + 0.5})
	}
	raw, _ := json.Marshal(words)
	segments, _ := json.Marshal([]models.TranscriptionSegment{{Speaker: "A", Text: "слово"}, {Speaker: "B", Text: "слово"}})
	w.exec(`INSERT INTO call_transcriptions (transcription_uuid, call_uuid, status, text, provider, segments, words) VALUES ($1,$2,'transcribed','слово','mock',$3::jsonb,$4::jsonb)`, uuid.New(), id, segments, raw)
	return id
}

func (w *rightsWorld) edit(callID, userID uuid.UUID) error {
	text := "исправлено"
	_, _, err := w.service.Update(context.Background(), UpdateInput{CallUUID: callID, UserUUID: userID, ExpectedRevision: 1, Edits: []WordEdit{{WordIndex: 0, Text: &text}}})
	return err
}

// Plan 2.3: marking an employee in a call lets them read it and nothing more;
// every change to its transcript stays with those who could change it before.
func TestAMarkedEmployeeCannotChangeTheTranscript(t *testing.T) {
	w := newRightsWorld(t)
	ctx := context.Background()
	company := uuid.New()
	w.exec(`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Ромашка',$2,$3,now())`, company, "t"+company.String()[:8], w.owner)
	repositorytest.InsertCompanyMember(t, w.db, company, w.owner, "company_manager", "active")
	repositorytest.InsertCompanyMember(t, w.db, company, w.olga, "employee", "active")
	callID := w.call(uuid.NullUUID{UUID: company, Valid: true})

	marked := []SpeakerAssignment{{SpeakerKey: "A", DisplayName: "Ольга", Role: "manager", ContactUserUUID: &w.olga}, {SpeakerKey: "B", DisplayName: "Клиент", Role: "client"}}
	_, err := w.service.ReplaceSpeakerAssignments(ctx, callID, w.stranger, marked)
	require.ErrorIs(t, err, models.ErrCallNotFound, "a stranger does not even see the call")
	_, err = w.service.ReplaceSpeakerAssignments(ctx, callID, w.owner, marked)
	require.NoError(t, err, "the uploader marks an employee")

	_, err = w.service.ListSpeakerAssignments(ctx, callID, w.olga)
	require.NoError(t, err, "the marked employee reads the speakers")
	_, err = w.service.ReplaceSpeakerAssignments(ctx, callID, w.olga, marked)
	require.ErrorIs(t, err, models.ErrForbidden, "but does not change them")
	require.ErrorIs(t, w.edit(callID, w.olga), models.ErrForbidden, "nor the words")
	_, _, err = w.service.Restore(ctx, callID, w.olga, 1, 1, "Вернуть версию")
	require.ErrorIs(t, err, models.ErrForbidden, "nor the chosen revision")
	require.ErrorIs(t, w.edit(callID, w.stranger), models.ErrCallNotFound)

	require.NoError(t, w.edit(callID, w.owner), "the uploader still edits")
	_, _, err = w.service.Restore(ctx, callID, w.owner, 2, 2, "Вернуть версию")
	require.NoError(t, err)
}

// The owner of a personal call says which voice is theirs; nobody else can be
// put on a speaker of it.
func TestThePersonalOwnerMarksTheirOwnSpeaker(t *testing.T) {
	w := newRightsWorld(t)
	ctx := context.Background()
	callID := w.call(uuid.NullUUID{})

	_, err := w.service.ReplaceSpeakerAssignments(ctx, callID, w.owner, []SpeakerAssignment{{SpeakerKey: "B", Role: "manager", ContactUserUUID: &w.stranger}})
	require.ErrorIs(t, err, ErrInvalidSpeakerAssignments, "not a contact and no company")

	_, err = w.service.ReplaceSpeakerAssignments(ctx, callID, w.owner, []SpeakerAssignment{{SpeakerKey: "A", Role: "client"}, {SpeakerKey: "B", Role: "manager", ContactUserUUID: &w.owner}})
	require.NoError(t, err)
	subjects, err := w.subjects.Get(ctx, callID)
	require.NoError(t, err)
	require.Len(t, subjects.Subjects, 1)
	require.Equal(t, w.owner, subjects.Subjects[0].UserID)
	require.NotNil(t, subjects.Subjects[0].SpeakerKey)
	require.Equal(t, "B", *subjects.Subjects[0].SpeakerKey)
	require.InDelta(t, 40.0, *subjects.Subjects[0].TalkShare, 0.01)
	require.False(t, subjects.IsShared)
	require.False(t, subjects.IsInternal)
	require.False(t, subjects.Subjects[0].GrantsAccess, "a personal call is shared with nobody")
}
