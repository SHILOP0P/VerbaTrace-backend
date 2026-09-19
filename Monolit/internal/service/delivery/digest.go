package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/teamanalytics"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const defaultTimezone = "Europe/Moscow"

// Digests builds the weekly digest from the analytics pages' own numbers: the
// same access rules and the same facts, put into a template. No model is
// called.
type Digests struct {
	service   *Service
	analytics *teamanalytics.Service
}

func NewDigests(service *Service, analytics *teamanalytics.Service) *Digests {
	return &Digests{service: service, analytics: analytics}
}

// Run looks every half hour for people whose Monday morning has come.
func (d *Digests) Run(ctx context.Context, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			d.RunOnce(ctx, d.service.now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

// RunOnce sends the digests due at now: to everyone for whom it is Monday,
// 09:00–09:59, in their time zone. A digest already sent for the week is not
// sent again, so the run may repeat within the hour.
func (d *Digests) RunOnce(ctx context.Context, now time.Time) int {
	zones, err := d.dueZones(ctx, now)
	if err != nil {
		d.service.log.Warn(ctx, "weekly digest zones not read", zap.Error(err))
		return 0
	}
	sent := 0
	for zone, location := range zones {
		users, err := d.usersIn(ctx, zone)
		if err != nil {
			d.service.log.Warn(ctx, "weekly digest users not read", zap.Error(err))
			continue
		}
		week := previousWeek(now.In(location))
		for _, user := range users {
			sent += d.forUser(ctx, user, week, location)
		}
	}
	return sent
}

// dueZones are the time zones where it is now Monday between nine and ten.
func (d *Digests) dueZones(ctx context.Context, now time.Time) (map[string]*time.Location, error) {
	rows, err := d.service.db.QueryContext(ctx, `SELECT DISTINCT COALESCE(NULLIF(timezone, ''), $1) FROM user_profiles UNION SELECT $1`, defaultTimezone)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	due := map[string]*time.Location{}
	for rows.Next() {
		var zone string
		if rows.Scan(&zone) != nil {
			continue
		}
		location, err := time.LoadLocation(zone)
		if err != nil {
			continue
		}
		local := now.In(location)
		if local.Weekday() == time.Monday && local.Hour() == 9 {
			due[zone] = location
		}
	}
	return due, rows.Err()
}

func (d *Digests) usersIn(ctx context.Context, zone string) ([]uuid.UUID, error) {
	rows, err := d.service.db.QueryContext(ctx, `
		SELECT u.user_uuid FROM users u LEFT JOIN user_profiles p ON p.user_uuid = u.user_uuid
		WHERE COALESCE(NULLIF(p.timezone, ''), $2) = $1`, zone, defaultTimezone)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var users []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			users = append(users, id)
		}
	}
	return users, rows.Err()
}

type week struct {
	from, to time.Time
	label    string
}

// previousWeek is the ISO week before the one local time is in.
func previousWeek(local time.Time) week {
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	from := monday.AddDate(0, 0, -7)
	year, number := from.ISOWeek()
	return week{from: from.UTC(), to: monday.UTC(), label: fmt.Sprintf("%d-W%02d", year, number)}
}

// forUser sends one digest per place the person works in: each company, and
// their personal account when it has calls.
func (d *Digests) forUser(ctx context.Context, user uuid.UUID, w week, location *time.Location) int {
	rows, err := d.service.db.QueryContext(ctx, `
		SELECT m.company_uuid FROM company_members m JOIN companies c ON c.company_uuid = m.company_uuid
		WHERE m.user_uuid = $1 AND m.status = 'active' AND c.lifecycle_state = 'active' AND c.deleted_at IS NULL`, user)
	if err != nil {
		d.service.log.Warn(ctx, "weekly digest companies not read", zap.Error(err))
		return 0
	}
	var companies []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			companies = append(companies, id)
		}
	}
	_ = rows.Close()
	sent := 0
	for _, company := range companies {
		if d.send(ctx, user, uuid.NullUUID{UUID: company, Valid: true}, w, location) {
			sent++
		}
	}
	if d.send(ctx, user, uuid.NullUUID{}, w, location) {
		sent++
	}
	return sent
}

func (d *Digests) send(ctx context.Context, user uuid.UUID, company uuid.NullUUID, w week, location *time.Location) bool {
	digest, ok, err := d.build(ctx, user, company, w)
	if err != nil {
		d.service.log.Warn(ctx, "weekly digest not built", zap.String("user_id", user.String()), zap.Error(err))
		return false
	}
	if !ok {
		// An empty week sends nothing.
		return false
	}
	place := "personal"
	event := Event{Kind: KindWeeklyDigest, User: user, Notification: models.NotificationTypeWeeklyDigestReady,
		Title: "Итоги недели", Body: digest.line, Company: company, Text: digest.text(d.service, w, location)}
	if company.Valid {
		place = company.UUID.String()
		event.EntityType, event.EntityID = "company", company
	}
	delivered, err := d.service.Deliver(ctx, "digest:"+user.String()+":"+place+":"+w.label, []Event{event})
	if err != nil {
		d.service.log.Warn(ctx, "weekly digest not delivered", zap.String("user_id", user.String()), zap.Error(err))
	}
	return delivered
}

type digest struct {
	line     string
	lines    []string
	calls    []string
	analysis string
}

