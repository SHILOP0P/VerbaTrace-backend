package teamanalytics

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

type Marker struct {
	Date  string `json:"date"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type Summary struct {
	Period                     PeriodView   `json:"period"`
	CallsTotal                 int          `json:"calls_total"`
	CallsAnalyzed              int          `json:"calls_analyzed"`
	CallsWithoutFixedScorecard int          `json:"calls_without_fixed_scorecard"`
	CallsShared                int          `json:"calls_shared"`
	CallsInternalExcluded      int          `json:"calls_internal_excluded"`
	AvgScore                   *int         `json:"avg_score"`
	AvgCriteriaScore           *int         `json:"avg_criteria_score"`
	Delta                      Delta        `json:"delta"`
	Sample                     string       `json:"sample"`
	CriticalMissed             int          `json:"critical_missed"`
	Trend                      []TrendPoint `json:"trend"`
	Markers                    []Marker     `json:"markers"`
	// For a department leader: the same average for the whole company.
	CompanyAvgScore *int `json:"company_avg_score,omitempty"`
}

// callTotals is the call-level aggregate of one period.
type callTotals struct {
	calls, withoutFixed, shared, critical int
	overall, criteria                     stat
}

func (s *Service) callTotals(ctx context.Context, scope Scope, from, to time.Time) (callTotals, error) {
	q := &query{}
	scope.callsOf(q, from, to)
	var t callTotals
	var mean, criteriaMean sql.NullFloat64
	var variance sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE f.scorecard_mode <> 'fixed'), count(*) FILTER (WHERE f.is_shared),
		       COALESCE(sum(f.critical_missed), 0),
		       avg(f.overall_score)::float8, COALESCE(var_samp(f.overall_score), 0)::float8, count(f.overall_score),
		       avg(f.criteria_score)::float8, count(f.criteria_score)
		FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid
		WHERE `+q.sql(), q.args...).
		Scan(&t.calls, &t.withoutFixed, &t.shared, &t.critical, &mean, &variance, &t.overall.n, &criteriaMean, &t.criteria.n)
	if err != nil {
		return t, fmt.Errorf("read call totals: %w", err)
	}
	if mean.Valid {
		t.overall.mean, t.overall.variance = &mean.Float64, variance.Float64
	}
	if criteriaMean.Valid {
		t.criteria.mean = &criteriaMean.Float64
	}
	return t, nil
}

// Summary is the strip on top of the analytics page.
func (s *Service) Summary(ctx context.Context, req Request) (Summary, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return Summary{}, err
	}
	if err := scope.requireTeam(); err != nil {
		return Summary{}, err
	}
	p := scope.Period
	current, err := s.callTotals(ctx, scope, p.From, p.To)
	if err != nil {
		return Summary{}, err
	}
	previous, err := s.callTotals(ctx, scope, p.PreviousFrom, p.PreviousTo)
	if err != nil {
		return Summary{}, err
	}
	changed, err := s.scorecardsChanged(ctx, scope)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{
		Period: scope.periodView(), CallsAnalyzed: current.calls, CallsWithoutFixedScorecard: current.withoutFixed,
		CallsShared: current.shared, CriticalMissed: current.critical,
		AvgScore: shown(current.overall.mean, current.overall.n), AvgCriteriaScore: shown(current.criteria.mean, current.criteria.n),
		Delta: delta(current.overall, previous.overall, changed), Sample: sample(current.overall.n),
	}
	if out.CallsTotal, err = s.callsTotal(ctx, scope); err != nil {
		return Summary{}, err
	}
	if !scope.IncludeInternal {
		internal := scope
		internal.IncludeInternal = true
		q := &query{}
		internal.callsOf(q, p.From, p.To)
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid WHERE f.is_internal AND `+q.sql(), q.args...).Scan(&out.CallsInternalExcluded); err != nil {
			return Summary{}, fmt.Errorf("count internal calls: %w", err)
		}
	}
	if out.Trend, err = s.trend(ctx, scope, "f.overall_score", "", nil); err != nil {
		return Summary{}, err
	}
	if out.Markers, err = s.markers(ctx, scope); err != nil {
		return Summary{}, err
	}
	if scope.Role == roleLeader {
		company := scope
		company.Role, company.Department, company.LedDepartments = roleManager, nullUUID(), nil
		totals, err := s.callTotals(ctx, company, p.From, p.To)
		if err != nil {
			return Summary{}, err
		}
		out.CompanyAvgScore = shown(totals.overall.mean, totals.overall.n)
	}
	return out, nil
}

// callsTotal counts every call of the scope in the period, analysed or not.
func (s *Service) callsTotal(ctx context.Context, scope Scope) (int, error) {
	q := &query{}
	q.add("c.deleted_at IS NULL")
	q.add(fmt.Sprintf("COALESCE(c.occurred_at, c.created_at) >= %s AND COALESCE(c.occurred_at, c.created_at) < %s", q.arg(scope.Period.From), q.arg(scope.Period.To)))
	if scope.Kind == "personal" {
		q.add(fmt.Sprintf("c.company_uuid IS NULL AND c.uploaded_by_user_uuid = %s", q.arg(scope.UserID)))
	} else {
		q.add(fmt.Sprintf("c.company_uuid = %s", q.arg(scope.CompanyID)))
		switch {
		case scope.Department.Valid:
			q.add(fmt.Sprintf("c.department_uuid = %s", q.arg(scope.Department.UUID)))
		case scope.Role == roleLeader:
			q.add(fmt.Sprintf("c.department_uuid = ANY(%s::uuid[])", q.arg(uuidStrings(scope.LedDepartments))))
		}
		if scope.Employee.Valid {
			q.add(fmt.Sprintf("EXISTS (SELECT 1 FROM call_subjects cs0 WHERE cs0.call_uuid = c.call_uuid AND cs0.user_uuid = %s)", q.arg(scope.Employee.UUID)))
		}
	}
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM calls c WHERE `+q.sql(), q.args...).Scan(&total)
	return total, err
}

