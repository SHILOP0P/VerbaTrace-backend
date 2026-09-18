//go:build integration

package teamanalytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type team struct {
	t                                         *testing.T
	db                                        *sql.DB
	service                                   *Service
	owner, leader, ivan, olga, petr, outsider uuid.UUID
	company, sales, support                   uuid.UUID
	instruction, scorecard, budget, nextStep  uuid.UUID
	now                                       time.Time
}

func (tm *team) exec(query string, args ...any) {
	tm.t.Helper()
	_, err := tm.db.Exec(query, args...)
	require.NoError(tm.t, err)
}

func (tm *team) user(first string) uuid.UUID {
	id := repositorytest.CreateUser(tm.t, tm.db)
	tm.exec(`UPDATE user_profiles SET full_name = $2, full_surname = 'Тестов', timezone = 'Europe/Moscow' WHERE user_uuid = $1`, id, first)
	return id
}

func newTeam(t *testing.T, plan string) *team {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	tm := &team{t: t, db: db, service: NewService(db, nil), now: time.Now().UTC()}
	tm.owner, tm.leader, tm.ivan, tm.olga, tm.petr, tm.outsider = tm.user("Павел"), tm.user("Лида"), tm.user("Иван"), tm.user("Ольга"), tm.user("Пётр"), tm.user("Чужой")
	tm.company, tm.sales, tm.support = uuid.New(), uuid.New(), uuid.New()
	tm.exec(`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at) VALUES ($1,'Ромашка',$2,$3,now())`, tm.company, "t"+tm.company.String()[:8], tm.owner)
	tm.exec(`INSERT INTO subscriptions (subscription_uuid, plan_uuid, type, user_uuid, status, starts_at) SELECT $1, plan_uuid, 'business', $2, 'active', now() - interval '1 day' FROM plans WHERE code = $3`, uuid.New(), tm.owner, plan)
	repositorytest.InsertCompanyMember(t, db, tm.company, tm.owner, "company_manager", "active")
	for _, id := range []uuid.UUID{tm.leader, tm.ivan, tm.olga, tm.petr} {
		repositorytest.InsertCompanyMember(t, db, tm.company, id, "employee", "active")
	}
	tm.exec(`INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1,$3,'Продажи'), ($2,$3,'Поддержка')`, tm.sales, tm.support, tm.company)
	repositorytest.InsertDepartmentMember(t, db, tm.sales, tm.leader, "department_leader", "active")
	repositorytest.InsertDepartmentMember(t, db, tm.sales, tm.ivan, "employee", "active")
	repositorytest.InsertDepartmentMember(t, db, tm.sales, tm.olga, "employee", "active")
	repositorytest.InsertDepartmentMember(t, db, tm.support, tm.petr, "employee", "active")

	tm.instruction, tm.budget, tm.nextStep = uuid.New(), uuid.New(), uuid.New()
	tm.exec(`INSERT INTO analysis_instructions (instruction_uuid, scope, company_uuid, title, original_filename, file_path, mime_type, size_bytes, content_sha256, sort_order, is_active, created_by_user_uuid, created_at, updated_at)
		VALUES ($1,'company',$2,'Стандарт продаж','s.md','p/s.md','text/markdown',1,'h',0,true,$3,now(),now())`, tm.instruction, tm.company, tm.owner)
	require.NoError(t, db.QueryRow(`SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_uuid = $1`, tm.instruction).Scan(&tm.scorecard))
	tm.exec(`UPDATE instruction_scorecards SET status = 'ready', is_current = true, compile_after = NULL WHERE scorecard_uuid = $1`, tm.scorecard)
	tm.exec(`INSERT INTO instruction_scorecard_criteria (scorecard_uuid, criterion_key, position, title, requirement) VALUES ($1,$2,1,'Выяснил бюджет','т'), ($1,$3,2,'Назвал следующий шаг','т')`, tm.scorecard, tm.budget, tm.nextStep)
	return tm
}

