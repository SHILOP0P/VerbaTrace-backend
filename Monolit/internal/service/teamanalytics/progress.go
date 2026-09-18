package teamanalytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	callRepo "verbatrace/monolit/internal/repository/call"

	"github.com/google/uuid"
)

// The work on mistakes compares each criterion of a call with the same
// criterion in the employee's previous call where it was scored. It keeps no
// state: the chain is read from the facts, so it survives a change of subject,
// a backfill and a QA override.
const (
	passScore     = 75
	closingStreak = 3
)

// Verdicts of one criterion against the previous scored call.
const (
	VerdictFixed     = "fixed"
	VerdictRepeated  = "repeated"
	VerdictNew       = "new"
	VerdictHolding   = "holding"
	VerdictFirstTime = "first_time"
)

// Why a call has no work-on-mistakes block.
const (
	ProgressSharedCall       = "shared_call"
	ProgressInternalCall     = "internal_call"
	ProgressNoFixedScorecard = "no_fixed_scorecard"
	ProgressNoAnalysis       = "no_analysis"
)

type ProgressEmployee struct {
	UserUUID string `json:"user_uuid"`
	FullName string `json:"full_name"`
}

type ProgressCounts struct {
	Fixed     int `json:"fixed"`
	Repeated  int `json:"repeated"`
	New       int `json:"new"`
	Holding   int `json:"holding"`
	FirstTime int `json:"first_time"`
}

type ProgressScore struct {
	Score  int    `json:"score"`
	ItemID string `json:"item_id"`
}

type ProgressPrevious struct {
	CallUUID   string    `json:"call_uuid"`
	OccurredAt time.Time `json:"occurred_at"`
	Score      int       `json:"score"`
	ItemID     string    `json:"item_id"`
	CanOpen    bool      `json:"can_open"`
}

type ProgressCriterion struct {
	CriterionKey string            `json:"criterion_key"`
	Title        string            `json:"title"`
	Verdict      string            `json:"verdict"`
	Current      ProgressScore     `json:"current"`
	Previous     *ProgressPrevious `json:"previous"`
	RepeatStreak int               `json:"repeat_streak"`
}

// CallGrowthArea is a growth area observed in one call (second queue of the
// work on mistakes).
type CallGrowthArea struct {
	AreaUUID string   `json:"area_uuid"`
	Title    string   `json:"title"`
	Verdict  string   `json:"verdict"`
	Note     string   `json:"note"`
	ItemIDs  []string `json:"item_ids"`
}

type CallProgress struct {
	Available         bool                `json:"available"`
	UnavailableReason *string             `json:"unavailable_reason"`
	Employee          *ProgressEmployee   `json:"employee"`
	Counts            ProgressCounts      `json:"counts"`
	Criteria          []ProgressCriterion `json:"criteria"`
	GrowthAreas       []CallGrowthArea    `json:"growth_areas"`
}

type OpenMistake struct {
	CriterionKey  string    `json:"criterion_key"`
	Title         string    `json:"title"`
	LastScore     int       `json:"last_score"`
	RepeatStreak  int       `json:"repeat_streak"`
	FirstFailedAt time.Time `json:"first_failed_at"`
	LastCallUUID  string    `json:"last_call_uuid"`
}

type ClosedMistake struct {
	CriterionKey string    `json:"criterion_key"`
	Title        string    `json:"title"`
	ClosedAt     time.Time `json:"closed_at"`
}

// GrowthObservationView is one call where a growth area was seen.
type GrowthObservationView struct {
	CallUUID   string    `json:"call_uuid"`
	OccurredAt time.Time `json:"occurred_at"`
	Verdict    string    `json:"verdict"`
	Note       string    `json:"note"`
	ItemIDs    []string  `json:"item_ids"`
	CanOpen    bool      `json:"can_open"`
}

type EmployeeGrowthArea struct {
	AreaUUID     string                  `json:"area_uuid"`
	Title        string                  `json:"title"`
	Description  string                  `json:"description"`
	Status       string                  `json:"status"`
	Occurrences  int                     `json:"occurrences"`
	CleanStreak  int                     `json:"clean_streak"`
	Returned     bool                    `json:"returned"`
	Observations []GrowthObservationView `json:"observations"`
}

