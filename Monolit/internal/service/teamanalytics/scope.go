// Package teamanalytics answers the analytics pages from the facts tables: the
// team's scores by criterion, employee and department, their trends and the
// calls behind every number. Who may see what follows the role in the company.
package teamanalytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/logger"

	"github.com/google/uuid"
)

var (
	ErrForbidden                           = errors.New("analytics forbidden")
	ErrTeamAnalyticsDenied                 = errors.New("team analytics access denied")
	ErrPersonalProgressDenied              = errors.New("personal progress access denied")
	ErrInvalidRequest                      = errors.New("invalid analytics request")
	ErrNotFound                            = errors.New("analytics subject not found")
	ErrSettingsVersionConflict             = errors.New("analytics settings changed")
	defaultTimezone                        = "Europe/Moscow"
	minSample, thinSample                  = 5, 20
	referenceMinPeople                     = 3
	smoothingWeight                        = 10.0
	maxMatrixCriteria                      = 30
	maxDrillDownLimit                      = 100
	worthListeningCount                    = 5
	roleManager, roleDeputy                = "company_manager", "company_deputy"
	roleLeader, roleEmployee, rolePersonal = "department_leader", "employee", "personal"
)

type Service struct {
	db  *sql.DB
	log logger.Logger
	now func() time.Time
}

func NewService(db *sql.DB, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, log: log, now: func() time.Time { return time.Now().UTC() }}
}

// Request is what every analytics route accepts. Filters a role may not use
// are replaced by the ones it is held to, not refused.
type Request struct {
	UserID          uuid.UUID
	CompanyID       uuid.NullUUID
	Personal        bool
	DepartmentID    uuid.NullUUID
	EmployeeID      uuid.NullUUID
	InstructionID   uuid.NullUUID
	FolderID        uuid.NullUUID
	From, To        *time.Time
	IncludeInternal bool
	ExcludeShared   bool
}

// Scope is a request resolved against the viewer's role and plan.
type Scope struct {
	Kind            string // company | personal
	Role            string
	UserID          uuid.UUID
	CompanyID       uuid.UUID
	LedDepartments  []uuid.UUID
	Department      uuid.NullUUID
	Employee        uuid.NullUUID
	Instruction     uuid.NullUUID
	Folder          uuid.NullUUID
	IncludeInternal bool
	ExcludeShared   bool
	TeamAllowed     bool
	PersonalAllowed bool
	RetentionDays   int
	Timezone        string
	Location        *time.Location
	Period          Period
}

// Period is the requested window, the one before it of the same length, and the
// trend bucket that fits its length.
type Period struct {
	From, To, PreviousFrom, PreviousTo time.Time
	Bucket                             string
}

func (s Scope) ownOnly() bool { return s.Role == roleEmployee || s.Role == rolePersonal }

// resolve works out the role and plan of the viewer and forces the filters the
// role is held to.
func (s *Service) resolve(ctx context.Context, req Request) (Scope, error) {
	scope := Scope{UserID: req.UserID, Instruction: req.InstructionID, Folder: req.FolderID,
		IncludeInternal: req.IncludeInternal, ExcludeShared: req.ExcludeShared}
	scope.Timezone, scope.Location = s.timezone(ctx, req.UserID)
	if req.Personal || !req.CompanyID.Valid {
		scope.Kind, scope.Role = "personal", rolePersonal
		flags, err := s.plan(ctx, `s.type = 'personal' AND s.user_uuid = $1`, req.UserID)
		if err != nil {
			return Scope{}, err
		}
		scope.PersonalAllowed, scope.RetentionDays = flags.personal, flags.retention
		scope.Employee = uuid.NullUUID{UUID: req.UserID, Valid: true}
		scope.Period = s.period(req, scope.Location)
		return scope, nil
	}
	scope.Kind, scope.CompanyID = "company", req.CompanyID.UUID
	var companyRole sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT role FROM company_members WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active'`, scope.CompanyID, req.UserID).Scan(&companyRole)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrForbidden
	}
	if err != nil {
		return Scope{}, fmt.Errorf("read company role: %w", err)
	}
	switch companyRole.String {
	case roleManager, roleDeputy:
		scope.Role = companyRole.String
	default:
		rows, err := s.db.QueryContext(ctx, `
			SELECT dm.department_uuid FROM department_members dm JOIN departments d ON d.department_uuid = dm.department_uuid
			WHERE d.company_uuid = $1 AND dm.user_uuid = $2 AND dm.role = 'department_leader' AND dm.status = 'active' AND d.deleted_at IS NULL
			ORDER BY d.name`, scope.CompanyID, req.UserID)
		if err != nil {
			return Scope{}, fmt.Errorf("read led departments: %w", err)
		}
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				scope.LedDepartments = append(scope.LedDepartments, id)
			}
		}
		_ = rows.Close()
		scope.Role = roleEmployee
		if len(scope.LedDepartments) > 0 {
			scope.Role = roleLeader
		}
	}
	flags, err := s.plan(ctx, `s.type = 'business' AND s.user_uuid IN (SELECT manager_user_uuid FROM companies WHERE company_uuid = $1 AND deleted_at IS NULL)`, scope.CompanyID)
	if err != nil {
		return Scope{}, err
	}
	scope.TeamAllowed, scope.RetentionDays = flags.team, flags.retention
	switch scope.Role {
	case roleEmployee:
		// An employee sees their own numbers whatever the plan says.
		scope.Employee = uuid.NullUUID{UUID: req.UserID, Valid: true}
	case roleLeader:
		if req.DepartmentID.Valid && contains(scope.LedDepartments, req.DepartmentID.UUID) {
			scope.Department = req.DepartmentID
		}
		scope.Employee = req.EmployeeID
	default:
		scope.Department, scope.Employee = req.DepartmentID, req.EmployeeID
	}
	scope.Period = s.period(req, scope.Location)
	return scope, nil
}

