package teamanalytics

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// query collects the WHERE conditions and arguments of one analytics query.
type query struct {
	args  []any
	where []string
}

func (q *query) arg(value any) string {
	q.args = append(q.args, value)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *query) add(condition string) { q.where = append(q.where, condition) }

func (q *query) sql() string {
	if len(q.where) == 0 {
		return "true"
	}
	return strings.Join(q.where, " AND ")
}

// callsOf limits a query over facts f joined to calls c to what the scope may
// count. The company decides visibility for management; an employee and a
// personal account count only the calls they are a subject of. A call in the bin
// drops out, and internal calls stay out unless asked for.
func (s Scope) callsOf(q *query, from, to time.Time) {
	q.add("c.deleted_at IS NULL")
	q.add(fmt.Sprintf("f.occurred_at >= %s AND f.occurred_at < %s", q.arg(from), q.arg(to)))
	if s.Kind == "personal" {
		q.add(fmt.Sprintf("c.company_uuid IS NULL AND c.uploaded_by_user_uuid = %s", q.arg(s.UserID)))
	} else {
		q.add(fmt.Sprintf("c.company_uuid = %s", q.arg(s.CompanyID)))
		switch {
		case s.Department.Valid:
			q.add(fmt.Sprintf("c.department_uuid = %s", q.arg(s.Department.UUID)))
		case s.Role == roleLeader:
			q.add(fmt.Sprintf("c.department_uuid = ANY(%s::uuid[])", q.arg(uuidStrings(s.LedDepartments))))
		}
	}
	if s.Employee.Valid && s.Kind != "personal" {
		q.add(fmt.Sprintf("EXISTS (SELECT 1 FROM call_subjects cs0 WHERE cs0.call_uuid = c.call_uuid AND cs0.user_uuid = %s)", q.arg(s.Employee.UUID)))
	}
	if !s.IncludeInternal {
		q.add("NOT f.is_internal")
	}
	if s.ExcludeShared {
		q.add("NOT f.is_shared")
	}
	if s.Folder.Valid {
		q.add(fmt.Sprintf("EXISTS (SELECT 1 FROM call_folder_assignments fa WHERE fa.call_uuid = c.call_uuid AND fa.folder_uuid = %s)", q.arg(s.Folder.UUID)))
	}
	if s.Instruction.Valid {
		q.add(fmt.Sprintf("EXISTS (SELECT 1 FROM analytics_criterion_facts i0 WHERE i0.call_uuid = f.call_uuid AND i0.instruction_uuid = %s)", q.arg(s.Instruction.UUID)))
	}
}

// Block shapes of the API (spec, section 18).

type PeriodView struct {
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	PreviousFrom time.Time `json:"previous_from"`
	PreviousTo   time.Time `json:"previous_to"`
	Bucket       string    `json:"bucket"`
	Timezone     string    `json:"timezone"`
}

type Delta struct {
	Value           *int `json:"value"`
	Significant     bool `json:"significant"`
	Comparable      bool `json:"comparable"`
	CriteriaChanged bool `json:"criteria_changed"`
}

type TrendPoint struct {
	Bucket string `json:"bucket"`
	Avg    *int   `json:"avg"`
	N      int    `json:"n"`
}

func (s Scope) periodView() PeriodView {
	p := s.Period
	return PeriodView{From: p.From.In(s.Location), To: p.To.In(s.Location), PreviousFrom: p.PreviousFrom.In(s.Location),
		PreviousTo: p.PreviousTo.In(s.Location), Bucket: p.Bucket, Timezone: s.Timezone}
}

// sample names how far a mean can be trusted.
func sample(n int) string {
	switch {
	case n == 0:
		return "none"
	case n < minSample:
		return "low"
	case n < thinSample:
		return "thin"
	}
	return "ok"
}

// shown hides a mean of fewer than five scores: it says more about one call than
// about a person.
func shown(mean *float64, n int) *int {
	if mean == nil || n < minSample {
		return nil
	}
	v := int(math.Round(*mean))
	return &v
}

// stat is a mean with what a significance test needs.
type stat struct {
	n        int
	mean     *float64
	variance float64
}

// delta compares two periods. The arrow is coloured only when the change is
// larger than the noise of two independent means.
func delta(current, previous stat, criteriaChanged bool) Delta {
	d := Delta{Comparable: previous.n > 0 && current.n > 0, CriteriaChanged: criteriaChanged}
	if current.mean == nil || previous.mean == nil || current.n < minSample || previous.n < minSample {
		return d
	}
	change := *current.mean - *previous.mean
	v := int(math.Round(change))
	d.Value = &v
	noise := 1.96 * math.Sqrt(current.variance/float64(current.n)+previous.variance/float64(previous.n))
	d.Significant = math.Abs(change) > noise
	return d
}

// smoothed is the mean pulled towards the team mean in proportion to how few
// scores it rests on, so two bad calls do not outrank forty mediocre ones. It
// orders rows; the raw mean is what is shown.
func smoothed(mean *float64, n int, teamMean float64) float64 {
	if mean == nil {
		return teamMean
	}
	return (smoothingWeight*teamMean + *mean*float64(n)) / (smoothingWeight + float64(n))
}

// wilsonLower is the lower bound of a pass rate at 95%.
func wilsonLower(passed, n int) float64 {
	if n == 0 {
		return 0
	}
	z := 1.96
	p := float64(passed) / float64(n)
	denominator := 1 + z*z/float64(n)
	centre := p + z*z/(2*float64(n))
	margin := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n)*float64(n)))
	return (centre - margin) / denominator
}

// bucketExpr truncates occurred_at to the trend bucket in the viewer's zone.
func bucketExpr(q *query, s Scope, column string) string {
	return fmt.Sprintf("to_char(date_trunc(%s, %s, %s), 'YYYY-MM-DD')", q.arg(s.Period.Bucket), column, q.arg(s.Timezone))
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = id.String()
	}
	return result
}

func intPtr(v int) *int { return &v }

func nullUUID() uuid.NullUUID { return uuid.NullUUID{} }
