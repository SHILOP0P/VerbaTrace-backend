package teamanalytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	callRepo "verbatrace/monolit/internal/repository/call"

	"github.com/google/uuid"
)

type MatrixCriterion struct {
	CriterionKey string `json:"criterion_key"`
	Title        string `json:"title"`
	IsCritical   bool   `json:"is_critical"`
}

type MatrixCell struct {
	CriterionKey string `json:"criterion_key"`
	AvgScore     *int   `json:"avg_score"`
	N            int    `json:"n"`
	Sample       string `json:"sample"`
}

type MatrixRow struct {
	UserUUID string       `json:"user_uuid"`
	FullName string       `json:"full_name"`
	Cells    []MatrixCell `json:"cells"`
}

type MatrixView struct {
	Period      PeriodView        `json:"period"`
	Instruction InstructionRef    `json:"instruction"`
	Criteria    []MatrixCriterion `json:"criteria"`
	Rows        []MatrixRow       `json:"rows"`
}

// Matrix is the employee by criterion grid of one instruction.
func (s *Service) Matrix(ctx context.Context, req Request) (MatrixView, error) {
	if !req.InstructionID.Valid {
		return MatrixView{}, ErrInvalidRequest
	}
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return MatrixView{}, err
	}
	if scope.ownOnly() {
		return MatrixView{}, ErrForbidden
	}
	if err := scope.requireTeam(); err != nil {
		return MatrixView{}, err
	}
	view := MatrixView{Period: scope.periodView(), Criteria: []MatrixCriterion{}, Rows: []MatrixRow{}}
	var deleted bool
	err = s.db.QueryRowContext(ctx, `SELECT title, deleted_at IS NOT NULL FROM analysis_instructions WHERE instruction_uuid = $1 AND company_uuid = $2`, req.InstructionID.UUID, scope.CompanyID).
		Scan(&view.Instruction.Title, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return MatrixView{}, ErrNotFound
	}
	if err != nil {
		return MatrixView{}, fmt.Errorf("read instruction: %w", err)
	}
	view.Instruction.UUID, view.Instruction.Deleted = req.InstructionID.UUID.String(), deleted
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.criterion_key, c.title, c.is_critical
		FROM instruction_scorecards sc JOIN instruction_scorecard_criteria c ON c.scorecard_uuid = sc.scorecard_uuid
		WHERE sc.instruction_uuid = $1 AND sc.is_current AND c.enabled
		ORDER BY c.position LIMIT $2`, req.InstructionID.UUID, maxMatrixCriteria)
	if err != nil {
		return MatrixView{}, fmt.Errorf("read instruction criteria: %w", err)
	}
	var keys []uuid.UUID
	for rows.Next() {
		var key uuid.UUID
		var c MatrixCriterion
		if rows.Scan(&key, &c.Title, &c.IsCritical) == nil {
			c.CriterionKey = key.String()
			view.Criteria = append(view.Criteria, c)
			keys = append(keys, key)
		}
	}
	_ = rows.Close()
	canonical, err := s.canonical(ctx, keys)
	if err != nil {
		return MatrixView{}, err
	}
	storedKeys := make([]uuid.UUID, 0, len(keys))
	for _, key := range keys {
		storedKeys = append(storedKeys, canonical[key])
	}
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	q.add(fmt.Sprintf("cf.criterion_key = ANY(%s::uuid[])", q.arg(uuidStrings(storedKeys))))
	rows, err = s.db.QueryContext(ctx, `
		SELECT cs.user_uuid, cf.criterion_key, avg(cf.score)::float8, count(cf.score)
		FROM analytics_criterion_facts cf JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid
		JOIN calls c ON c.call_uuid = f.call_uuid `+subjectJoin+`
		WHERE `+q.sql()+` GROUP BY 1, 2`, q.args...)
	if err != nil {
		return MatrixView{}, fmt.Errorf("read matrix: %w", err)
	}
	type cell struct {
		mean *float64
		n    int
	}
	cells := map[uuid.UUID]map[uuid.UUID]cell{}
	var users []uuid.UUID
	for rows.Next() {
		var user, key uuid.UUID
		var mean sql.NullFloat64
		var n int
		if rows.Scan(&user, &key, &mean, &n) != nil {
			continue
		}
		if cells[user] == nil {
			cells[user] = map[uuid.UUID]cell{}
			users = append(users, user)
		}
		c := cell{n: n}
		if mean.Valid {
			c.mean = &mean.Float64
		}
		cells[user][key] = c
	}
	_ = rows.Close()
	names, err := s.people(ctx, scope, users)
	if err != nil {
		return MatrixView{}, err
	}
	for _, user := range users {
		row := MatrixRow{UserUUID: user.String(), FullName: names[user].name, Cells: []MatrixCell{}}
		for i, key := range keys {
			c := cells[user][storedKeys[i]]
			row.Cells = append(row.Cells, MatrixCell{CriterionKey: key.String(), AvgScore: shown(c.mean, c.n), N: c.n, Sample: sample(c.n)})
		}
		view.Rows = append(view.Rows, row)
	}
	sortRows(view.Rows)
	return view, nil
}

func sortRows(rows []MatrixRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].FullName < rows[j-1].FullName; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

type ProfileEmployee struct {
	UserUUID       string         `json:"user_uuid"`
	FullName       string         `json:"full_name"`
	Department     *DepartmentRef `json:"department"`
	IsFormerMember bool           `json:"is_former_member"`
}

type ProfileTotals struct {
	Calls            int    `json:"calls"`
	AvgScore         *int   `json:"avg_score"`
	AvgCriteriaScore *int   `json:"avg_criteria_score"`
	Delta            Delta  `json:"delta"`
	Sample           string `json:"sample"`
	CriticalMissed   int    `json:"critical_missed"`
}

type Reference struct {
	Label    string       `json:"label"`
	AvgScore *int         `json:"avg_score"`
	Trend    []TrendPoint `json:"trend"`
	Hidden   bool         `json:"hidden"`
}

type ProfileCriterion struct {
	CriterionKey string         `json:"criterion_key"`
	Title        string         `json:"title"`
	Instruction  InstructionRef `json:"instruction"`
	OwnAvg       *int           `json:"own_avg"`
	OwnN         int            `json:"own_n"`
	ReferenceAvg *int           `json:"reference_avg"`
	Delta        Delta          `json:"delta"`
	Sample       string         `json:"sample"`
}

type WorthListening struct {
	CallUUID       string    `json:"call_uuid"`
	Title          string    `json:"title"`
	OccurredAt     time.Time `json:"occurred_at"`
	OverallScore   *int      `json:"overall_score"`
	CriticalMissed int       `json:"critical_missed"`
	CanOpen        bool      `json:"can_open"`
}

type Profile struct {
	Period         PeriodView         `json:"period"`
	Employee       ProfileEmployee    `json:"employee"`
	Totals         ProfileTotals      `json:"totals"`
	Trend          []TrendPoint       `json:"trend"`
	Reference      Reference          `json:"reference"`
	Criteria       []ProfileCriterion `json:"criteria"`
	WorthListening []WorthListening   `json:"worth_listening"`
}

// Profile is one employee's page: their numbers against their department's,
// and the calls worth listening to. target is the employee, or uuid.Nil for
// the viewer themselves.
func (s *Service) Profile(ctx context.Context, req Request, target uuid.UUID) (Profile, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return Profile{}, err
	}
	if target == uuid.Nil || target == scope.UserID {
		target = scope.UserID
		if err := scope.requireOwn(); err != nil {
			return Profile{}, err
		}
	} else {
		if scope.ownOnly() {
			return Profile{}, ErrForbidden
		}
		if err := scope.requireTeam(); err != nil {
			return Profile{}, err
		}
		if scope.Role == roleLeader {
			var member bool
			if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM department_members WHERE user_uuid = $1 AND department_uuid = ANY($2::uuid[]))`, target, uuidStrings(scope.LedDepartments)).Scan(&member); err != nil {
				return Profile{}, err
			}
			if !member {
				return Profile{}, ErrForbidden
			}
		}
	}
	own := scope
	own.Employee = uuid.NullUUID{UUID: target, Valid: true}
	names, err := s.people(ctx, scope, []uuid.UUID{target})
	if err != nil {
		return Profile{}, err
	}
	p := names[target]
	profile := Profile{Period: scope.periodView(), Employee: ProfileEmployee{UserUUID: target.String(), FullName: p.name, Department: p.department, IsFormerMember: p.former},
		Criteria: []ProfileCriterion{}, WorthListening: []WorthListening{}}
	current, err := s.callTotals(ctx, own, own.Period.From, own.Period.To)
	if err != nil {
		return Profile{}, err
	}
	previous, err := s.callTotals(ctx, own, own.Period.PreviousFrom, own.Period.PreviousTo)
	if err != nil {
		return Profile{}, err
	}
	profile.Totals = ProfileTotals{Calls: current.calls, AvgScore: shown(current.overall.mean, current.overall.n),
		AvgCriteriaScore: shown(current.criteria.mean, current.criteria.n), Delta: delta(current.overall, previous.overall, false),
		Sample: sample(current.overall.n), CriticalMissed: current.critical}
	if profile.Trend, err = s.trend(ctx, own, "f.overall_score", "", nil); err != nil {
		return Profile{}, err
	}
	reference, refScope, err := s.reference(ctx, scope, p)
	if err != nil {
		return Profile{}, err
	}
	profile.Reference = reference
	ownStats, err := s.criterionStats(ctx, own, own.Period.From, own.Period.To, nil)
	if err != nil {
		return Profile{}, err
	}
	ownPrevious, err := s.criterionStats(ctx, own, own.Period.PreviousFrom, own.Period.PreviousTo, nil)
	if err != nil {
		return Profile{}, err
	}
	var refStats map[uuid.UUID]*criterionStats
	if !reference.Hidden && refScope != nil {
		if refStats, err = s.criterionStats(ctx, *refScope, refScope.Period.From, refScope.Period.To, nil); err != nil {
			return Profile{}, err
		}
	}
	keys := make([]uuid.UUID, 0, len(ownStats))
	for key := range ownStats {
		keys = append(keys, key)
	}
	info, err := s.criterionInfo(ctx, keys)
	if err != nil {
		return Profile{}, err
	}
	for _, key := range keys {
		st := ownStats[key]
		prev := ownPrevious[key]
		if prev == nil {
			prev = &criterionStats{}
		}
		row := ProfileCriterion{CriterionKey: key.String(), Title: info[key].title, Instruction: info[key].instruction,
			OwnAvg: shown(st.mean, st.n), OwnN: st.n, Delta: delta(st.stat, prev.stat, false), Sample: sample(st.n)}
		if ref := refStats[key]; ref != nil {
			row.ReferenceAvg = shown(ref.mean, ref.n)
		}
		profile.Criteria = append(profile.Criteria, row)
	}
	sortProfileCriteria(profile.Criteria)
	if profile.WorthListening, err = s.worthListening(ctx, own); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func sortProfileCriteria(rows []ProfileCriterion) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && value(rows[j].OwnAvg) < value(rows[j-1].OwnAvg); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// reference is the department the employee belongs to, or the company when
