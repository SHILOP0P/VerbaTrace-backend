package teamanalytics

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type InstructionRef struct {
	UUID    string `json:"uuid"`
	Title   string `json:"title"`
	Deleted bool   `json:"deleted"`
}

type Distribution struct {
	Met          int `json:"met"`
	MostlyMet    int `json:"mostly_met"`
	PartiallyMet int `json:"partially_met"`
	MinimallyMet int `json:"minimally_met"`
	Missed       int `json:"missed"`
}

type CriterionRow struct {
	CriterionKey   string         `json:"criterion_key"`
	Title          string         `json:"title"`
	Instruction    InstructionRef `json:"instruction"`
	Weight         int            `json:"weight"`
	IsCritical     bool           `json:"is_critical"`
	NScored        int            `json:"n_scored"`
	NNotApplicable int            `json:"n_not_applicable"`
	NUnassessed    int            `json:"n_unassessed"`
	AvgScore       *int           `json:"avg_score"`
	PassRate       *float64       `json:"pass_rate"`
	Delta          Delta          `json:"delta"`
	Sample         string         `json:"sample"`
	Distribution   Distribution   `json:"distribution"`
	Trend          []TrendPoint   `json:"trend"`
	SortRank       int            `json:"sort_rank"`
	mean           *float64
	passed         int
}

type CriteriaView struct {
	Period   PeriodView     `json:"period"`
	Criteria []CriterionRow `json:"criteria"`
	Total    int            `json:"total"`
}

// Effective status of a fact: the reviewer's score falls into the same five
// bands the model uses.
const effectiveStatus = `CASE
	WHEN cf.human_decision = 'not_applicable' THEN 'not_applicable'
	WHEN cf.human_score IS NOT NULL THEN CASE WHEN cf.human_score >= 88 THEN 'met' WHEN cf.human_score >= 63 THEN 'mostly_met'
		WHEN cf.human_score >= 38 THEN 'partially_met' WHEN cf.human_score >= 13 THEN 'minimally_met' ELSE 'missed' END
	ELSE cf.ai_status END`

type criterionStats struct {
	stat
	na, unassessed, passed int
	dist                   Distribution
	scorecards             map[string]bool
}