func (g digest) text(s *Service, w week, location *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Итоги недели %s — %s\n", w.from.In(location).Format("02.01"), w.to.In(location).AddDate(0, 0, -1).Format("02.01"))
	b.WriteString(g.line + "\n")
	for _, line := range g.lines {
		b.WriteString(line + "\n")
	}
	if len(g.calls) > 0 {
		b.WriteString("Стоит послушать:\n")
		for _, id := range g.calls {
			b.WriteString(s.Link("/app/calls?call="+id) + "\n")
		}
	}
	b.WriteString("Подробнее: " + s.Link("/app/analytics"+g.analysis))
	return b.String()
}

// build assembles a digest: the team's for the owner, the deputy and a
// department leader, one's own numbers and work on mistakes for everyone else.
func (d *Digests) build(ctx context.Context, user uuid.UUID, company uuid.NullUUID, w week) (digest, bool, error) {
	from, to := w.from, w.to
	req := teamanalytics.Request{UserID: user, CompanyID: company, Personal: !company.Valid, From: &from, To: &to}
	capabilities, err := d.analytics.Capabilities(ctx, req)
	if err != nil {
		return digest{}, false, err
	}
	if capabilities.OwnProfileOnly || !capabilities.CanViewEmployees {
		return d.own(ctx, req, company, w)
	}
	return d.team(ctx, req)
}

func (d *Digests) team(ctx context.Context, req teamanalytics.Request) (digest, bool, error) {
	summary, err := d.analytics.Summary(ctx, req)
	if errors.Is(err, teamanalytics.ErrTeamAnalyticsDenied) {
		return digest{}, false, nil
	}
	if err != nil || summary.CallsAnalyzed == 0 {
		return digest{}, false, err
	}
	g := digest{line: headline(summary.CallsAnalyzed, summary.AvgScore, summary.Delta.Value)}
	if criteria, err := d.analytics.Criteria(ctx, req, "", ""); err == nil {
		var weak []string
		for _, row := range criteria.Criteria {
			if len(weak) == 3 {
				break
			}
			if row.AvgScore != nil {
				weak = append(weak, fmt.Sprintf("%s (%d)", row.Title, *row.AvgScore))
			}
		}
		if len(weak) > 0 {
			g.lines = append(g.lines, "Слабые критерии: "+strings.Join(weak, "; "))
		}
	}
	if employees, err := d.analytics.Employees(ctx, req); err == nil {
		moved := make([]teamanalytics.EmployeeRow, 0, len(employees.Employees))
		for _, row := range employees.Employees {
			if row.Delta.Value != nil {
				moved = append(moved, row)
			}
		}
		sort.SliceStable(moved, func(i, j int) bool { return *moved[i].Delta.Value > *moved[j].Delta.Value })
		if len(moved) > 0 {
			if first := moved[0]; *first.Delta.Value > 0 {
				g.lines = append(g.lines, fmt.Sprintf("Сильнее всех вырос: %s (%s)", first.FullName, signed(*first.Delta.Value)))
			}
			if last := moved[len(moved)-1]; *last.Delta.Value < 0 {
				g.lines = append(g.lines, fmt.Sprintf("Сильнее всех упал: %s (%s)", last.FullName, signed(*last.Delta.Value)))
			}
		}
	}
	if summary.CriticalMissed > 0 {
		g.lines = append(g.lines, fmt.Sprintf("Критичных пропусков: %d", summary.CriticalMissed))
	}
	for _, call := range summary.WorthListening {
		if len(g.calls) == 3 {
			break
		}
		if call.CanOpen {
			g.calls = append(g.calls, call.CallUUID)
		}
	}
	return g, true, nil
}

func (d *Digests) own(ctx context.Context, req teamanalytics.Request, company uuid.NullUUID, w week) (digest, bool, error) {
	profile, err := d.analytics.Profile(ctx, req, uuid.Nil)
	if errors.Is(err, teamanalytics.ErrPersonalProgressDenied) {
		return digest{}, false, nil
	}
	if err != nil || profile.Totals.Calls == 0 {
		return digest{}, false, err
	}
	g := digest{line: headline(profile.Totals.Calls, profile.Totals.AvgScore, profile.Totals.Delta.Value), analysis: "/employees/me"}
	if counts, err := d.analytics.WeekVerdicts(ctx, company, req.UserID, w.from, w.to); err == nil && counts.Fixed+counts.Repeated+counts.New > 0 {
		g.lines = append(g.lines, fmt.Sprintf("Исправлено %d, повторилось %d, новых ошибок %d", counts.Fixed, counts.Repeated, counts.New))
	}
	if progress, err := d.analytics.EmployeeProgress(ctx, req, uuid.Nil); err == nil && len(progress.Open) > 0 {
		titles := make([]string, 0, 3)
		for _, row := range progress.Open {
			if len(titles) == 3 {
				break
			}
			titles = append(titles, row.Title)
		}
		g.lines = append(g.lines, fmt.Sprintf("Открытых ошибок: %d (%s)", len(progress.Open), strings.Join(titles, "; ")))
	}
	return g, true, nil
}

// headline is the bell's line: "42 звонка, средний балл 71, +3 к прошлой неделе".
func headline(calls int, avg, delta *int) string {
	line := fmt.Sprintf("%d %s", calls, plural(calls, "звонок", "звонка", "звонков"))
	if avg != nil {
		line += ", средний балл " + strconv.Itoa(*avg)
	}
	if delta != nil {
		line += ", " + signed(*delta) + " к прошлой неделе"
	}
	return line
}

func signed(v int) string {
	if v > 0 {
		return "+" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

func plural(n int, one, few, many string) string {
	tens, ones := n%100, n%10
	switch {
	case tens >= 11 && tens <= 14:
		return many
	case ones == 1:
		return one
	case ones >= 2 && ones <= 4:
		return few
	}
	return many
}
