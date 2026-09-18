package teamanalytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// GetPersonalSettings are the analytics settings of a personal account; they
// live in the user's preferences and default like a company's.
func (s *Service) GetPersonalSettings(ctx context.Context, userID uuid.UUID) (Settings, error) {
	settings := Settings{CriticalAlertThreshold: 50, GrowthAreasEnabled: true}
	err := s.db.QueryRowContext(ctx, `SELECT critical_alert_threshold, growth_areas_enabled, updated_at FROM user_preferences WHERE user_uuid = $1`, userID).
		Scan(&settings.CriticalAlertThreshold, &settings.GrowthAreasEnabled, &settings.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, fmt.Errorf("read personal analytics settings: %w", err)
	}
	return settings, nil
}

// UpdatePersonalSettings changes them. One person edits them, so there is no
// version to lock.
func (s *Service) UpdatePersonalSettings(ctx context.Context, userID uuid.UUID, patch SettingsPatch) (Settings, error) {
	if patch.CriticalAlertThreshold != nil && (*patch.CriticalAlertThreshold < 0 || *patch.CriticalAlertThreshold > 100) {
		return Settings{}, ErrInvalidRequest
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_preferences (user_uuid, critical_alert_threshold, growth_areas_enabled)
		VALUES ($1, COALESCE($2::smallint, 50), COALESCE($3::boolean, true))
		ON CONFLICT (user_uuid) DO UPDATE SET
			critical_alert_threshold = COALESCE($2::smallint, user_preferences.critical_alert_threshold),
			growth_areas_enabled = COALESCE($3::boolean, user_preferences.growth_areas_enabled),
			updated_at = now()`, userID, patch.CriticalAlertThreshold, patch.GrowthAreasEnabled); err != nil {
		return Settings{}, fmt.Errorf("save personal analytics settings: %w", err)
	}
	return s.GetPersonalSettings(ctx, userID)
}