func (s *Service) criterionStats(ctx context.Context, scope Scope, from, to time.Time, narrow func(q *query)) (map[uuid.UUID]*criterionStats, error) {
	q := &query{}
	scope.callsOf(q, from, to)
	if narrow != nil {
		narrow(q)
	}
	if scope.Instruction.Valid {
		q.add(fmt.Sprintf("cf.instruction_uuid = %s", q.arg(scope.Instruction.UUID)))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cf.criterion_key, count(cf.score), avg(cf.score)::float8, COALESCE(var_samp(cf.score), 0)::float8,
		       count(*) FILTER (WHERE `+effectiveStatus+` = 'not_applicable'),
		       count(*) FILTER (WHERE cf.score IS NULL AND `+effectiveStatus+` IN ('unclear','conflict','not_assessed')),
		       count(*) FILTER (WHERE cf.score >= 75),
		       count(*) FILTER (WHERE cf.score IS NOT NULL AND `+effectiveStatus+` = 'met'),
		       count(*) FILTER (WHERE cf.score IS NOT NULL AND `+effectiveStatus+` = 'mostly_met'),
		       count(*) FILTER (WHERE cf.score IS NOT NULL AND `+effectiveStatus+` = 'partially_met'),
		       count(*) FILTER (WHERE cf.score IS NOT NULL AND `+effectiveStatus+` = 'minimally_met'),
		       count(*) FILTER (WHERE cf.score IS NOT NULL AND `+effectiveStatus+` = 'missed'),
		       string_agg(DISTINCT cf.scorecard_uuid::text, ',')
		FROM analytics_criterion_facts cf
		JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid JOIN calls c ON c.call_uuid = f.call_uuid
		WHERE `+q.sql()+`
		GROUP BY cf.criterion_key`, q.args...)
	if err != nil {
		return nil, fmt.Errorf("read criterion stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := map[uuid.UUID]*criterionStats{}
	for rows.Next() {
		var key uuid.UUID
		var st criterionStats
		var mean sql.NullFloat64
		var cards sql.NullString
		if err := rows.Scan(&key, &st.n, &mean, &st.variance, &st.na, &st.unassessed, &st.passed,
			&st.dist.Met, &st.dist.MostlyMet, &st.dist.PartiallyMet, &st.dist.MinimallyMet, &st.dist.Missed, &cards); err != nil {
			return nil, err
		}
		if mean.Valid {
			st.mean = &mean.Float64
		}
		st.scorecards = map[string]bool{}
		for _, id := range strings.Split(cards.String, ",") {
			if id != "" {
				st.scorecards[id] = true
			}
		}
		copied := st
		result[key] = &copied
	}
	return result, rows.Err()
}

// Criteria is the table of criteria, weakest first among those with enough data.
func (s *Service) Criteria(ctx context.Context, req Request, sortBy, order string) (CriteriaView, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return CriteriaView{}, err
	}
	if err := scope.requireTeam(); err != nil {
		return CriteriaView{}, err
	}
	current, err := s.criterionStats(ctx, scope, scope.Period.From, scope.Period.To, nil)
	if err != nil {
		return CriteriaView{}, err
	}
	previous, err := s.criterionStats(ctx, scope, scope.Period.PreviousFrom, scope.Period.PreviousTo, nil)
	if err != nil {
		return CriteriaView{}, err
	}
	keys := make([]uuid.UUID, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	info, err := s.criterionInfo(ctx, keys)
	if err != nil {
		return CriteriaView{}, err
	}
	trends, err := s.criterionTrends(ctx, scope, keys)
	if err != nil {
		return CriteriaView{}, err
	}
	var teamSum float64
	var teamN int
	for _, st := range current {
		if st.mean != nil {
			teamSum += *st.mean * float64(st.n)
			teamN += st.n
		}
	}
	teamMean := 0.0
	if teamN > 0 {
		teamMean = teamSum / float64(teamN)
	}
	rows := make([]CriterionRow, 0, len(keys))
	for _, key := range keys {
		st := current[key]
		meta := info[key]
		row := CriterionRow{CriterionKey: key.String(), Title: meta.title, Instruction: meta.instruction, Weight: meta.weight, IsCritical: meta.critical,
			NScored: st.n, NNotApplicable: st.na, NUnassessed: st.unassessed, AvgScore: shown(st.mean, st.n), Sample: sample(st.n),
			Distribution: st.dist, Trend: trends[key], mean: st.mean, passed: st.passed}
		if row.Trend == nil {
			row.Trend = []TrendPoint{}
		}
		if st.n >= minSample {
			rate := float64(st.passed) / float64(st.n)
			row.PassRate = &rate
		}
		prev := previous[key]
		if prev == nil {
			prev = &criterionStats{}
		}
		row.Delta = delta(st.stat, prev.stat, prev.n > 0 && !sameSet(st.scorecards, prev.scorecards))
		rows = append(rows, row)
	}
	// The default order ranks by the smoothed mean, weakest first; criteria
	// with too little data follow.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.NScored >= minSample) != (b.NScored >= minSample) {
			return a.NScored >= minSample
		}
		return smoothed(a.mean, a.NScored, teamMean) < smoothed(b.mean, b.NScored, teamMean)
	})
	for i := range rows {
		rows[i].SortRank = i + 1
	}
	sortCriteria(rows, sortBy, order)
	return CriteriaView{Period: scope.periodView(), Criteria: rows, Total: len(rows)}, nil
}

func sortCriteria(rows []CriterionRow, sortBy, order string) {
	desc := order == "desc"
	less := map[string]func(a, b CriterionRow) bool{
		"avg_score": func(a, b CriterionRow) bool { return value(a.AvgScore) < value(b.AvgScore) },
		"pass_rate": func(a, b CriterionRow) bool {
			return wilsonLower(a.passed, a.NScored) < wilsonLower(b.passed, b.NScored)
		},
		"delta":    func(a, b CriterionRow) bool { return value(a.Delta.Value) < value(b.Delta.Value) },
		"n_scored": func(a, b CriterionRow) bool { return a.NScored < b.NScored },
		"title":    func(a, b CriterionRow) bool { return a.Title < b.Title },
	}[sortBy]
	if less == nil {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}

func value(v *int) int {
	if v == nil {
		return -1
	}
	return *v
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func (s *Service) criterionTrends(ctx context.Context, scope Scope, keys []uuid.UUID) (map[uuid.UUID][]TrendPoint, error) {
	result := map[uuid.UUID][]TrendPoint{}
	if len(keys) == 0 {
		return result, nil
	}
	q := &query{}
	scope.callsOf(q, scope.Period.From, scope.Period.To)
	q.add(fmt.Sprintf("cf.criterion_key = ANY(%s::uuid[])", q.arg(uuidStrings(keys))))
	bucket := bucketExpr(q, scope, "f.occurred_at")
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT cf.criterion_key, %s, avg(cf.score)::float8, count(cf.score)
		FROM analytics_criterion_facts cf JOIN analytics_call_facts f ON f.call_uuid = cf.call_uuid JOIN calls c ON c.call_uuid = f.call_uuid
		WHERE %s GROUP BY 1, 2 ORDER BY 1, 2`, bucket, q.sql()), q.args...)
	if err != nil {
		return nil, fmt.Errorf("read criterion trends: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key uuid.UUID
		var point TrendPoint
		var mean sql.NullFloat64
		if err := rows.Scan(&key, &point.Bucket, &mean, &point.N); err != nil {
			return nil, err
		}
		if mean.Valid && point.N > 0 {
			point.Avg = intPtr(int(mean.Float64 + 0.5))
		}
		result[key] = append(result[key], point)
	}
	return result, rows.Err()
}

