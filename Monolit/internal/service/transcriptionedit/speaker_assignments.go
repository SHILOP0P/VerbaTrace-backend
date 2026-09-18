package transcriptionedit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/callsubject"

	"github.com/google/uuid"
)

var ErrInvalidSpeakerAssignments = errors.New("invalid speaker assignments")

type SpeakerAssignment struct {
	SpeakerKey      string     `json:"speaker_key"`
	DisplayName     string     `json:"display_name"`
	Role            string     `json:"role"`
	CustomRole      string     `json:"custom_role,omitempty"`
	ContactUserUUID *uuid.UUID `json:"contact_user_uuid,omitempty"`
}

func (s *Service) ListSpeakerAssignments(ctx context.Context, callID, userID uuid.UUID) ([]SpeakerAssignment, error) {
	if _, err := s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT speaker_key,display_name,role,custom_role,contact_user_uuid FROM call_transcription_speaker_assignments WHERE call_uuid=$1 ORDER BY speaker_key`, callID)
	if err != nil {
		return nil, fmt.Errorf("list speaker assignments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]SpeakerAssignment, 0)
	for rows.Next() {
		var item SpeakerAssignment
		if err := rows.Scan(&item.SpeakerKey, &item.DisplayName, &item.Role, &item.CustomRole, &item.ContactUserUUID); err != nil {
			return nil, fmt.Errorf("scan speaker assignment: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) ReplaceSpeakerAssignments(ctx context.Context, callID, userID uuid.UUID, input []SpeakerAssignment) ([]SpeakerAssignment, error) {
	if len(input) > 32 {
		return nil, ErrInvalidSpeakerAssignments
	}
	// Marking an employee in a call shares it with them, so only those who may
	// change the call may mark.
	call, err := s.callRepository.GetEditableByUUID(ctx, callID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureCompanyActive(ctx, callID); err != nil {
		return nil, err
	}
	if err := s.ensureNotUnderReview(ctx, callID); err != nil {
		return nil, err
	}
	allowedRoles := map[string]bool{"unknown": true, "client": true, "manager": true, "operator": true, "partner": true, "other": true}
	seen := make(map[string]bool, len(input))
	for index := range input {
		input[index].SpeakerKey = strings.TrimSpace(input[index].SpeakerKey)
		input[index].DisplayName = strings.TrimSpace(input[index].DisplayName)
		input[index].Role = strings.TrimSpace(input[index].Role)
		input[index].CustomRole = strings.TrimSpace(input[index].CustomRole)
		if input[index].Role == "" {
			input[index].Role = "unknown"
		}
		if input[index].SpeakerKey == "" || len([]rune(input[index].SpeakerKey)) > 100 || len([]rune(input[index].DisplayName)) > 100 || len([]rune(input[index].CustomRole)) > 100 || !allowedRoles[input[index].Role] || (input[index].Role == "other" && input[index].CustomRole == "") || seen[input[index].SpeakerKey] {
			return nil, ErrInvalidSpeakerAssignments
		}
		if input[index].ContactUserUUID != nil {
			// A speaker may be one of the editor's contacts or an employee of the
			// call's company: marking employees is how a call is shared with them.
			var allowed bool
			if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_contacts WHERE user_uuid=$1 AND contact_user_uuid=$2)
				OR EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$3 AND user_uuid=$2 AND status='active')`, userID, *input[index].ContactUserUUID, call.CompanyUUID).Scan(&allowed); err != nil || !allowed {
				return nil, ErrInvalidSpeakerAssignments
			}
		}
		seen[input[index].SpeakerKey] = true
	}
	// A role says who was talking to whom, and the analysis reads it. Renaming a
	// speaker changes a label only, so it leaves the analysis alone.
	rolesChanged, err := s.speakerRolesChanged(ctx, callID, input)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin speaker assignments: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM call_transcription_speaker_assignments WHERE call_uuid=$1`, callID); err != nil {
		return nil, fmt.Errorf("clear speaker assignments: %w", err)
	}
	for _, item := range input {
		if _, err := tx.ExecContext(ctx, `INSERT INTO call_transcription_speaker_assignments(call_uuid,speaker_key,display_name,role,custom_role,contact_user_uuid,updated_by_user_uuid) VALUES($1,$2,$3,$4,$5,$6,$7)`, callID, item.SpeakerKey, item.DisplayName, item.Role, item.CustomRole, item.ContactUserUUID, userID); err != nil {
			return nil, fmt.Errorf("insert speaker assignment: %w", err)
		}
	}
	if rolesChanged {
		if err := markAnalysisStale(ctx, tx, callID); err != nil {
			return nil, fmt.Errorf("mark analysis stale: %w", err)
		}
	}
	// Who spoke decides whom the call counts for and who may read it; that
	// commits together with the roles that say so.
	var change callsubject.Change
	if s.subjects != nil {
		if change, err = s.subjects.ResolveTx(ctx, tx, callID, uuid.NullUUID{UUID: userID, Valid: true}, models.CallSubjectCauseSpeakerAssignments); err != nil {
			return nil, fmt.Errorf("resolve call subjects: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit speaker assignments: %w", err)
	}
	if s.subjects != nil {
		s.subjects.After(ctx, change)
	}
	return input, nil
}

func (s *Service) speakerRolesChanged(ctx context.Context, callID uuid.UUID, input []SpeakerAssignment) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT speaker_key,role,custom_role FROM call_transcription_speaker_assignments WHERE call_uuid=$1`, callID)
	if err != nil {
		return false, fmt.Errorf("read speaker roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	current := map[string]string{}
	for rows.Next() {
		var key, role, customRole string
		if err := rows.Scan(&key, &role, &customRole); err != nil {
			return false, fmt.Errorf("scan speaker role: %w", err)
		}
		current[key] = role + "\x00" + customRole
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read speaker roles: %w", err)
	}

	next := map[string]string{}
	for _, item := range input {
		next[item.SpeakerKey] = item.Role + "\x00" + item.CustomRole
	}
	if len(next) != len(current) {
		return true, nil
	}
	for key, role := range next {
		if current[key] != role {
			return true, nil
		}
	}

	return false, nil
}