// they have none. It is hidden when fewer than three people had calls: a mean
// of two could be read back to a colleague.
func (s *Service) reference(ctx context.Context, scope Scope, p person) (Reference, *Scope, error) {
	if scope.Kind == "personal" {
		return Reference{Label: "", Hidden: true, Trend: []TrendPoint{}}, nil, nil
	}
	ref := scope
	ref.Role, ref.Employee, ref.LedDepartments = roleManager, nullUUID(), nil
	label := "Компания"
	ref.Department = nullUUID()
	if p.department != nil {
		id, _ := uuid.Parse(p.department.UUID)
		ref.Department = uuid.NullUUID{UUID: id, Valid: true}
		label = "Отдел «" + p.department.Name + "»"
	}
	q := &query{}
	ref.callsOf(q, ref.Period.From, ref.Period.To)
	var people int
	if err := s.db.QueryRowContext(ctx, `SELECT count(DISTINCT cs.user_uuid) FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid `+subjectJoin+` WHERE `+q.sql(), q.args...).Scan(&people); err != nil {
		return Reference{}, nil, fmt.Errorf("count reference people: %w", err)
	}
	if people < referenceMinPeople {
		return Reference{Label: label, Hidden: true, Trend: []TrendPoint{}}, nil, nil
	}
	totals, err := s.callTotals(ctx, ref, ref.Period.From, ref.Period.To)
	if err != nil {
		return Reference{}, nil, err
	}
	trend, err := s.trend(ctx, ref, "f.overall_score", "", nil)
	if err != nil {
		return Reference{}, nil, err
	}
	return Reference{Label: label, AvgScore: shown(totals.overall.mean, totals.overall.n), Trend: trend}, &ref, nil
}