// call seeds an analysed call of the sales department for the given subjects.
func (tm *team) call(daysAgo int, department uuid.UUID, overall, budget int, subjects ...uuid.UUID) uuid.UUID {
	id, analysis := uuid.New(), uuid.New()
	at := tm.now.AddDate(0, 0, -daysAgo)
	tm.exec(`INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		VALUES ($1,'Звонок','analyzed','a.mp3','a.mp3','audio/mpeg',1,$2,$3,$4,'department',$5)`, id, subjects[0], tm.company, department, at)
	for i, s := range subjects {
		tm.exec(`INSERT INTO call_subjects (call_uuid, user_uuid, source, is_primary, grants_access) VALUES ($1,$2,'speaker_match',$3,true)`, id, s, i == 0)
	}
	tm.exec(`INSERT INTO analytics_call_facts (call_uuid, analysis_uuid, occurred_at, is_shared, schema_version, scorecard_mode, pipeline_version, ai_overall_score, criteria_score, coverage_status, criteria_total, criteria_scored)
		VALUES ($1,$2,$3,$4,3,'fixed','universal-staged-v7',$5,$6,'complete',2,2)`, id, analysis, at, len(subjects) > 1, overall, budget)
	tm.exec(`INSERT INTO analytics_criterion_facts (call_uuid, criterion_key, instruction_uuid, scorecard_uuid, item_id, ai_status, ai_score, weight, is_critical, evidence_start_seconds)
		VALUES ($1,$2,$4,$5,'r1',$6,$7,2,true,12.5), ($1,$3,$4,$5,'r2','met',100,1,false,NULL)`, id, tm.budget, tm.nextStep, tm.instruction, tm.scorecard, status(budget), budget)
	return id
}

func status(score int) string {
	switch {
	case score >= 100:
		return "met"
	case score >= 75:
		return "mostly_met"
	case score >= 50:
		return "partially_met"
	case score >= 25:
		return "minimally_met"
	}
	return "missed"
}

func (tm *team) req(user uuid.UUID) Request {
	return Request{UserID: user, CompanyID: uuid.NullUUID{UUID: tm.company, Valid: true}}
}

func TestEachRoleSeesWhatItShould(t *testing.T) {
	tm := newTeam(t, "business_plus")
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		tm.call(i+1, tm.sales, 40+i, 25, tm.ivan)
		tm.call(i+1, tm.sales, 80, 100, tm.olga)
		tm.call(i+1, tm.support, 90, 100, tm.petr)
	}
	tm.call(2, tm.sales, 60, 50, tm.ivan, tm.olga)

	summary, err := tm.service.Summary(ctx, tm.req(tm.owner))
	require.NoError(t, err)
	require.Equal(t, 19, summary.CallsAnalyzed)
	require.Equal(t, 1, summary.CallsShared)
	require.Equal(t, "thin", summary.Sample, "19 scores: shown with a note")
	require.NotNil(t, summary.AvgScore)

	criteria, err := tm.service.Criteria(ctx, tm.req(tm.owner), "", "")
	require.NoError(t, err)
	require.Len(t, criteria.Criteria, 2)
	require.Equal(t, "Выяснил бюджет", criteria.Criteria[0].Title, "weakest first")
	require.Equal(t, 1, criteria.Criteria[0].SortRank)
	require.Equal(t, 19, criteria.Criteria[0].NScored)
	require.Equal(t, "Стандарт продаж", criteria.Criteria[0].Instruction.Title)

	// The leader sees their department and the company to compare with.
	leaderSummary, err := tm.service.Summary(ctx, tm.req(tm.leader))
	require.NoError(t, err)
	require.Equal(t, 13, leaderSummary.CallsAnalyzed)
	require.NotNil(t, leaderSummary.CompanyAvgScore)
	employees, err := tm.service.Employees(ctx, tm.req(tm.leader))
	require.NoError(t, err)
	names := map[uuid.UUID]EmployeeRow{}
	for _, row := range employees.Employees {
		id, _ := uuid.Parse(row.UserUUID)
		names[id] = row
	}
	require.Contains(t, names, tm.ivan)
	require.Contains(t, names, tm.olga)
	require.NotContains(t, names, tm.petr, "another department stays out")
	require.Equal(t, 7, names[tm.ivan].Calls, "a shared call counts for both")
	require.NotNil(t, names[tm.ivan].WeakestCriterion)
	departments, err := tm.service.Departments(ctx, tm.req(tm.leader))
	require.NoError(t, err)
	require.Len(t, departments.Departments, 1)
	require.NotNil(t, departments.Company)
	_, err = tm.service.Profile(ctx, tm.req(tm.leader), tm.petr)
	require.ErrorIs(t, err, ErrForbidden)

	// An employee sees only their own numbers and one department figure.
	own, err := tm.service.Employees(ctx, tm.req(tm.ivan))
	require.NoError(t, err)
	require.Len(t, own.Employees, 1)
	require.True(t, own.Employees[0].IsMe)
	require.Nil(t, own.Team)
	_, err = tm.service.Departments(ctx, tm.req(tm.ivan))
	require.ErrorIs(t, err, ErrForbidden)
	_, err = tm.service.Profile(ctx, tm.req(tm.ivan), tm.olga)
	require.ErrorIs(t, err, ErrForbidden)
	profile, err := tm.service.Profile(ctx, tm.req(tm.ivan), uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, 7, profile.Totals.Calls)
	require.Equal(t, "Отдел «Продажи»", profile.Reference.Label)
	require.True(t, profile.Reference.Hidden, "two people with calls in the department could be told apart")
	require.NotEmpty(t, profile.WorthListening)
	require.True(t, profile.WorthListening[0].CanOpen)

	// A stranger to the company gets nothing.
	_, err = tm.service.Summary(ctx, tm.req(tm.outsider))
	require.ErrorIs(t, err, ErrForbidden)

	calls, err := tm.service.CriterionCalls(ctx, tm.req(tm.owner), tm.budget, "minimally_met", "score", 10, 0)
	require.NoError(t, err)
	require.Equal(t, 6, calls.Total)
	require.InDelta(t, 12.5, *calls.Calls[0].EvidenceStartSeconds, 0.001)
	require.Equal(t, "ai", calls.Calls[0].ScoreSource)
	require.NotEmpty(t, calls.Calls[0].Employees)

	instruction := tm.req(tm.owner)
	instruction.InstructionID = uuid.NullUUID{UUID: tm.instruction, Valid: true}
	matrix, err := tm.service.Matrix(ctx, instruction)
	require.NoError(t, err)
	require.Len(t, matrix.Criteria, 2)
	require.Len(t, matrix.Rows, 3)
}

