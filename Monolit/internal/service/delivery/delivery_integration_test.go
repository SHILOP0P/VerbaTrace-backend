//go:build integration

package delivery

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"
	"verbatrace/monolit/internal/service/teamanalytics"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type world struct {
	t                          *testing.T
	db                         *sql.DB
	service                    *Service
	owner, leader, ivan, olga  uuid.UUID
	company, sales, scorecard  uuid.UUID
	instruction, budget, steps uuid.UUID
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	_, err := w.db.Exec(query, args...)
	require.NoError(w.t, err)
}

func (w *world) user(name string) uuid.UUID {
	id := repositorytest.CreateUser(w.t, w.db)
	w.exec(`UPDATE user_profiles SET full_name = $2, full_surname = 'Тестов', timezone = 'Europe/Moscow' WHERE user_uuid = $1`, id, name)
	return id
}

func newWorld(t *testing.T) *world {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	w := &world{t: t, db: db, service: NewService(db, nil, "https://app.example")}
	w.owner, w.leader, w.ivan, w.olga = w.user("Павел"), w.user("Лида"), w.user("Иван"), w.user("Ольга")
	w.company, w.sales = uuid.New(), uuid.New()
	w.exec(`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Ромашка',$2,$3,now())`, w.company, "d"+w.company.String()[:8], w.owner)
	w.exec(`INSERT INTO subscriptions (subscription_uuid, plan_uuid, type, user_uuid, status, starts_at) SELECT $1, plan_uuid, 'business', $2, 'active', now() - interval '30 days' FROM plans WHERE code = 'business_plus'`, uuid.New(), w.owner)
	repositorytest.InsertCompanyMember(t, db, w.company, w.owner, "company_manager", "active")
	for _, id := range []uuid.UUID{w.leader, w.ivan, w.olga} {
		repositorytest.InsertCompanyMember(t, db, w.company, id, "employee", "active")
	}
	w.exec(`INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1,$2,'Продажи')`, w.sales, w.company)
	repositorytest.InsertDepartmentMember(t, db, w.sales, w.leader, "department_leader", "active")
	repositorytest.InsertDepartmentMember(t, db, w.sales, w.ivan, "employee", "active")
	w.instruction, w.budget, w.steps = uuid.New(), uuid.New(), uuid.New()
	w.exec(`INSERT INTO analysis_instructions (instruction_uuid, scope, company_uuid, title, original_filename, file_path, mime_type, size_bytes, content_sha256, sort_order, is_active, created_by_user_uuid, created_at, updated_at)
		VALUES ($1,'company',$2,'Стандарт продаж','s.md','p/s.md','text/markdown',1,'h',0,true,$3,now(),now())`, w.instruction, w.company, w.owner)
	require.NoError(t, db.QueryRow(`SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_uuid = $1`, w.instruction).Scan(&w.scorecard))
	w.exec(`UPDATE instruction_scorecards SET status = 'ready', is_current = true, compile_after = NULL WHERE scorecard_uuid = $1`, w.scorecard)
	w.exec(`INSERT INTO instruction_scorecard_criteria (scorecard_uuid, criterion_key, position, title, requirement, is_critical) VALUES ($1,$2,1,'Выяснил бюджет','т',true), ($1,$3,2,'Назвал следующий шаг','т',false)`, w.scorecard, w.budget, w.steps)
	return w
}

