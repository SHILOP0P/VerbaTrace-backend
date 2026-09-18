//go:build integration

package growth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type world struct {
	t                           *testing.T
	db                          *sql.DB
	service                     *Service
	owner, ivan, olga, outsider uuid.UUID
	company, sales              uuid.UUID
	now                         time.Time
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	_, err := w.db.Exec(query, args...)
	require.NoError(w.t, err)
}

func newWorld(t *testing.T) *world {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	w := &world{t: t, db: db, service: NewService(db, nil), now: time.Now().UTC()}
	w.owner, w.ivan, w.olga, w.outsider = repositorytest.CreateUser(t, db), repositorytest.CreateUser(t, db), repositorytest.CreateUser(t, db), repositorytest.CreateUser(t, db)
	w.company, w.sales = uuid.New(), uuid.New()
	w.exec(`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Ромашка',$2,$3,now())`, w.company, "g"+w.company.String()[:8], w.owner)
	repositorytest.InsertCompanyMember(t, db, w.company, w.owner, "company_manager", "active")
	repositorytest.InsertCompanyMember(t, db, w.company, w.ivan, "employee", "active")
	repositorytest.InsertCompanyMember(t, db, w.company, w.olga, "employee", "active")
	w.exec(`INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1,$2,'Продажи')`, w.sales, w.company)
	repositorytest.InsertDepartmentMember(t, db, w.sales, w.ivan, "employee", "active")
	return w
}

