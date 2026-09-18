//go:build integration

package analyticsfacts

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/models"
	callRepo "verbatrace/monolit/internal/repository/call"
	companyRepo "verbatrace/monolit/internal/repository/company"
	"verbatrace/monolit/internal/repository/repositorytest"
	"verbatrace/monolit/internal/service/callsubject"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The backfill fills whom a call counts for, projects the facts, and projects
// them again when the analysis is newer than its facts; a call in the bin is
// left alone.
func TestWorkerCatchesUpSubjectsAndNewerAnalyses(t *testing.T) {
	f := seed(t)
	ctx := context.Background()
	worker := NewWorker(NewService(f.db, nil), callsubject.NewService(f.db, nil), 0, 0)
	binned := uuid.New()
	exec(t, f.db, `INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at, deleted_at, purge_after)
		VALUES ($1,'В корзине','analyzed','b.mp3','b.mp3','audio/mpeg',1,$2,$3,'company',now(),now(),now() + interval '30 days')`, binned, f.owner, f.company)

	require.Equal(t, 2, worker.RunOnce(ctx), "the call's employees and its facts")
	var subject uuid.UUID
	require.NoError(t, f.db.QueryRow(`SELECT user_uuid FROM call_subjects WHERE call_uuid = $1 AND is_primary`, f.call).Scan(&subject))
	require.Equal(t, f.owner, subject, "nothing known about the speakers: the uploader")
	var binnedStates int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM call_subject_states WHERE call_uuid = $1`, binned).Scan(&binnedStates))
	require.Zero(t, binnedStates, "a call in the bin is not resolved")
	require.Zero(t, worker.RunOnce(ctx), "nothing left to catch up")

	// The analysis was re-run after its facts were written.
	exec(t, f.db, `UPDATE analytics_call_facts SET projected_at = projected_at - interval '1 hour' WHERE call_uuid = $1`, f.call)
	exec(t, f.db, `UPDATE call_analyses SET updated_at = now() - interval '1 minute' WHERE analysis_uuid = $1`, f.analysis)
	require.Equal(t, 1, worker.RunOnce(ctx), "an analysis newer than its facts is projected again")
	require.Zero(t, worker.RunOnce(ctx))
}

// Moving calls to another company of the same owner drops whom they counted
// for: that was decided among the old company's employees. The worker decides
// it again among the new company's, and the event says why.
func TestMovedCallsAreResolvedAmongTheNewCompany(t *testing.T) {
	f := seed(t)
	ctx := context.Background()
	subjects := callsubject.NewService(f.db, nil)
	worker := NewWorker(NewService(f.db, nil), subjects, 0, 0)
	calls := callRepo.NewRepository(f.db)

	olga, anna := repositorytest.CreateUser(t, f.db), repositorytest.CreateUser(t, f.db)
	exec(t, f.db, `UPDATE user_profiles SET full_name = 'Ольга', full_surname = 'Смирнова' WHERE user_uuid = $1`, olga)
	exec(t, f.db, `UPDATE user_profiles SET full_name = 'Анна', full_surname = 'Котова' WHERE user_uuid = $1`, anna)
	target := uuid.New()
	exec(t, f.db, `INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Лютик',$2,$3,now())`, target, "t"+target.String()[:8], f.owner)
	repositorytest.InsertCompanyMember(t, f.db, target, f.owner, "company_manager", "active")
	repositorytest.InsertCompanyMember(t, f.db, f.company, olga, "employee", "active")
	repositorytest.InsertCompanyMember(t, f.db, f.company, anna, "employee", "active")
	repositorytest.InsertCompanyMember(t, f.db, target, anna, "employee", "active")

	// Olga works only in the old company, Anna in both.
	annaCall := uuid.New()
	exec(t, f.db, `INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at)
		VALUES ($1,'Звонок Анны','analyzed','c.mp3','c.mp3','audio/mpeg',1,$2,$3,'company',now())`, annaCall, f.owner, f.company)
	for call, employee := range map[uuid.UUID]uuid.UUID{f.call: olga, annaCall: anna} {
		exec(t, f.db, `INSERT INTO call_transcription_speaker_assignments (call_uuid, speaker_key, display_name, role, contact_user_uuid, updated_by_user_uuid) VALUES ($1,'A','Сотрудник','manager',$2,$3)`, call, employee, f.owner)
		exec(t, f.db, `INSERT INTO call_transcriptions (transcription_uuid, call_uuid, status, text, provider, segments, words)
			VALUES ($1,$2,'transcribed','слово','mock','[{"speaker":"A","text":"слово"}]'::jsonb,'[{"text":"слово","speaker":"A","start_seconds":0,"end_seconds":1}]'::jsonb)`, uuid.New(), call)
		subjects.Refresh(ctx, call, uuid.NullUUID{UUID: f.owner, Valid: true}, models.CallSubjectCauseSpeakerAssignments)
	}
	primary := func(call uuid.UUID) uuid.UUID {
		var id uuid.UUID
		require.NoError(t, f.db.QueryRow(`SELECT user_uuid FROM call_subjects WHERE call_uuid = $1 AND is_primary`, call).Scan(&id))
		return id
	}
	require.Equal(t, olga, primary(f.call))
	require.Equal(t, anna, primary(annaCall))
	_, err := calls.GetByUUID(ctx, f.call, olga)
	require.NoError(t, err, "marked in the call: Olga reads it")

	_, err = companyRepo.NewRepository(f.db).TransferCompanyData(ctx, models.TransferCompanyDataInput{
		OwnerUserUUID: f.owner, SourceCompanyUUID: f.company, TargetCompanyUUID: target, IncludeCalls: true, Reason: "Объединение компаний",
	})
	require.NoError(t, err)
	var left int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM call_subjects WHERE call_uuid IN ($1, $2)`, f.call, annaCall).Scan(&left))
	require.Zero(t, left, "who the calls counted for was the old company's answer")

	worker.RunOnce(ctx)
	require.Equal(t, f.owner, primary(f.call), "Olga is not in the new company: the uploader")
	require.Equal(t, anna, primary(annaCall), "Anna works in both companies and stays")
	_, err = calls.GetByUUID(ctx, f.call, olga)
	require.ErrorIs(t, err, models.ErrCallNotFound, "and Olga no longer reads the moved call")
	var causes int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM call_subject_events WHERE call_uuid IN ($1, $2) AND cause = 'company_transfer'`, f.call, annaCall).Scan(&causes))
	require.Equal(t, 2, causes)
}