// requireTeam is for views of other people's numbers.
func (s Scope) requireTeam() error {
	switch {
	case s.Kind == "personal":
		if !s.PersonalAllowed {
			return ErrPersonalProgressDenied
		}
		return nil
	case s.Role == roleEmployee:
		return nil
	case !s.TeamAllowed:
		return ErrTeamAnalyticsDenied
	}
	return nil
}

// requireOwn is for a viewer's own numbers: a company employee always sees
// them; a personal account needs a paid plan.
func (s Scope) requireOwn() error {
	if s.Kind == "personal" && !s.PersonalAllowed {
		return ErrPersonalProgressDenied
	}
	return nil
}

type planFlags struct {
	team, personal bool
	retention      int
}

func (s *Service) plan(ctx context.Context, condition string, arg any) (planFlags, error) {
	var flags planFlags
	err := s.db.QueryRowContext(ctx, `
		SELECT p.team_analytics_enabled, p.personal_progress_enabled, p.history_retention_days
		FROM subscriptions s JOIN plans p ON p.plan_uuid = s.plan_uuid
		WHERE s.status = 'active' AND s.starts_at <= now() AND (s.ends_at IS NULL OR s.ends_at > now()) AND `+condition+`
		ORDER BY p.monthly_price_minor DESC LIMIT 1`, arg).Scan(&flags.team, &flags.personal, &flags.retention)
	if errors.Is(err, sql.ErrNoRows) {
		return planFlags{}, nil
	}
	if err != nil {
		return planFlags{}, fmt.Errorf("read plan: %w", err)
	}
	return flags, nil
}

func (s *Service) timezone(ctx context.Context, userID uuid.UUID) (string, *time.Location) {
	var name sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT timezone FROM user_profiles WHERE user_uuid = $1`, userID).Scan(&name)
	if name.Valid && strings.TrimSpace(name.String) != "" {
		if location, err := time.LoadLocation(name.String); err == nil {
			return name.String, location
		}
	}
	location, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		return "UTC", time.UTC
	}
	return defaultTimezone, location
}

// period defaults to the last 30 days ending tomorrow in the viewer's zone.
func (s *Service) period(req Request, location *time.Location) Period {
	now := s.now().In(location)
	to := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
	from := to.AddDate(0, 0, -30)
	if req.To != nil {
		to = *req.To
	}
	if req.From != nil {
		from = *req.From
	}
	if !from.Before(to) {
		from = to.AddDate(0, 0, -30)
	}
	length := to.Sub(from)
	p := Period{From: from, To: to, PreviousFrom: from.Add(-length), PreviousTo: from}
	switch days := length.Hours() / 24; {
	case days <= 31:
		p.Bucket = "day"
	case days <= 26*7:
		p.Bucket = "week"
	default:
		p.Bucket = "month"
	}
	return p
}

func contains(ids []uuid.UUID, id uuid.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