// trend buckets a score column over the period; narrow adds conditions of its
// own and may join more tables through join.
func (s *Service) trend(ctx context.Context, scope Scope, column, join string, narrow func(q *query)) ([]TrendPoint, error) {
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	if narrow != nil {
		narrow(q)
	}
	bucket := bucketExpr(q, scope, "f.occurred_at")
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s AS bucket, avg(%s)::float8, count(%s)
		FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid %s
		WHERE %s GROUP BY 1 ORDER BY 1`, bucket, column, column, join, q.sql()), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read trend: %w", err)
	}
	defer func() { _ = rows.Close() }()
	points := []TrendPoint{}
	for rows.Next() {
		var point TrendPoint
		var mean sql.NullFloat64
		if err := rows.Scan(&point.Bucket, &mean, &point.N); err != nil {
			return nil, err
		}
		if mean.Valid && point.N > 0 {
			point.Avg = intPtr(int(mean.Float64 + 0.5))
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

// markers are the dates the yardstick moved: a new instruction version, or a
// new pipeline judging the calls.
func (s *Service) markers(ctx context.Context, scope Scope) ([]Marker, error) {
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	tz := q.arg(scope.Timezone)
	from, to := q.arg(scope.Period.From), q.arg(scope.Period.To)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT to_char(date_trunc('day', v.created_at, %s), 'YYYY-MM-DD'), 'instruction_version', v.title_snapshot || ', версия ' || v.version
		FROM analysis_instruction_versions v
		WHERE v.created_at >= %s AND v.created_at < %s AND v.version > 1 AND v.instruction_uuid IN (
			SELECT DISTINCT cf.instruction_uuid FROM analytics_criterion_facts cf
			JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid JOIN calls c ON c.call_uuid = f.call_uuid
			WHERE %s)
		UNION ALL
		SELECT to_char(date_trunc('day', min(f.occurred_at), %s), 'YYYY-MM-DD'), 'judge_changed', f.pipeline_version
		FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid
		WHERE %s AND f.pipeline_version <> ''
		GROUP BY f.pipeline_version`, tz, from, to, q.sql(), tz, q.sql()), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read markers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	markers := []Marker{}
	judges := []Marker{}
	for rows.Next() {
		var m Marker
		if err := rows.Scan(&m.Date, &m.Kind, &m.Label); err != nil {
			return nil, err
		}
		if m.Kind == "judge_changed" {
			judges = append(judges, m)
			continue
		}
		markers = append(markers, m)
	}
	// The first judge of the period is the baseline, not a change.
	sort.Slice(judges, func(i, j int) bool { return judges[i].Date < judges[j].Date })
	if len(judges) > 1 {
		markers = append(markers, judges[1:]...)
	}
	sort.SliceStable(markers, func(i, j int) bool { return markers[i].Date < markers[j].Date })
	return markers, rows.Err()
}

// scorecardsChanged says whether the calls of the two periods were scored on
// different scorecards.
func (s *Service) scorecardsChanged(ctx context.Context, scope Scope) (bool, error) {
	sets := [2]map[string]bool{{}, {}}
	for i, window := range [][2]time.Time{{scope.Period.From, scope.Period.To}, {scope.Period.PreviousFrom, scope.Period.PreviousTo}} {
		q := &query{}
		scope.callsOf(q, window[0], window[1])
		rows, err := s.db.QueryContext(ctx, `
			SELECT DISTINCT cf.scorecard_uuid::text FROM analytics_criterion_facts cf
			JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid JOIN calls c ON c.call_uuid = f.call_uuid
			WHERE `+q.sql(), q.args...)
		if err != nil {
			return false, fmt.Errorf("read scorecards of a period: %w", err)
		}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				sets[i][id] = true
			}
		}
		_ = rows.Close()
	}
	if len(sets[0]) == 0 || len(sets[1]) == 0 || len(sets[0]) != len(sets[1]) {
		return len(sets[0]) > 0 && len(sets[1]) > 0, nil
	}
	for id := range sets[0] {
		if !sets[1][id] {
			return true, nil
		}
	}
	return false, nil
}