// call seeds a company call of one subject bound to speaker A.
func (w *world) call(daysAgo int, subject uuid.UUID) uuid.UUID {
	id := uuid.New()
	at := w.now.AddDate(0, 0, -daysAgo)
	w.exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		VALUES ($1,'Звонок','analyzed','a.mp3','a.mp3','audio/mpeg',1,$2,$3,$4,'department',$5)`, id, subject, w.company, w.sales, at)
	w.exec(`INSERT INTO call_subjects (call_uuid, user_uuid, source, is_primary, grants_access, speaker_key) VALUES ($1,$2,'speaker_match',true,true,'A')`, id, subject)
	w.exec(`INSERT INTO call_subject_states (call_uuid, is_shared, is_internal) VALUES ($1,false,false)`, id)
	return id
}

func (w *world) area(id uuid.UUID) (status string, occurrences, streak int, returned bool) {
	w.t.Helper()
	require.NoError(w.t, w.db.QueryRow(`SELECT status, occurrences, clean_streak, returned FROM growth_areas WHERE area_uuid = $1`, id).Scan(&status, &occurrences, &streak, &returned))
	return
}

func observe(area uuid.UUID, verdict string) models.GrowthOutcome {
	return models.GrowthOutcome{Observations: []models.GrowthObservation{{AreaID: area.String(), Verdict: verdict, ItemIDs: []string{"q1"}, Note: "{{speaker:A}} снова отвечает общими словами"}}}
}

func TestGrowthAreasFollowTheSeriesRules(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	first := w.call(10, w.ivan)
	growth, err := w.service.ContextFor(ctx, first)
	require.NoError(t, err)
	require.NotNil(t, growth)
	require.Equal(t, "A", growth.SubjectSpeaker)
	require.Empty(t, growth.OpenAreas)
	require.NoError(t, w.service.Record(ctx, first, models.GrowthOutcome{NewAreas: []models.NewGrowthArea{{Title: "Отвечает общими словами", Description: "Без примеров.", ItemIDs: []string{"q1"}}}}))

	growth, err = w.service.ContextFor(ctx, w.call(9, w.ivan))
	require.NoError(t, err)
	require.Len(t, growth.OpenAreas, 1)
	area := uuid.MustParse(growth.OpenAreas[0].ID)

	// A repeat counts; three calls where he coped resolve it.
	require.NoError(t, w.service.Record(ctx, w.call(8, w.ivan), observe(area, models.GrowthVerdictRepeated)))
	status, occurrences, streak, _ := w.area(area)
	require.Equal(t, "open", status)
	require.Equal(t, 2, occurrences)
	require.Zero(t, streak)
	skipped := w.call(7, w.ivan)
	require.NoError(t, w.service.Record(ctx, skipped, observe(area, models.GrowthVerdictNotApplicable)))
	for day := 6; day >= 4; day-- {
		require.NoError(t, w.service.Record(ctx, w.call(day, w.ivan), observe(area, models.GrowthVerdictImproved)))
	}
	status, _, streak, _ = w.area(area)
	require.Equal(t, "resolved", status)
	require.Equal(t, 3, streak)
	growth, err = w.service.ContextFor(ctx, w.call(3, w.ivan))
	require.NoError(t, err)
	require.Empty(t, growth.OpenAreas, "a resolved area is not asked about")

	// Seen again as new, it comes back as the same area, marked as returned.
	back := w.call(2, w.ivan)
	require.NoError(t, w.service.Record(ctx, back, models.GrowthOutcome{NewAreas: []models.NewGrowthArea{{Title: "отвечает  общими словами", Description: "Опять.", ItemIDs: []string{"q2"}}}}))
	status, occurrences, streak, returned := w.area(area)
	require.Equal(t, "open", status)
	require.True(t, returned)
	require.Equal(t, 3, occurrences)
	require.Zero(t, streak)
	var areas int
	require.NoError(t, w.db.QueryRow(`SELECT count(*) FROM growth_areas`).Scan(&areas))
	require.Equal(t, 1, areas)

	// Analysing the same call again replaces what it said.
	require.NoError(t, w.service.Record(ctx, back, models.GrowthOutcome{}))
	status, occurrences, _, returned = w.area(area)
	require.Equal(t, "resolved", status)
	require.False(t, returned)
	require.Equal(t, 2, occurrences)

	// The call changes hands: its observation goes, counters follow.
	require.NoError(t, w.service.Record(ctx, back, observe(area, models.GrowthVerdictRepeated)))
	w.exec(`UPDATE call_subjects SET user_uuid = $2 WHERE call_uuid = $1`, back, w.olga)
	w.service.Reconcile(ctx, back)
	status, occurrences, _, _ = w.area(area)
	require.Equal(t, "resolved", status)
	require.Equal(t, 2, occurrences)
}

func TestGrowthAreasAreKeptOnlyForOneEmployeesCalls(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	shared := w.call(1, w.ivan)
	w.exec(`UPDATE call_subject_states SET is_shared = true WHERE call_uuid = $1`, shared)
	growth, err := w.service.ContextFor(ctx, shared)
	require.NoError(t, err)
	require.Nil(t, growth, "a shared call keeps no growth areas")

	unbound := w.call(1, w.ivan)
	w.exec(`UPDATE call_subjects SET speaker_key = NULL WHERE call_uuid = $1`, unbound)
	growth, err = w.service.ContextFor(ctx, unbound)
	require.NoError(t, err)
	require.Nil(t, growth, "the model must know which speaker is the employee")

	w.exec(`INSERT INTO company_analytics_settings (company_uuid, growth_areas_enabled) VALUES ($1, false)`, w.company)
	growth, err = w.service.ContextFor(ctx, w.call(1, w.ivan))
	require.NoError(t, err)
	require.Nil(t, growth, "the company switched growth areas off")
}

func TestHidingAGrowthAreaNeedsAReasonAndTheRight(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	call := w.call(1, w.ivan)
	require.NoError(t, w.service.Record(ctx, call, models.GrowthOutcome{NewAreas: []models.NewGrowthArea{{Title: "Перебивает клиента", Description: "Не даёт договорить.", ItemIDs: []string{"q1"}}}}))
	var area uuid.UUID
	require.NoError(t, w.db.QueryRow(`SELECT area_uuid FROM growth_areas`).Scan(&area))

	require.ErrorIs(t, w.service.Dismiss(ctx, w.ivan, area, "  "), models.ErrInvalidGrowthAreaReason)
	require.ErrorIs(t, w.service.Dismiss(ctx, w.olga, area, "не согласна"), models.ErrGrowthAreaNotFound, "a colleague may not")
	require.ErrorIs(t, w.service.Dismiss(ctx, w.outsider, area, "чужой"), models.ErrGrowthAreaNotFound)
	require.NoError(t, w.service.Dismiss(ctx, w.ivan, area, "Это был не я, а коллега"))
	status, _, _, _ := w.area(area)
	require.Equal(t, "dismissed", status)
	require.NoError(t, w.service.Record(ctx, call, models.GrowthOutcome{Observations: []models.GrowthObservation{{AreaID: area.String(), Verdict: models.GrowthVerdictRepeated, ItemIDs: []string{"q1"}}}}))
	status, _, _, _ = w.area(area)
	require.Equal(t, "dismissed", status, "a hidden area stays hidden")

	require.NoError(t, w.service.Reopen(ctx, w.owner, area))
	status, _, _, _ = w.area(area)
	require.Equal(t, "open", status)
	require.ErrorIs(t, w.service.Reopen(ctx, w.owner, area), models.ErrGrowthAreaNotDismissed)
	var events int
	require.NoError(t, w.db.QueryRow(`SELECT count(*) FROM growth_area_events WHERE area_uuid = $1`, area).Scan(&events))
	require.Equal(t, 2, events)
}