type EmployeeProgress struct {
	Open           []OpenMistake        `json:"open"`
	ClosedInPeriod []ClosedMistake      `json:"closed_in_period"`
	GrowthAreas    []EmployeeGrowthArea `json:"growth_areas"`
}

// chainOwner says whose calls make up a chain: an employee's calls of one
// company, or the personal calls of their uploader.
type chainOwner struct {
	company uuid.NullUUID
	user    uuid.UUID
}

// chainCalls limits facts f joined to calls c to the owner's calls that can
// carry the work on mistakes: a shared or an internal call cannot say who fixed
// or repeated a mistake, so it is not a link of the chain.
func (o chainOwner) chainCalls(q *query) {
	q.add("c.deleted_at IS NULL AND NOT f.is_shared AND NOT f.is_internal")
	if o.company.Valid {
		q.add(fmt.Sprintf("c.company_uuid = %s AND EXISTS (SELECT 1 FROM call_subjects cs0 WHERE cs0.call_uuid = c.call_uuid AND cs0.user_uuid = %s)", q.arg(o.company.UUID), q.arg(o.user)))
		return
	}
	q.add(fmt.Sprintf("c.company_uuid IS NULL AND c.uploaded_by_user_uuid = %s", q.arg(o.user)))
}

// chainRows is the scored criterion results of the chain with, for each, the
// previous link, the failures in the current failing run and the passes in the
// current passing run. N/A leaves no row, so it does not break a chain.
func chainRows(where string) string {
	return fmt.Sprintf(`
	WITH chain AS (
		SELECT k.call_uuid, k.criterion_key, k.item_id, k.score, f.occurred_at
		FROM analytics_criterion_facts k
		JOIN analytics_call_facts f ON f.call_uuid = k.call_uuid
		JOIN calls c ON c.call_uuid = k.call_uuid
		WHERE k.score IS NOT NULL AND k.counted_in_score AND %s
	), links AS (
		SELECT chain.*,
		       lag(call_uuid) OVER w AS prev_call, lag(score) OVER w AS prev_score,
		       lag(item_id) OVER w AS prev_item, lag(occurred_at) OVER w AS prev_at,
		       count(*) FILTER (WHERE score >= %[2]d) OVER w AS passes,
		       count(*) FILTER (WHERE score < %[2]d) OVER w AS fails,
		       row_number() OVER (PARTITION BY criterion_key ORDER BY occurred_at DESC, call_uuid DESC) AS latest
		FROM chain
		WINDOW w AS (PARTITION BY criterion_key ORDER BY occurred_at, call_uuid)
	)
	SELECT links.*,
	       count(*) FILTER (WHERE score < %[2]d) OVER (PARTITION BY criterion_key, passes ORDER BY occurred_at, call_uuid) AS fail_run,
	       min(occurred_at) FILTER (WHERE score < %[2]d) OVER (PARTITION BY criterion_key, passes) AS fail_run_from,
	       count(*) FILTER (WHERE score >= %[2]d) OVER (PARTITION BY criterion_key, fails ORDER BY occurred_at, call_uuid) AS pass_run
	FROM links`, where, passScore)
}

