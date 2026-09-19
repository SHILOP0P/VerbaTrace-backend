package teamanalytics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/analysistext"
	callRepo "verbatrace/monolit/internal/repository/call"

	"github.com/google/uuid"
)

// maxObservations bounds the feed of one area; the oldest calls say least
// about where the person is now.
const maxObservations = 20

// withName shows the employee in a note: the model marks them by speaker ID.
// Notes stored before reference IDs were cleaned on write lose them here.
func withName(note, name string) string {
	if name == "" {
		name = "сотрудник"
	}
	return analysistext.ResolveSpeakerMarkersFunc(readable(note), func(string) string { return name })
}

// readable takes the model's reference IDs out of growth text; the titles of
// the cards they cite are not at hand here, so the IDs simply go.
func readable(text string) string { return analysistext.StripReferenceIDs(text, nil) }

type GrowthAreasView struct {
	Areas []EmployeeGrowthArea `json:"areas"`
}

// EmployeeGrowthAreas lists an employee's growth areas with the calls they were
// seen in. Access is that of the employee's profile. status is open, resolved,
// dismissed or empty for every area that is not hidden.
func (s *Service) EmployeeGrowthAreas(ctx context.Context, req Request, target uuid.UUID, status string) (GrowthAreasView, error) {
	switch status {
	case "", "open", "resolved", "dismissed":
	default:
		return GrowthAreasView{}, ErrInvalidRequest
	}
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return GrowthAreasView{}, err
	}
	if target, err = s.profileTarget(ctx, scope, target); err != nil {
		return GrowthAreasView{}, err
	}
	statuses := []string{"open", "resolved"}
	if status != "" {
		statuses = []string{status}
	}
	areas, err := s.employeeGrowth(ctx, scope, target, statuses)
	return GrowthAreasView{Areas: areas}, err
}

func (s *Service) employeeGrowth(ctx context.Context, scope Scope, target uuid.UUID, statuses []string) ([]EmployeeGrowthArea, error) {
	company := uuid.NullUUID{}
	if scope.Kind == "company" {
		company = uuid.NullUUID{UUID: scope.CompanyID, Valid: true}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT area_uuid, title, description, status, occurrences, clean_streak, returned
		FROM growth_areas
		WHERE subject_user_uuid = $1 AND company_uuid IS NOT DISTINCT FROM $2 AND status = ANY($3::text[])
		ORDER BY status = 'open' DESC, occurrences DESC, last_seen_at DESC NULLS LAST, created_at`, target, company, statuses)
	if err != nil {
		return nil, fmt.Errorf("read growth areas: %w", err)
	}
	areas := []EmployeeGrowthArea{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var area EmployeeGrowthArea
		if err := rows.Scan(&id, &area.Title, &area.Description, &area.Status, &area.Occurrences, &area.CleanStreak, &area.Returned); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan growth area: %w", err)
		}
		area.AreaUUID, area.Observations = id.String(), []GrowthObservationView{}
		area.Title, area.Description = readable(area.Title), readable(area.Description)
		index[id] = len(areas)
		areas = append(areas, area)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil || len(areas) == 0 {
		return areas, err
	}
	names, err := s.people(ctx, scope, []uuid.UUID{target})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id.String())
	}
	rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT area_uuid, call_uuid, at, verdict, item_ids, note, can_open FROM (
			SELECT o.area_uuid, o.call_uuid, COALESCE(f.occurred_at, c.created_at) AS at, o.verdict, o.item_ids, o.note,
			       %s AS can_open,
			       row_number() OVER (PARTITION BY o.area_uuid ORDER BY COALESCE(f.occurred_at, c.created_at) DESC, o.call_uuid) AS n
			FROM growth_area_observations o
			JOIN calls c ON c.call_uuid = o.call_uuid
			LEFT JOIN analytics_call_facts f ON f.call_uuid = o.call_uuid
			WHERE o.area_uuid = ANY($1::uuid[])
		) r WHERE n <= %d ORDER BY at DESC`, callRepo.VisibleToUserCondition("c", "$2"), maxObservations), ids, scope.UserID)
	if err != nil {
		return nil, fmt.Errorf("read growth observations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			areaID, callID uuid.UUID
			at             time.Time
			view           GrowthObservationView
			items          stringArray
		)
		if err := rows.Scan(&areaID, &callID, &at, &view.Verdict, &items, &view.Note, &view.CanOpen); err != nil {
			return nil, fmt.Errorf("scan growth observation: %w", err)
		}
		view.OccurredAt, view.ItemIDs, view.Note = at.In(scope.Location), []string(items), withName(view.Note, names[target].name)
		if view.CanOpen {
			view.CallUUID = callID.String()
		}
		i := index[areaID]
		areas[i].Observations = append(areas[i].Observations, view)
	}
	return areas, rows.Err()
}

// callGrowth is what the analysis of one call said about the employee's growth
// areas.
func (s *Service) callGrowth(ctx context.Context, callID uuid.UUID, name string) ([]CallGrowthArea, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.area_uuid, a.title, o.verdict, CASE WHEN o.verdict = 'new' THEN a.description ELSE o.note END, o.item_ids
		FROM growth_area_observations o JOIN growth_areas a ON a.area_uuid = o.area_uuid
		WHERE o.call_uuid = $1 AND a.status <> 'dismissed'
		ORDER BY o.verdict = 'not_applicable', o.verdict = 'improved', a.title`, callID)
	if err != nil {
		return nil, fmt.Errorf("read call growth: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []CallGrowthArea{}
	for rows.Next() {
		var (
			id    uuid.UUID
			area  CallGrowthArea
			items stringArray
		)
		if err := rows.Scan(&id, &area.Title, &area.Verdict, &area.Note, &items); err != nil {
			return nil, fmt.Errorf("scan call growth: %w", err)
		}
		area.AreaUUID, area.ItemIDs, area.Title, area.Note = id.String(), []string(items), readable(area.Title), withName(area.Note, name)
		out = append(out, area)
	}
	return out, rows.Err()
}

// stringArray scans a PostgreSQL text[] in its text form.
type stringArray []string

func (a *stringArray) Scan(src any) error {
	var raw string
	switch v := src.(type) {
	case nil:
		*a = []string{}
		return nil
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("unsupported text[] value %T", src)
	}
	*a = parseTextArray(raw)
	return nil
}

var _ sql.Scanner = (*stringArray)(nil)

func parseTextArray(raw string) []string {
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "{"), "}")
	out := []string{}
	if raw == "" {
		return out
	}
	var current strings.Builder
	quoted, escaped := false, false
	for _, r := range raw {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case r == ',' && !quoted:
			out = append(out, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	return append(out, current.String())
}
