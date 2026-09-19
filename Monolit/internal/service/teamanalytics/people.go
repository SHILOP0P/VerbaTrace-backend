package teamanalytics

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

type DepartmentRef struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type WeakestCriterion struct {
	CriterionKey string `json:"criterion_key"`
	Title        string `json:"title"`
	AvgScore     int    `json:"avg_score"`
}

type TeamRow struct {
	Calls            int          `json:"calls"`
	AvgScore         *int         `json:"avg_score"`
	AvgCriteriaScore *int         `json:"avg_criteria_score"`
	Delta            Delta        `json:"delta"`
	Sample           string       `json:"sample"`
	Trend            []TrendPoint `json:"trend"`
	// Speech is the team's median, what each employee's numbers are read
	// against.
	Speech *Speech `json:"speech"`
}

type EmployeeRow struct {
	UserUUID         string            `json:"user_uuid"`
	FullName         string            `json:"full_name"`
	AvatarURL        *string           `json:"avatar_url"`
	Department       *DepartmentRef    `json:"department"`
	IsFormerMember   bool              `json:"is_former_member"`
	IsMe             bool              `json:"is_me"`
	Calls            int               `json:"calls"`
	CallsShared      int               `json:"calls_shared"`
	AvgScore         *int              `json:"avg_score"`
	AvgCriteriaScore *int              `json:"avg_criteria_score"`
	Delta            Delta             `json:"delta"`
	Sample           string            `json:"sample"`
	CriticalMissed   int               `json:"critical_missed"`
	WeakestCriterion *WeakestCriterion `json:"weakest_criterion"`
	Speech           any               `json:"speech"`
	Trend            []TrendPoint      `json:"trend"`
}

type EmployeesView struct {
	Period    PeriodView    `json:"period"`
	Team      *TeamRow      `json:"team"`
	Employees []EmployeeRow `json:"employees"`
	Total     int           `json:"total"`
}

type DepartmentRow struct {
	DepartmentUUID   string            `json:"department_uuid"`
	Name             string            `json:"name"`
	Employees        int               `json:"employees"`
	Calls            int               `json:"calls"`
	AvgScore         *int              `json:"avg_score"`
	AvgCriteriaScore *int              `json:"avg_criteria_score"`
	Delta            Delta             `json:"delta"`
	Sample           string            `json:"sample"`
	CriticalMissed   int               `json:"critical_missed"`
	WeakestCriterion *WeakestCriterion `json:"weakest_criterion"`
	Trend            []TrendPoint      `json:"trend"`
}

type DepartmentsView struct {
	Period      PeriodView      `json:"period"`
	Company     *TeamRow        `json:"company"`
	Departments []DepartmentRow `json:"departments"`
	Total       int             `json:"total"`
}

// groupTotals is a call-level aggregate per group: an employee or a department.
type groupTotals struct {
	calls, shared, critical int
	overall, criteria       stat
}

