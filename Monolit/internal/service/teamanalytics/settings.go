package teamanalytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/companystate"

	"github.com/google/uuid"
)

type Capabilities struct {
	Scope                   string   `json:"scope"`
	Role                    string   `json:"role"`
	TeamAnalyticsEnabled    bool     `json:"team_analytics_enabled"`
	PersonalProgressEnabled bool     `json:"personal_progress_enabled"`
	CanViewCompany          bool     `json:"can_view_company"`
	CanViewDepartments      bool     `json:"can_view_departments"`
	CanViewEmployees        bool     `json:"can_view_employees"`
	DepartmentUUIDs         []string `json:"department_uuids"`
	OwnProfileOnly          bool     `json:"own_profile_only"`
	MinSample               int      `json:"min_sample"`
	ThinSample              int      `json:"thin_sample"`
	Timezone                string   `json:"timezone"`
	RetentionDays           int      `json:"retention_days"`
}

// Capabilities tells the page what the viewer's role and plan allow, so the
// page does not have to guess the role from the company list.
func (s *Service) Capabilities(ctx context.Context, req Request) (Capabilities, error) {
	scope, err := s.resolve(ctx, req)
	if err != nil {
		return Capabilities{}, err
	}
	out := Capabilities{Scope: scope.Kind, Role: scope.Role, TeamAnalyticsEnabled: scope.TeamAllowed, PersonalProgressEnabled: scope.PersonalAllowed,
		DepartmentUUIDs: []string{}, OwnProfileOnly: scope.ownOnly(), MinSample: minSample, ThinSample: thinSample,
		Timezone: scope.Timezone, RetentionDays: scope.RetentionDays}
	switch scope.Role {
	case roleManager, roleDeputy:
		out.CanViewCompany, out.CanViewDepartments, out.CanViewEmployees = scope.TeamAllowed, scope.TeamAllowed, scope.TeamAllowed
	case roleLeader:
		out.CanViewDepartments, out.CanViewEmployees = scope.TeamAllowed, scope.TeamAllowed
		out.DepartmentUUIDs = uuidStrings(scope.LedDepartments)
	}
	return out, nil
}

type Settings struct {
	CriticalAlertThreshold int       `json:"critical_alert_threshold"`
	GrowthAreasEnabled     bool      `json:"growth_areas_enabled"`
	LockVersion            int       `json:"lock_version"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SettingsPatch struct {
	LockVersion            int
	CriticalAlertThreshold *int
	GrowthAreasEnabled     *bool
}

func (s *Service) requireSettingsRights(ctx context.Context, companyID, userID uuid.UUID) error {
	var allowed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM company_members WHERE company_uuid = $1 AND user_uuid = $2 AND status = 'active' AND role IN ('company_manager','company_deputy'))`, companyID, userID).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("check analytics settings rights: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// GetSettings answers with the defaults when the company never changed them.
func (s *Service) GetSettings(ctx context.Context, companyID, userID uuid.UUID) (Settings, error) {
	if err := s.requireSettingsRights(ctx, companyID, userID); err != nil {
		return Settings{}, err
	}
	settings := Settings{CriticalAlertThreshold: 50, GrowthAreasEnabled: true, LockVersion: 0}
	err := s.db.QueryRowContext(ctx, `SELECT critical_alert_threshold, growth_areas_enabled, lock_version, updated_at FROM company_analytics_settings WHERE company_uuid = $1`, companyID).
		Scan(&settings.CriticalAlertThreshold, &settings.GrowthAreasEnabled, &settings.LockVersion, &settings.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, fmt.Errorf("read analytics settings: %w", err)
	}
	return settings, nil
}

// UpdateSettings changes the settings under optimistic locking: lock_version 0
// means "no row yet".
func (s *Service) UpdateSettings(ctx context.Context, companyID, userID uuid.UUID, patch SettingsPatch) (Settings, error) {
	if err := s.requireSettingsRights(ctx, companyID, userID); err != nil {
		return Settings{}, err
	}
	if patch.CriticalAlertThreshold != nil && (*patch.CriticalAlertThreshold < 0 || *patch.CriticalAlertThreshold > 100) {
		return Settings{}, ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := companystate.EnsureActive(ctx, tx, companyID); err != nil {
		return Settings{}, err
	}
	current := Settings{CriticalAlertThreshold: 50, GrowthAreasEnabled: true}
	err = tx.QueryRowContext(ctx, `SELECT critical_alert_threshold, growth_areas_enabled, lock_version FROM company_analytics_settings WHERE company_uuid = $1 FOR UPDATE`, companyID).
		Scan(&current.CriticalAlertThreshold, &current.GrowthAreasEnabled, &current.LockVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, fmt.Errorf("lock analytics settings: %w", err)
	}
	if current.LockVersion != patch.LockVersion {
		return Settings{}, ErrSettingsVersionConflict
	}
	if patch.CriticalAlertThreshold != nil {
		current.CriticalAlertThreshold = *patch.CriticalAlertThreshold
	}
	if patch.GrowthAreasEnabled != nil {
		current.GrowthAreasEnabled = *patch.GrowthAreasEnabled
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO company_analytics_settings (company_uuid, critical_alert_threshold, growth_areas_enabled, lock_version, updated_by_user_uuid, updated_at)
		VALUES ($1,$2,$3,1,$4,now())
		ON CONFLICT (company_uuid) DO UPDATE SET critical_alert_threshold = EXCLUDED.critical_alert_threshold,
			growth_areas_enabled = EXCLUDED.growth_areas_enabled, lock_version = company_analytics_settings.lock_version + 1,
			updated_by_user_uuid = EXCLUDED.updated_by_user_uuid, updated_at = now()`,
		companyID, current.CriticalAlertThreshold, current.GrowthAreasEnabled, userID); err != nil {
		return Settings{}, fmt.Errorf("save analytics settings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Settings{}, err
	}
	return s.GetSettings(ctx, companyID, userID)
}