func TestAPlanWithoutTeamAnalyticsKeepsOwnNumbersOnly(t *testing.T) {
	tm := newTeam(t, "business_start")
	ctx := context.Background()
	tm.call(1, tm.sales, 70, 50, tm.ivan)
	_, err := tm.service.Summary(ctx, tm.req(tm.owner))
	require.ErrorIs(t, err, ErrTeamAnalyticsDenied)
	own, err := tm.service.Profile(ctx, tm.req(tm.ivan), uuid.Nil)
	require.NoError(t, err, "an employee sees their own progress on any plan")
	require.Equal(t, 1, own.Totals.Calls)
	capabilities, err := tm.service.Capabilities(ctx, tm.req(tm.owner))
	require.NoError(t, err)
	require.False(t, capabilities.TeamAnalyticsEnabled)
	require.Equal(t, roleManager, capabilities.Role)
}

func TestSettingsAreForTheOwnerAndTheDeputy(t *testing.T) {
	tm := newTeam(t, "business_plus")
	ctx := context.Background()
	_, err := tm.service.GetSettings(ctx, tm.company, tm.ivan)
	require.ErrorIs(t, err, ErrForbidden)
	settings, err := tm.service.GetSettings(ctx, tm.company, tm.owner)
	require.NoError(t, err)
	require.Equal(t, 50, settings.CriticalAlertThreshold)
	threshold := 40
	settings, err = tm.service.UpdateSettings(ctx, tm.company, tm.owner, SettingsPatch{LockVersion: 0, CriticalAlertThreshold: &threshold})
	require.NoError(t, err)
	require.Equal(t, 40, settings.CriticalAlertThreshold)
	_, err = tm.service.UpdateSettings(ctx, tm.company, tm.owner, SettingsPatch{LockVersion: 0, CriticalAlertThreshold: &threshold})
	require.ErrorIs(t, err, ErrSettingsVersionConflict)
}
