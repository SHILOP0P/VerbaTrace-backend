package user_preferences

import (
	"context"
	"database/sql"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const defaultTheme = "system"

func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (models.UserPreferences, error) {
	query := `INSERT INTO user_preferences (user_uuid, theme)
	VALUES ($1, $2)
	ON CONFLICT (user_uuid) DO NOTHING`
	if _, err := r.db.ExecContext(ctx, query, userID, defaultTheme); err != nil {
		return models.UserPreferences{}, fmt.Errorf("create default preferences: %w", err)
	}

	row := r.db.QueryRowContext(ctx, selectPreferencesQuery()+` WHERE user_uuid = $1`, userID)
	return scanPreferences(row)
}

func (r *Repository) Upsert(ctx context.Context, input models.UpdateUserPreferencesInput) (models.UserPreferences, error) {
	current, err := r.Get(ctx, input.UserUUID)
	if err != nil {
		return models.UserPreferences{}, err
	}

	activeCompanyUUID := current.ActiveCompanyUUID
	if input.ActiveCompanyUUID != nil {
		activeCompanyUUID = *input.ActiveCompanyUUID
	}

	theme := current.Theme
	if input.Theme != nil {
		theme = *input.Theme
	}

	dateFrom := current.DateRange.From
	dateTo := current.DateRange.To
	if input.DateRange != nil {
		dateFrom = input.DateRange.From
		dateTo = input.DateRange.To
	}

	invitationsMuted := current.InvitationsMuted
	if input.InvitationsMuted != nil {
		invitationsMuted = *input.InvitationsMuted
	}

	query := `UPDATE user_preferences
	SET active_company_uuid = $2,
	    theme = $3,
	    invitations_muted = $6,
	    date_range_from = $4,
	    date_range_to = $5,
	    updated_at = now()
	WHERE user_uuid = $1
	RETURNING user_uuid,
	          active_company_uuid,
	          theme,
	          date_range_from,
	          date_range_to,
	          invitations_muted,
	          updated_at`

	row := r.db.QueryRowContext(ctx, query, input.UserUUID, activeCompanyUUID, theme, dateFrom, dateTo, invitationsMuted)
	return scanPreferences(row)
}

func selectPreferencesQuery() string {
	return `SELECT user_uuid,
	              active_company_uuid,
	              theme,
	              date_range_from,
	              date_range_to,
	              invitations_muted,
	              updated_at
	       FROM user_preferences`
}

func scanPreferences(row interface{ Scan(dest ...any) error }) (models.UserPreferences, error) {
	var preferences models.UserPreferences
	var activeCompanyUUID uuid.NullUUID
	var dateFrom sql.NullTime
	var dateTo sql.NullTime

	if err := row.Scan(
		&preferences.UserUUID,
		&activeCompanyUUID,
		&preferences.Theme,
		&dateFrom,
		&dateTo,
		&preferences.InvitationsMuted,
		&preferences.UpdatedAt,
	); err != nil {
		return models.UserPreferences{}, fmt.Errorf("scan preferences: %w", err)
	}

	preferences.ActiveCompanyUUID = activeCompanyUUID
	if dateFrom.Valid {
		value := dateFrom.Time.Format("2006-01-02")
		preferences.DateRange.From = &value
	}
	if dateTo.Valid {
		value := dateTo.Time.Format("2006-01-02")
		preferences.DateRange.To = &value
	}

	return preferences, nil
}