// worthListening are the employee's weakest calls of the period and those with
// a critical miss. A call the viewer may not open is counted but not named.
func (s *Service) worthListening(ctx context.Context, own Scope) ([]WorthListening, error) {
	q := &query{}
	own.callsOf(q, own.Period.From, own.Period.To)
	viewer := q.arg(own.UserID)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.call_uuid, c.title, f.occurred_at, f.overall_score, f.critical_missed, %s AS can_open
		FROM analytics_call_facts f JOIN calls c ON c.call_uuid = f.call_uuid
		WHERE %s AND (f.critical_missed > 0 OR f.overall_score IS NOT NULL)
		ORDER BY f.critical_missed > 0 DESC, f.overall_score ASC NULLS LAST, f.occurred_at DESC
		LIMIT %d`, callRepo.VisibleToUserCondition("c", viewer), q.sql(), worthListeningCount), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read worth listening: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []WorthListening{}
	for rows.Next() {
		var item WorthListening
		var id uuid.UUID
		var score sql.NullInt64
		if err := rows.Scan(&id, &item.Title, &item.OccurredAt, &score, &item.CriticalMissed, &item.CanOpen); err != nil {
			return nil, err
		}
		item.CallUUID = id.String()
		if score.Valid {
			item.OverallScore = intPtr(int(score.Int64))
		}
		if !item.CanOpen {
			item.Title = ""
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type DrillEmployee struct {
	UserUUID string `json:"user_uuid"`
	FullName string `json:"full_name"`
}

type DrillCall struct {
	CallUUID                string          `json:"call_uuid"`
	Title                   string          `json:"title"`
	OccurredAt              time.Time       `json:"occurred_at"`
	Employees               []DrillEmployee `json:"employees"`
	Status                  string          `json:"status"`
	Score                   *int            `json:"score"`
	ScoreSource             string          `json:"score_source"`
	ItemID                  string          `json:"item_id"`
	EvidenceStartSeconds    *float64        `json:"evidence_start_seconds"`
	IsShared                bool            `json:"is_shared"`
	SubjectsChangedManually bool            `json:"subjects_changed_manually"`
	CanOpen                 bool            `json:"can_open"`
}

type DrillCriterion struct {
	CriterionKey string         `json:"criterion_key"`
	Title        string         `json:"title"`
	Instruction  InstructionRef `json:"instruction"`
}

type DrillDown struct {
	Criterion DrillCriterion `json:"criterion"`
	Calls     []DrillCall    `json:"calls"`
	Total     int            `json:"total"`
	Limit     int            `json:"limit"`
	Offset    int            `json:"offset"`
}

// CriterionCalls opens a number: every call behind a criterion's figure, with
// the moment in the recording the judgement rests on.
func (s *Service) CriterionCalls(ctx context.Context, req Request, key uuid.UUID, status, sortBy string, limit, offset int) (DrillDown, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return DrillDown{}, err
	}
	if err := scope.requireTeam(); err != nil {
		return DrillDown{}, err
	}
	if limit <= 0 || limit > maxDrillDownLimit {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	canonical, err := s.canonical(ctx, []uuid.UUID{key})
	if err != nil {
		return DrillDown{}, err
	}
	stored := canonical[key]
	info, err := s.criterionInfo(ctx, []uuid.UUID{stored})
	if err != nil {
		return DrillDown{}, err
	}
	out := DrillDown{Criterion: DrillCriterion{CriterionKey: key.String(), Title: info[stored].title, Instruction: info[stored].instruction},
		Calls: []DrillCall{}, Limit: limit, Offset: offset}
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	q.add(fmt.Sprintf("cf.criterion_key = %s", q.arg(stored)))
	if status != "" {
		q.add(fmt.Sprintf("(%s) = %s", effectiveStatus, q.arg(status)))
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM analytics_criterion_facts cf JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid JOIN calls c ON c.call_uuid = f.call_uuid WHERE `+q.sql(), q.args...).Scan(&out.Total); err != nil {
		return DrillDown{}, fmt.Errorf("count criterion calls: %w", err)
	}
	orderBy := "f.occurred_at DESC"
	if sortBy == "score" {
		orderBy = "cf.score ASC NULLS LAST, f.occurred_at DESC"
	}
	viewer := q.arg(scope.UserID)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.call_uuid, c.title, f.occurred_at, %s, cf.score, cf.human_score IS NOT NULL OR cf.human_decision IS NOT NULL,
		       cf.item_id, cf.evidence_start_seconds::float8, f.is_shared,
		       EXISTS (SELECT 1 FROM call_subject_events e WHERE e.call_uuid = c.call_uuid AND e.actor_user_uuid IS NOT NULL),
		       %s
		FROM analytics_criterion_facts cf JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid JOIN calls c ON c.call_uuid = f.call_uuid
		WHERE %s ORDER BY %s LIMIT %d OFFSET %d`, effectiveStatus, callRepo.VisibleToUserCondition("c", viewer), q.sql(), orderBy, limit, offset), q.args...)
	if err != nil {
		return DrillDown{}, fmt.Errorf("read criterion calls: %w", err)
	}
	var callIDs []uuid.UUID
	for rows.Next() {
		var item DrillCall
		var id uuid.UUID
		var score sql.NullInt64
		var human bool
		var start sql.NullFloat64
		if err := rows.Scan(&id, &item.Title, &item.OccurredAt, &item.Status, &score, &human, &item.ItemID, &start, &item.IsShared, &item.SubjectsChangedManually, &item.CanOpen); err != nil {
			_ = rows.Close()
			return DrillDown{}, err
		}
		item.CallUUID = id.String()
		item.ScoreSource = "ai"
		if human {
			item.ScoreSource = "human"
		}
		if score.Valid {
			item.Score = intPtr(int(score.Int64))
		}
		if start.Valid {
			item.EvidenceStartSeconds = &start.Float64
		}
		if !item.CanOpen {
			item.Title = ""
		}
		item.Employees = []DrillEmployee{}
		out.Calls = append(out.Calls, item)
		callIDs = append(callIDs, id)
	}
	_ = rows.Close()
	if len(callIDs) > 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT cs.call_uuid, cs.user_uuid, COALESCE(btrim(p.full_name || ' ' || p.full_surname), '')
			FROM call_subjects cs LEFT JOIN user_profiles p ON p.user_uuid = cs.user_uuid
			WHERE cs.call_uuid = ANY($1::uuid[]) ORDER BY cs.is_primary DESC`, uuidStrings(callIDs))
		if err != nil {
			return DrillDown{}, fmt.Errorf("read call employees: %w", err)
		}
		byCall := map[string][]DrillEmployee{}
		for rows.Next() {
			var call, user uuid.UUID
			var name string
			if rows.Scan(&call, &user, &name) == nil {
				byCall[call.String()] = append(byCall[call.String()], DrillEmployee{UserUUID: user.String(), FullName: name})
			}
		}
		_ = rows.Close()
		for i := range out.Calls {
			if employees := byCall[out.Calls[i].CallUUID]; employees != nil {
				out.Calls[i].Employees = employees
			}
		}
	}
	return out, nil
}