// groupStats aggregates calls per group. groupBy is the grouping column and
// join the table that provides it.
func (s *Service) groupStats(ctx context.Context, scope Scope, from, to time.Time, groupBy, join string, narrow func(q *query)) (map[uuid.UUID]*groupTotals, error) {
	q := &query{}
	scope.callsOf(q, from, to)
	if narrow != nil {
		narrow(q)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s, count(DISTINCT f.call_uuid), count(DISTINCT f.call_uuid) FILTER (WHERE f.is_shared),
		       COALESCE(sum(f.critical_missed), 0),
		       avg(f.overall_score)::float8, COALESCE(var_samp(f.overall_score), 0)::float8, count(f.overall_score),
		       avg(f.criteria_score)::float8, count(f.criteria_score)
		FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid %s
		WHERE %s AND %s IS NOT NULL
		GROUP BY 1`, groupBy, join, q.sql(), groupBy), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read group stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := map[uuid.UUID]*groupTotals{}
	for rows.Next() {
		var id uuid.UUID
		var g groupTotals
		var mean, criteriaMean sql.NullFloat64
		if err := rows.Scan(&id, &g.calls, &g.shared, &g.critical, &mean, &g.overall.variance, &g.overall.n, &criteriaMean, &g.criteria.n); err != nil {
			return nil, err
		}
		if mean.Valid {
			g.overall.mean = &mean.Float64
		}
		if criteriaMean.Valid {
			g.criteria.mean = &criteriaMean.Float64
		}
		copied := g
		result[id] = &copied
	}
	return result, rows.Err()
}

// weakest finds each group's lowest-scoring criterion among those with enough
// scores to say so.
func (s *Service) weakest(ctx context.Context, scope Scope, groupBy, join string) (map[uuid.UUID]*WeakestCriterion, error) {
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT ON (g) g, key, mean FROM (
			SELECT %s AS g, cf.criterion_key AS key, avg(cf.score)::float8 AS mean
			FROM analytics_criterion_facts cf JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid
			JOIN calls c ON c.call_uuid = f.call_uuid %s
			WHERE %s AND %s IS NOT NULL
			GROUP BY 1, 2 HAVING count(cf.score) >= %d) t
		ORDER BY g, mean`, groupBy, join, q.sql(), groupBy, minSample), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read weakest criteria: %w", err)
	}
	type found struct {
		key  uuid.UUID
		mean float64
	}
	byGroup := map[uuid.UUID]found{}
	var keys []uuid.UUID
	for rows.Next() {
		var group uuid.UUID
		var f found
		if rows.Scan(&group, &f.key, &f.mean) == nil {
			byGroup[group] = f
			keys = append(keys, f.key)
		}
	}
	_ = rows.Close()
	info, err := s.criterionInfo(ctx, keys)
	if err != nil {
		return nil, err
	}
	result := map[uuid.UUID]*WeakestCriterion{}
	for group, f := range byGroup {
		result[group] = &WeakestCriterion{CriterionKey: f.key.String(), Title: info[f.key].title, AvgScore: int(f.mean + 0.5)}
	}
	return result, nil
}

// groupTrends buckets the overall score per group.
func (s *Service) groupTrends(ctx context.Context, scope Scope, groupBy, join string) (map[uuid.UUID][]TrendPoint, error) {
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	bucket := bucketExpr(q, scope, "f.occurred_at")
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s, %s, avg(f.overall_score)::float8, count(f.overall_score)
		FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid %s
		WHERE %s AND %s IS NOT NULL GROUP BY 1, 2 ORDER BY 1, 2`, groupBy, bucket, join, q.sql(), groupBy), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read group trends: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := map[uuid.UUID][]TrendPoint{}
	for rows.Next() {
		var group uuid.UUID
		var point TrendPoint
		var mean sql.NullFloat64
		if err := rows.Scan(&group, &point.Bucket, &mean, &point.N); err != nil {
			return nil, err
		}
		if mean.Valid && point.N > 0 {
			point.Avg = intPtr(int(mean.Float64 + 0.5))
		}
		result[group] = append(result[group], point)
	}
	return result, rows.Err()
}

func (s *Service) teamRow(ctx context.Context, scope Scope) (*TeamRow, error) {
	current, err := s.callTotals(ctx, scope, scope.Period.From, scope.Period.To)
	if err != nil {
		return nil, err
	}
	previous, err := s.callTotals(ctx, scope, scope.Period.PreviousFrom, scope.Period.PreviousTo)
	if err != nil {
		return nil, err
	}
	trend, err := s.trend(ctx, scope, "f.overall_score", "", nil)
	if err != nil {
		return nil, err
	}
	return &TeamRow{Calls: current.calls, AvgScore: shown(current.overall.mean, current.overall.n),
		AvgCriteriaScore: shown(current.criteria.mean, current.criteria.n),
		Delta:            delta(current.overall, previous.overall, false), Sample: sample(current.overall.n), Trend: trend}, nil
}

type person struct {
	name       string
	department *DepartmentRef
	former     bool
}

// people names users and says where they work in the company now.
func (s *Service) people(ctx context.Context, scope Scope, ids []uuid.UUID) (map[uuid.UUID]person, error) {
	result := map[uuid.UUID]person{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, COALESCE(btrim(p.full_name || ' ' || p.full_surname), ''),
		       NOT EXISTS (SELECT 1 FROM company_members m WHERE m.company_uuid = $2 AND m.user_uuid = u.id AND m.status = 'active'),
		       d.department_uuid, d.name
		FROM unnest($1::uuid[]) AS u(id)
		LEFT JOIN user_profiles p ON p.user_uuid = u.id
		LEFT JOIN LATERAL (
			SELECT d.department_uuid, d.name FROM department_members dm JOIN departments d ON d.department_uuid = dm.department_uuid
			WHERE dm.user_uuid = u.id AND dm.status = 'active' AND d.company_uuid = $2 AND d.deleted_at IS NULL
			ORDER BY dm.created_at LIMIT 1) d ON true`, uuidStrings(ids), nullableCompany(scope))
	if err != nil {
		return nil, fmt.Errorf("read people: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.UUID
		var p person
		var department uuid.NullUUID
		var departmentName sql.NullString
		if err := rows.Scan(&id, &p.name, &p.former, &department, &departmentName); err != nil {
			return nil, err
		}
		if scope.Kind == "personal" {
			p.former = false
		}
		if department.Valid {
			p.department = &DepartmentRef{UUID: department.UUID.String(), Name: departmentName.String}
		}
		result[id] = p
	}
	return result, rows.Err()
}