// CallProgress is the work-on-mistakes block of one call.
func (s *Service) CallProgress(ctx context.Context, viewer, callID uuid.UUID) (CallProgress, error) {
	var (
		company, department, uploader uuid.NullUUID
		occurredAt                    sql.NullTime
		shared, internal              bool
	)
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT c.company_uuid, c.department_uuid, c.uploaded_by_user_uuid, f.occurred_at,
		       COALESCE(f.is_shared, st.is_shared, false), COALESCE(f.is_internal, st.is_internal, false)
		FROM calls c
		LEFT JOIN analytics_call_facts f ON f.call_uuid = c.call_uuid
		LEFT JOIN call_subject_states st ON st.call_uuid = c.call_uuid
		WHERE c.call_uuid = $1 AND %s`, callRepo.VisibleToUserCondition("c", "$2")), callID, viewer).
		Scan(&company, &department, &uploader, &occurredAt, &shared, &internal)
	if errors.Is(err, sql.ErrNoRows) {
		return CallProgress{}, ErrNotFound
	}
	if err != nil {
		return CallProgress{}, fmt.Errorf("read call for progress: %w", err)
	}
	subjects := []uuid.UUID{}
	if company.Valid {
		rows, err := s.db.QueryContext(ctx, `SELECT user_uuid FROM call_subjects WHERE call_uuid = $1 ORDER BY is_primary DESC, user_uuid`, callID)
		if err != nil {
			return CallProgress{}, fmt.Errorf("read call subjects: %w", err)
		}
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				subjects = append(subjects, id)
			}
		}
		_ = rows.Close()
	} else if uploader.Valid {
		subjects = append(subjects, uploader.UUID)
	}
	if err := s.progressAccess(ctx, viewer, company, department, subjects); err != nil {
		return CallProgress{}, err
	}

	out := CallProgress{Criteria: []ProgressCriterion{}, GrowthAreas: []CallGrowthArea{}}
	unavailable := func(reason string) (CallProgress, error) {
		out.UnavailableReason = &reason
		return out, nil
	}
	switch {
	case shared:
		return unavailable(ProgressSharedCall)
	case internal:
		return unavailable(ProgressInternalCall)
	case !occurredAt.Valid || len(subjects) == 0:
		return unavailable(ProgressNoAnalysis)
	}
	subject := subjects[0]
	names, err := s.people(ctx, Scope{CompanyID: company.UUID}, []uuid.UUID{subject})
	if err != nil {
		return CallProgress{}, err
	}
	out.Employee = &ProgressEmployee{UserUUID: subject.String(), FullName: names[subject].name}

	q := &query{}
	chainOwner{company: company, user: subject}.chainCalls(q)
	call, at := q.arg(callID), q.arg(occurredAt.Time)
	q.add(fmt.Sprintf("(f.occurred_at, k.call_uuid) <= (%s::timestamptz, %s::uuid)", at, call))
	q.add(fmt.Sprintf("k.criterion_key IN (SELECT criterion_key FROM analytics_criterion_facts WHERE call_uuid = %s)", call))
	viewerArg := q.arg(viewer)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT r.criterion_key, r.item_id, r.score, r.prev_call, r.prev_score, r.prev_item, r.prev_at, r.fail_run,
		       r.prev_call IS NOT NULL AND EXISTS (SELECT 1 FROM calls c WHERE c.call_uuid = r.prev_call AND %s)
		FROM (%s) r
		WHERE r.call_uuid = %s`, callRepo.VisibleToUserCondition("c", viewerArg), chainRows(q.sql()), call), q.args...)
	if err != nil {
		return CallProgress{}, fmt.Errorf("read call progress: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keys := []uuid.UUID{}
	for rows.Next() {
		var (
			key            uuid.UUID
			item           string
			score, failRun int
			prevCall       uuid.NullUUID
			prevScore      sql.NullInt64
			prevItem       sql.NullString
			prevAt         sql.NullTime
			canOpen        bool
		)
		if err := rows.Scan(&key, &item, &score, &prevCall, &prevScore, &prevItem, &prevAt, &failRun, &canOpen); err != nil {
			return CallProgress{}, fmt.Errorf("scan call progress: %w", err)
		}
		row := ProgressCriterion{CriterionKey: key.String(), Current: ProgressScore{Score: score, ItemID: item}}
		if score < passScore {
			row.RepeatStreak = failRun
		}
		if prevCall.Valid {
			row.Previous = &ProgressPrevious{CallUUID: prevCall.UUID.String(), OccurredAt: prevAt.Time, Score: int(prevScore.Int64), ItemID: prevItem.String, CanOpen: canOpen}
		}
		row.Verdict = verdict(row.Previous, score)
		out.Counts.add(row.Verdict)
		out.Criteria = append(out.Criteria, row)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return CallProgress{}, err
	}
	if out.GrowthAreas, err = s.callGrowth(ctx, callID, out.Employee.FullName); err != nil {
		return CallProgress{}, err
	}
	if len(out.Criteria) == 0 {
		// Nothing scored by a scorecard: questions and episodes are not a chain,
		// though growth areas cover exactly such calls.
		if len(out.GrowthAreas) > 0 {
			out.Available = true
			return out, nil
		}
		return unavailable(ProgressNoFixedScorecard)
	}
	info, err := s.criterionInfo(ctx, keys)
	if err != nil {
		return CallProgress{}, err
	}
	for i := range out.Criteria {
		key, _ := uuid.Parse(out.Criteria[i].CriterionKey)
		out.Criteria[i].Title = info[key].title
	}
	sortProgress(out.Criteria)
	out.Available = true
	return out, nil
}

func verdict(previous *ProgressPrevious, score int) string {
	switch {
	case previous == nil:
		return VerdictFirstTime
	case previous.Score < passScore && score >= passScore:
		return VerdictFixed
	case previous.Score < passScore:
		return VerdictRepeated
	case score < passScore:
		return VerdictNew
	}
	return VerdictHolding
}

func (c *ProgressCounts) add(verdict string) {
	switch verdict {
	case VerdictFixed:
		c.Fixed++
	case VerdictRepeated:
		c.Repeated++
	case VerdictNew:
		c.New++
	case VerdictHolding:
		c.Holding++
	default:
		c.FirstTime++
	}
}

// sortProgress puts what needs attention first: repeated, new, fixed, first
// time, holding.
func sortProgress(rows []ProgressCriterion) {
	rank := map[string]int{VerdictRepeated: 0, VerdictNew: 1, VerdictFixed: 2, VerdictFirstTime: 3, VerdictHolding: 4}
	sort.SliceStable(rows, func(i, j int) bool {
		if rank[rows[i].Verdict] != rank[rows[j].Verdict] {
			return rank[rows[i].Verdict] < rank[rows[j].Verdict]
		}
		return rows[i].Title < rows[j].Title
	})
}

// progressAccess lets a subject of the call see their own progress, and the
// owner, the deputy and the leader of the call's department see an employee's.
// Seeing someone else's progress is team analytics and follows its plan flag;
// a personal account's own progress follows the personal one.
func (s *Service) progressAccess(ctx context.Context, viewer uuid.UUID, company, department uuid.NullUUID, subjects []uuid.UUID) error {
	own := false
	for _, id := range subjects {
		own = own || id == viewer
	}
	if !company.Valid {
		if !own {
			return ErrForbidden
		}
		flags, err := s.plan(ctx, `s.type = 'personal' AND s.user_uuid = $1`, viewer)
		if err != nil {
			return err
		}
		if !flags.personal {
			return ErrPersonalProgressDenied
		}
		return nil
	}
	if own {
		return nil
	}
	var manages bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM company_members WHERE company_uuid = $1 AND user_uuid = $3 AND status = 'active' AND role IN ('company_manager','company_deputy'))
		    OR EXISTS (SELECT 1 FROM department_members WHERE $2::uuid IS NOT NULL AND department_uuid = $2 AND user_uuid = $3 AND status = 'active' AND role = 'department_leader')`,
		company.UUID, department, viewer).Scan(&manages); err != nil {
		return fmt.Errorf("check progress rights: %w", err)
	}
	if !manages {
		return ErrForbidden
	}
	flags, err := s.plan(ctx, `s.type = 'business' AND s.user_uuid IN (SELECT manager_user_uuid FROM companies WHERE company_uuid = $1 AND deleted_at IS NULL)`, company.UUID)
	if err != nil {
		return err
	}
	if !flags.team {
		return ErrTeamAnalyticsDenied
	}
	return nil
}

// EmployeeProgress is the work on mistakes of one employee: criteria still
// failing now, and those closed during the period by three passing calls in a
// row. target is the employee, or uuid.Nil for the viewer.
func (s *Service) EmployeeProgress(ctx context.Context, req Request, target uuid.UUID) (EmployeeProgress, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return EmployeeProgress{}, err
	}
	if target, err = s.profileTarget(ctx, scope, target); err != nil {
		return EmployeeProgress{}, err
	}
	owner := chainOwner{user: target}
	if scope.Kind == "company" {
		owner.company = uuid.NullUUID{UUID: scope.CompanyID, Valid: true}
	}
	q := &query{}
	owner.chainCalls(q)
	viewer := q.arg(scope.UserID)
	from, to := q.arg(scope.Period.From), q.arg(scope.Period.To)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		WITH r AS (%s),
		open_rows AS (SELECT * FROM r WHERE latest = 1 AND score < %d),
		closed_rows AS (
			SELECT DISTINCT ON (criterion_key) criterion_key, occurred_at
			FROM r
			WHERE score >= %d AND pass_run = %d AND fails > 0 AND occurred_at >= %s AND occurred_at < %s
			ORDER BY criterion_key, occurred_at DESC)
		SELECT 'open', o.criterion_key, o.score::int, o.fail_run::int, o.fail_run_from, o.call_uuid,
		       EXISTS (SELECT 1 FROM calls c WHERE c.call_uuid = o.call_uuid AND %s)
		FROM open_rows o
		UNION ALL
		SELECT 'closed', cl.criterion_key, 0, 0, cl.occurred_at, NULL::uuid, false
		FROM closed_rows cl WHERE NOT EXISTS (SELECT 1 FROM open_rows o WHERE o.criterion_key = cl.criterion_key)`,
		chainRows(q.sql()), passScore, passScore, closingStreak, from, to, callRepo.VisibleToUserCondition("c", viewer)), q.args...)
	if err != nil {
		return EmployeeProgress{}, fmt.Errorf("read employee progress: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := EmployeeProgress{Open: []OpenMistake{}, ClosedInPeriod: []ClosedMistake{}, GrowthAreas: []EmployeeGrowthArea{}}
	keys := []uuid.UUID{}
	for rows.Next() {
		var (
			kind          string
			key           uuid.UUID
			score, streak int
			at            time.Time
			call          uuid.NullUUID
			canOpen       bool
		)
		if err := rows.Scan(&kind, &key, &score, &streak, &at, &call, &canOpen); err != nil {
			return EmployeeProgress{}, fmt.Errorf("scan employee progress: %w", err)
		}
		keys = append(keys, key)
		if kind == "closed" {
			out.ClosedInPeriod = append(out.ClosedInPeriod, ClosedMistake{CriterionKey: key.String(), ClosedAt: at.In(scope.Location)})
			continue
		}
		row := OpenMistake{CriterionKey: key.String(), LastScore: score, RepeatStreak: streak, FirstFailedAt: at.In(scope.Location)}
		if canOpen {
			// A call the viewer may not open is not linked.
			row.LastCallUUID = call.UUID.String()
		}
		out.Open = append(out.Open, row)
	}
	if err := rows.Err(); err != nil {
		return EmployeeProgress{}, err
	}
	info, err := s.criterionInfo(ctx, keys)
	if err != nil {
		return EmployeeProgress{}, err
	}
	for i := range out.Open {
		key, _ := uuid.Parse(out.Open[i].CriterionKey)
		out.Open[i].Title = info[key].title
	}
	for i := range out.ClosedInPeriod {
		key, _ := uuid.Parse(out.ClosedInPeriod[i].CriterionKey)
		out.ClosedInPeriod[i].Title = info[key].title
	}
	// The longest failing runs first: they are the habits.
	sort.SliceStable(out.Open, func(i, j int) bool {
		if out.Open[i].RepeatStreak != out.Open[j].RepeatStreak {
			return out.Open[i].RepeatStreak > out.Open[j].RepeatStreak
		}
		return out.Open[i].Title < out.Open[j].Title
	})
	sort.SliceStable(out.ClosedInPeriod, func(i, j int) bool { return out.ClosedInPeriod[i].ClosedAt.After(out.ClosedInPeriod[j].ClosedAt) })
	if out.GrowthAreas, err = s.employeeGrowth(ctx, scope, target, []string{"open"}); err != nil {
		return EmployeeProgress{}, err
	}
	return out, nil
}
