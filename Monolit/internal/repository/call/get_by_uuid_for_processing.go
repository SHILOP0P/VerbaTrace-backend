package call

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	repoModel "calllens/monolit/internal/repository/models"
	"calllens/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) GetByUUIDForProcessing(ctx context.Context, callUUID uuid.UUID) (model.Call, error) {
	query := `
	SELECT call_uuid,
	       title,
	       status,
	       audio_path,
	       asr_cache_path,
	       original_filename,
	       mime_type,
	       size_bytes,
	       duration_seconds,
	       uploaded_by_user_uuid,
	       company_uuid,
	       department_uuid,
	       visibility_scope,
	       skip_custom_instructions,
	       created_at
	FROM calls
	WHERE call_uuid = $1
	`

	row := r.db.QueryRowContext(ctx, query, callUUID)

	var repoCall repoModel.Call
	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Call{}, model.ErrCallNotFound
		}

		return model.Call{}, fmt.Errorf("get call by uuid for processing: %w", err)
	}

	call, err := converter.RepoCallToModel(repoCall)
	if err != nil {
		return model.Call{}, err
	}
	hints, err := r.getSpeakerHints(ctx, callUUID)
	if err != nil {
		return model.Call{}, err
	}
	call.SpeakerHints = hints
	roles, err := r.getDiarizationRoles(ctx, callUUID)
	if err != nil {
		return model.Call{}, err
	}
	call.DiarizationRoles = roles
	return call, nil
}

func (r *Repository) getSpeakerHints(ctx context.Context, callUUID uuid.UUID) ([]model.SpeakerHint, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT user_uuid, participant_name, username, role, note
		FROM call_speaker_hints
		WHERE call_uuid = $1
		ORDER BY position ASC
	`, callUUID)
	if err != nil {
		return nil, fmt.Errorf("get speaker hints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hints []model.SpeakerHint
	for rows.Next() {
		var hint model.SpeakerHint
		if err := rows.Scan(&hint.UserID, &hint.Name, &hint.Username, &hint.Role, &hint.Note); err != nil {
			return nil, fmt.Errorf("scan speaker hint: %w", err)
		}
		hints = append(hints, hint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate speaker hints: %w", err)
	}
	return hints, nil
}

func (r *Repository) getDiarizationRoles(ctx context.Context, callUUID uuid.UUID) ([]model.DiarizationRole, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, description
		FROM call_diarization_roles
		WHERE call_uuid = $1
		ORDER BY position ASC
	`, callUUID)
	if err != nil {
		return nil, fmt.Errorf("get diarization roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var roles []model.DiarizationRole
	for rows.Next() {
		var role model.DiarizationRole
		if err := rows.Scan(&role.Name, &role.Description); err != nil {
			return nil, fmt.Errorf("scan diarization role: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate diarization roles: %w", err)
	}
	return roles, nil
}