func nullableCompany(scope Scope) any {
	if scope.Kind == "personal" {
		return nil
	}
	return scope.CompanyID
}

const subjectJoin = "JOIN call_subjects cs ON cs.call_uuid = f.call_uuid"

// Employees is the table of employees with a "team" row on top. An employee
// sees only their own row.
func (s *Service) Employees(ctx context.Context, req Request) (EmployeesView, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return EmployeesView{}, err
	}
	if err := scope.requireTeam(); err != nil {
		return EmployeesView{}, err
	}
	var own func(q *query)
	if scope.ownOnly() && scope.Kind != "personal" {
		own = func(q *query) { q.add(fmt.Sprintf("cs.user_uuid = %s", q.arg(scope.UserID))) }
	}
	groupBy := "cs.user_uuid"
	join := subjectJoin
	if scope.Kind == "personal" {
		groupBy, join = "c.uploaded_by_user_uuid", ""
	}
	current, err := s.groupStats(ctx, scope, scope.Period.From, scope.Period.To, groupBy, join, own)
	if err != nil {
		return EmployeesView{}, err
	}
	previous, err := s.groupStats(ctx, scope, scope.Period.PreviousFrom, scope.Period.PreviousTo, groupBy, join, own)
	if err != nil {
		return EmployeesView{}, err
	}
	weakest, err := s.weakest(ctx, scope, groupBy, join)
	if err != nil {
		return EmployeesView{}, err
	}
	trends, err := s.groupTrends(ctx, scope, groupBy, join)
	if err != nil {
		return EmployeesView{}, err
	}
	ids := make([]uuid.UUID, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	names, err := s.people(ctx, scope, ids)
	if err != nil {
		return EmployeesView{}, err
	}
	speech, err := s.speechByEmployee(ctx, scope, scope.Period.From, scope.Period.To)
	if err != nil {
		return EmployeesView{}, err
	}
	view := EmployeesView{Period: scope.periodView(), Employees: []EmployeeRow{}}
	if !scope.ownOnly() {
		if view.Team, err = s.teamRow(ctx, scope); err != nil {
			return EmployeesView{}, err
		}
		if view.Team != nil {
			if view.Team.Speech, err = s.speechMedian(ctx, scope, scope.Period.From, scope.Period.To); err != nil {
				return EmployeesView{}, err
			}
		}
	}
	for _, id := range ids {
		g := current[id]
		prev := previous[id]
		if prev == nil {
			prev = &groupTotals{}
		}
		p := names[id]
		row := EmployeeRow{UserUUID: id.String(), FullName: p.name, Department: p.department, IsFormerMember: p.former, IsMe: id == scope.UserID,
			Calls: g.calls, CallsShared: g.shared, AvgScore: shown(g.overall.mean, g.overall.n), AvgCriteriaScore: shown(g.criteria.mean, g.criteria.n),
			Delta: delta(g.overall, prev.overall, false), Sample: sample(g.overall.n), CriticalMissed: g.critical,
			WeakestCriterion: weakest[id], Trend: trends[id]}
		if row.Trend == nil {
			row.Trend = []TrendPoint{}
		}
		if sp, ok := speech[id]; ok {
			row.Speech = sp
		}
		view.Employees = append(view.Employees, row)
	}
	// The viewer first, then by name; the table sorts itself on the page.
	sort.SliceStable(view.Employees, func(i, j int) bool {
		a, b := view.Employees[i], view.Employees[j]
		if a.IsMe != b.IsMe {
			return a.IsMe
		}
		return a.FullName < b.FullName
	})
	view.Total = len(view.Employees)
	return view, nil
}