type criterionMeta struct {
	title       string
	weight      int
	critical    bool
	instruction InstructionRef
}

// criterionInfo names criteria by their newest wording. Facts are stored under
// the canonical key, and a newer version may carry the criterion under an alias.
func (s *Service) criterionInfo(ctx context.Context, keys []uuid.UUID) (map[uuid.UUID]criterionMeta, error) {
	result := map[uuid.UUID]criterionMeta{}
	if len(keys) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH wanted AS (SELECT unnest($1::uuid[]) AS canonical_key),
		     forms AS (
		         SELECT canonical_key, canonical_key AS key FROM wanted
		         UNION SELECT a.canonical_key, a.alias_key FROM criterion_key_aliases a JOIN wanted w ON w.canonical_key = a.canonical_key)
		SELECT DISTINCT ON (fm.canonical_key) fm.canonical_key, c.title, c.weight, c.is_critical, i.instruction_uuid, i.title, i.deleted_at IS NOT NULL
		FROM forms fm
		JOIN instruction_scorecard_criteria c ON c.criterion_key = fm.key
		JOIN instruction_scorecards sc ON sc.scorecard_uuid = c.scorecard_uuid
		JOIN analysis_instructions i ON i.instruction_uuid = sc.instruction_uuid
		ORDER BY fm.canonical_key, sc.is_current DESC, sc.created_at DESC`, uuidStrings(keys))
	if err != nil {
		return nil, fmt.Errorf("read criterion titles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, instruction uuid.UUID
		var meta criterionMeta
		if err := rows.Scan(&key, &meta.title, &meta.weight, &meta.critical, &instruction, &meta.instruction.Title, &meta.instruction.Deleted); err != nil {
			return nil, err
		}
		meta.instruction.UUID = instruction.String()
		result[key] = meta
	}
	return result, rows.Err()
}

// canonical maps criterion keys, as a current scorecard names them, to the keys
// the facts are stored under.
func (s *Service) canonical(ctx context.Context, keys []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	result := map[uuid.UUID]uuid.UUID{}
	for _, key := range keys {
		result[key] = key
	}
	if len(keys) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT alias_key, canonical_key FROM criterion_key_aliases WHERE alias_key = ANY($1::uuid[])`, uuidStrings(keys))
	if err != nil {
		return nil, fmt.Errorf("read aliases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var alias, target uuid.UUID
		if rows.Scan(&alias, &target) == nil {
			result[alias] = target
		}
	}
	return result, rows.Err()
}