// call seeds an analysed sales call of Ivan: overall may be nil, budget is the
// critical criterion's score.
func (w *world) call(at time.Time, overall *int, budget int, internal bool) uuid.UUID {
	id := uuid.New()
	w.exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		VALUES ($1,'Звонок клиенту','analyzed','a.mp3','a.mp3','audio/mpeg',1,$2,$3,$4,'department',$5)`, id, w.ivan, w.company, w.sales, at)
	w.exec(`INSERT INTO call_subjects (call_uuid, user_uuid, source, is_primary, grants_access, speaker_key) VALUES ($1,$2,'speaker_match',true,true,'A')`, id, w.ivan)
	missed := 0
	if budget < criticalMissBelow {
		missed = 1
	}
	w.exec(`INSERT INTO analytics_call_facts (call_uuid, analysis_uuid, occurred_at, is_internal, schema_version, scorecard_mode, ai_overall_score, coverage_status, criteria_total, criteria_scored, critical_missed)
		VALUES ($1,$2,$3,$4,3,'fixed',$5,'complete',2,2,$6)`, id, uuid.New(), at, internal, overall, missed)
	w.exec(`INSERT INTO analytics_criterion_facts (call_uuid, criterion_key, instruction_uuid, scorecard_uuid, item_id, ai_status, ai_score, weight, is_critical)
		VALUES ($1,$2,$4,$5,'r1','met',$6,2,true), ($1,$3,$4,$5,'r2','met',100,1,false)`, id, w.budget, w.steps, w.instruction, w.scorecard, budget)
	return id
}

func (w *world) notifications(user uuid.UUID, kind models.NotificationType) []string {
	w.t.Helper()
	rows, err := w.db.Query(`SELECT body FROM notifications WHERE user_uuid = $1 AND type = $2 ORDER BY created_at`, user, string(kind))
	require.NoError(w.t, err)
	defer func() { _ = rows.Close() }()
	var bodies []string
	for rows.Next() {
		var body string
		require.NoError(w.t, rows.Scan(&body))
		bodies = append(bodies, body)
	}
	return bodies
}

func score(v int) *int { return &v }

func TestAFailedCallAlertsOnceTheOneWhoAnswersForIt(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	now := time.Now().UTC()

	fine := w.call(now, score(80), 100, false)
	w.service.CallAnalyzed(ctx, fine)
	unscored := w.call(now, nil, 100, false)
	w.service.CallAnalyzed(ctx, unscored)
	internal := w.call(now, score(20), 0, true)
	w.service.CallAnalyzed(ctx, internal)
	require.Empty(t, w.notifications(w.leader, models.NotificationTypeCriticalCallAlert), "a fine call, a call without a score and an internal call raise nothing")

	weak := w.call(now, score(40), 100, false)
	w.service.CallAnalyzed(ctx, weak)
	w.service.CallAnalyzed(ctx, weak)
	alerts := w.notifications(w.leader, models.NotificationTypeCriticalCallAlert)
	require.Len(t, alerts, 1, "one alert per call, a second analysis does not repeat it")
	require.Equal(t, "Иван Тестов: «Звонок клиенту», оценка 40. Оценка ниже порога 50", alerts[0])
	require.Len(t, w.notifications(w.ivan, models.NotificationTypeCriticalCallAlert), 1, "the employee learns about their own call")
	require.Empty(t, w.notifications(w.owner, models.NotificationTypeCriticalCallAlert), "the department has a leader")

	missed := w.call(now, score(90), 0, false)
	w.service.CallAnalyzed(ctx, missed)
	alerts = w.notifications(w.leader, models.NotificationTypeCriticalCallAlert)
	require.Len(t, alerts, 2)
	require.Contains(t, alerts[1], "Пропущен критичный критерий: Выяснил бюджет")

	// Without a leader the deputy answers, without a deputy the owner; a frozen
	// company sends nothing.
	w.exec(`UPDATE department_members SET status = 'left' WHERE user_uuid = $1`, w.leader)
	w.service.CallAnalyzed(ctx, w.call(now, score(10), 100, false))
	require.Len(t, w.notifications(w.owner, models.NotificationTypeCriticalCallAlert), 1)
	w.exec(`UPDATE companies SET lifecycle_state = 'frozen' WHERE company_uuid = $1`, w.company)
	w.service.CallAnalyzed(ctx, w.call(now, score(10), 100, false))
	require.Len(t, w.notifications(w.owner, models.NotificationTypeCriticalCallAlert), 1)
}

type failingSender struct{ calls int }

func (f *failingSender) Name() string { return "failing" }
func (f *failingSender) Send(context.Context, Message) error {
	f.calls++
	return errors.New("smtp is down")
}

func TestTheQueueSendsOnceRetriesAndGivesUp(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	// Mail is on and confirmed only here: no channel can be switched on in the app yet.
	w.exec(`INSERT INTO notification_channels (user_uuid, channel, address, status, verified_at) VALUES ($1,'email','ivan@example.com','verified',now())`, w.ivan)
	w.exec(`INSERT INTO notification_subscriptions (user_uuid, kind, channel, enabled) VALUES ($1,'critical_call_alert','email',true)`, w.ivan)
	event := Event{Kind: KindCriticalCallAlert, User: w.ivan, Notification: models.NotificationTypeCriticalCallAlert, Title: "Звонок требует внимания", Body: "тест", Text: "тест"}

	delivered, err := w.service.Deliver(ctx, "alert:test", []Event{event})
	require.NoError(t, err)
	require.True(t, delivered)
	delivered, err = w.service.Deliver(ctx, "alert:test", []Event{event})
	require.NoError(t, err)
	require.False(t, delivered, "the same mark delivers nothing")
	queued, err := Enqueue(ctx, w.db, Outbound{User: w.ivan, Channel: ChannelEmail, Kind: KindCriticalCallAlert, DedupeKey: "alert:test:" + w.ivan.String() + ":email", Payload: map[string]any{}})
	require.NoError(t, err)
	require.False(t, queued, "a repeated dedupe key makes no second message")
	require.Len(t, w.notifications(w.ivan, models.NotificationTypeCriticalCallAlert), 1)

	failing := &failingSender{}
	worker := NewWorker(w.db, failing, nil, time.Second, 10)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		require.Equal(t, 1, worker.RunOnce(ctx))
		w.exec(`UPDATE outbound_messages SET available_at = now() - interval '1 second'`)
	}
	var status string
	var attempts int
	require.NoError(t, w.db.QueryRow(`SELECT status, attempts FROM outbound_messages`).Scan(&status, &attempts))
	require.Equal(t, "dead", status)
	require.Equal(t, maxAttempts, attempts)
	require.Zero(t, worker.RunOnce(ctx), "a dead message is not retried")

	_, err = Enqueue(ctx, w.db, Outbound{User: w.ivan, Channel: ChannelEmail, Kind: KindWeeklyDigest, DedupeKey: "digest:test", Payload: map[string]any{"title": "Итоги"}})
	require.NoError(t, err)
	require.Equal(t, 1, NewWorker(w.db, NewMockSender(nil), nil, time.Second, 10).RunOnce(ctx))
	require.NoError(t, w.db.QueryRow(`SELECT status FROM outbound_messages WHERE dedupe_key = 'digest:test'`).Scan(&status))
	require.Equal(t, "sent", status)
}

func TestSubscriptionsKeepOnlyTheAppSwitchable(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	items, err := w.service.Subscriptions(ctx, w.ivan)
	require.NoError(t, err)
	require.Len(t, items, 6)
	for _, item := range items {
		require.Equal(t, item.Channel == ChannelInApp, item.Enabled)
		require.Equal(t, item.Channel == ChannelInApp, item.Available)
	}
	_, err = w.service.SetSubscriptions(ctx, w.ivan, []Subscription{{Kind: KindWeeklyDigest, Channel: ChannelEmail, Enabled: true}})
	require.ErrorIs(t, err, ErrChannelUnavailable)
	items, err = w.service.SetSubscriptions(ctx, w.ivan, []Subscription{{Kind: KindCriticalCallAlert, Channel: ChannelInApp, Enabled: false}})
	require.NoError(t, err)
	for _, item := range items {
		if item.Kind == KindCriticalCallAlert && item.Channel == ChannelInApp {
			require.False(t, item.Enabled)
		}
	}
	w.service.CallAnalyzed(ctx, w.call(time.Now().UTC(), score(10), 100, false))
	require.Empty(t, w.notifications(w.ivan, models.NotificationTypeCriticalCallAlert), "switched off in the app")
	require.Len(t, w.notifications(w.leader, models.NotificationTypeCriticalCallAlert), 1)
}

func TestWeeklyDigestComesOnMondayMorningOncePerWeek(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	moscow, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	monday := time.Date(2026, 9, 14, 9, 20, 0, 0, moscow)
	for day := 1; day <= 5; day++ {
		w.call(monday.AddDate(0, 0, -day), score(60+day), 100, false)
	}
	digests := NewDigests(w.service, teamanalytics.NewService(w.db, nil))

	require.Zero(t, digests.RunOnce(ctx, monday.Add(-2*time.Hour)), "too early")
	sent := digests.RunOnce(ctx, monday)
	require.Positive(t, sent)
	owner := w.notifications(w.owner, models.NotificationTypeWeeklyDigestReady)
	require.Len(t, owner, 1)
	require.Contains(t, owner[0], "5 звонков, средний балл 63")
	ivan := w.notifications(w.ivan, models.NotificationTypeWeeklyDigestReady)
	require.Len(t, ivan, 1, "the employee gets their own digest")
	require.Empty(t, w.notifications(w.olga, models.NotificationTypeWeeklyDigestReady), "an empty week sends nothing")
	require.Zero(t, digests.RunOnce(ctx, monday.Add(30*time.Minute)), "once per week")
}