// Departments is for the owner and the deputy; a department leader gets their
// departments and the company row to compare with.
func (s *Service) Departments(ctx context.Context, req Request) (DepartmentsView, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return DepartmentsView{}, err
	}
	if scope.ownOnly() {
		return DepartmentsView{}, ErrForbidden
	}
	if err := scope.requireTeam(); err != nil {
		return DepartmentsView{}, err
	}
	const groupBy = "c.department_uuid"
	current, err := s.groupStats(ctx, scope, scope.Period.From, scope.Period.To, groupBy, "", nil)
	if err != nil {
		return DepartmentsView{}, err
	}
	previous, err := s.groupStats(ctx, scope, scope.Period.PreviousFrom, scope.Period.PreviousTo, groupBy, "", nil)
	if err != nil {
		return DepartmentsView{}, err
	}
	weakest, err := s.weakest(ctx, scope, groupBy, "")
	if err != nil {
		return DepartmentsView{}, err
	}
	trends, err := s.groupTrends(ctx, scope, groupBy, "")
	if err != nil {
		return DepartmentsView{}, err
	}
	company := scope
	company.Role, company.Department, company.LedDepartments = roleManager, nullUUID(), nil
	view := DepartmentsView{Period: scope.periodView(), Departments: []DepartmentRow{}}
	if view.Company, err = s.teamRow(ctx, company); err != nil {
		return DepartmentsView{}, err
	}
	employees := map[uuid.UUID]int{}
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.department_uuid, count(DISTINCT cs.user_uuid) FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid `+subjectJoin+`
		WHERE `+q.sql()+` AND c.department_uuid IS NOT NULL GROUP BY 1`, q.args...)
	if err != nil {
		return DepartmentsView{}, fmt.Errorf("count department employees: %w", err)
	}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if rows.Scan(&id, &n) == nil {
			employees[id] = n
		}
	}
	_ = rows.Close()
	names := map[uuid.UUID]string{}
	rows, err = s.db.QueryContext(ctx, `SELECT department_uuid, name FROM departments WHERE company_uuid = $1`, scope.CompanyID)
	if err != nil {
		return DepartmentsView{}, fmt.Errorf("read departments: %w", err)
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if rows.Scan(&id, &name) == nil {
			names[id] = name
		}
	}
	_ = rows.Close()
	for id, g := range current {
		prev := previous[id]
		if prev == nil {
			prev = &groupTotals{}
		}
		row := DepartmentRow{DepartmentUUID: id.String(), Name: names[id], Employees: employees[id], Calls: g.calls,
			AvgScore: shown(g.overall.mean, g.overall.n), AvgCriteriaScore: shown(g.criteria.mean, g.criteria.n),
			Delta: delta(g.overall, prev.overall, false), Sample: sample(g.overall.n), CriticalMissed: g.critical,
			WeakestCriterion: weakest[id], Trend: trends[id]}
		if row.Trend == nil {
			row.Trend = []TrendPoint{}
		}
		view.Departments = append(view.Departments, row)
	}
	sort.SliceStable(view.Departments, func(i, j int) bool { return view.Departments[i].Name < view.Departments[j].Name })
	view.Total = len(view.Departments)
	return view, nil
}
