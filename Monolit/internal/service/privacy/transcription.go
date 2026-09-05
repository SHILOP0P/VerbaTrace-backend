package privacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ResolveCallPrivacy(ctx context.Context, call models.Call) (models.CallPrivacyState, error) {
	state := models.CallPrivacyState{
		CallID: call.ID, PolicySource: "none", PolicySnapshot: models.DefaultPrivacyPolicyConfig(),
		MarkerContract: models.PrivacyMarkerContractRUv1, Status: "not_requested",
	}
	for _, candidate := range privacyPolicyCandidates(call) {
		if candidate.ID == uuid.Nil {
			continue
		}
		version, err := s.ActivePolicy(ctx, candidate.Scope, candidate.ID)
		if err != nil {
			return models.CallPrivacyState{}, err
		}
		if version == nil {
			continue
		}
		state.PolicyVersionID = uuid.NullUUID{UUID: version.ID, Valid: true}
		state.PolicySource = string(candidate.Scope)
		state.PolicySnapshot = version.Config
		state.MarkerContract = version.Config.MarkerContract
		if version.Config.Enabled {
			state.Status = "queued"
		}
		return state, nil
	}
	return state, nil
}

type privacyPolicyCandidate struct {
	Scope models.PrivacyScopeType
	ID    uuid.UUID
}

func privacyPolicyCandidates(call models.Call) []privacyPolicyCandidate {
	if call.DepartmentUUID.Valid && call.CompanyUUID.Valid {
		return []privacyPolicyCandidate{
			{Scope: models.PrivacyScopeDepartment, ID: call.DepartmentUUID.UUID},
			{Scope: models.PrivacyScopeCompany, ID: call.CompanyUUID.UUID},
		}
	}
	if call.CompanyUUID.Valid {
		return []privacyPolicyCandidate{{Scope: models.PrivacyScopeCompany, ID: call.CompanyUUID.UUID}}
	}
	return []privacyPolicyCandidate{{Scope: models.PrivacyScopePersonal, ID: call.UploadedByUserUUID.UUID}}
}

func (s *Service) EnsureCallState(ctx context.Context, call models.Call) (models.CallPrivacyState, error) {
	state, err := s.GetCallState(ctx, call.ID)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.CallPrivacyState{}, err
	}
	state, err = s.ResolveCallPrivacy(ctx, call)
	if err != nil {
		return models.CallPrivacyState{}, err
	}
	body, _ := json.Marshal(state.PolicySnapshot)
	_, err = s.db.ExecContext(ctx, `INSERT INTO call_privacy_states
		(call_uuid,privacy_policy_version_uuid,policy_source,policy_snapshot,marker_contract,status)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(call_uuid) DO NOTHING`, state.CallID, state.PolicyVersionID, state.PolicySource, body, state.MarkerContract, state.Status)
	if err != nil {
		return models.CallPrivacyState{}, err
	}
	return s.GetCallState(ctx, call.ID)
}

func (s *Service) GetCallState(ctx context.Context, callID uuid.UUID) (models.CallPrivacyState, error) {
	var result models.CallPrivacyState
	var raw []byte
	var revision sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT call_uuid,privacy_policy_version_uuid,policy_source,policy_snapshot,
		marker_contract,status,transcription_revision,detected_spans,last_error_code,updated_at
		FROM call_privacy_states WHERE call_uuid=$1`, callID).Scan(
		&result.CallID, &result.PolicyVersionID, &result.PolicySource, &raw, &result.MarkerContract, &result.Status,
		&revision, &result.DetectedSpans, &result.LastErrorCode, &result.UpdatedAt)
	if err != nil {
		return result, err
	}
	if revision.Valid {
		result.Revision = int(revision.Int64)
	}
	if err = json.Unmarshal(raw, &result.PolicySnapshot); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) MarkProcessing(ctx context.Context, callID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_privacy_states SET status='processing',started_at=COALESCE(started_at,now()),updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1 AND status='queued'`, callID)
	return err
}

func (s *Service) MarkFailed(ctx context.Context, callID uuid.UUID, code string) error {
	if strings.TrimSpace(code) == "" {
		code = "privacy_processing_failed"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE call_privacy_states SET status='failed',last_error_code=$2,last_error_message_safe='Не удалось безопасно обработать персональные данные',updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1`, callID, code)
	return err
}

func (s *Service) TranscriptionRequest(ctx context.Context, call models.Call, file models.File, mode models.TranscriptionMode) (models.TranscriptionRequest, error) {
	state, err := s.EnsureCallState(ctx, call)
	if err != nil {
		return models.TranscriptionRequest{}, err
	}
	request := models.TranscriptionRequest{File: file, Mode: mode}
	if state.PolicySnapshot.Enabled {
		if err = s.MarkProcessing(ctx, call.ID); err != nil {
			return models.TranscriptionRequest{}, err
		}
		request.Privacy = &models.TranscriptionPrivacyRequest{MarkerContract: state.MarkerContract, EntityTypes: append([]string(nil), state.PolicySnapshot.EntityTypes...)}
	}
	return request, nil
}

func (s *Service) RedactionSpans(ctx context.Context, callID uuid.UUID) ([]models.RedactionSpan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.redaction_span_uuid,r.transcription_uuid,r.revision,r.entity_type,r.marker,
		r.word_start_index,r.word_end_index,r.start_seconds,r.end_seconds,r.source,COALESCE(r.provider_policy,''),r.created_by_user_uuid
		FROM call_transcription_redaction_spans r JOIN call_transcriptions t ON t.transcription_uuid=r.transcription_uuid
		LEFT JOIN call_transcription_revision_state rs ON rs.transcription_uuid=t.transcription_uuid
		WHERE t.call_uuid=$1 AND r.revision=COALESCE(rs.active_revision,1) ORDER BY r.word_start_index`, callID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]models.RedactionSpan, 0)
	for rows.Next() {
		var item models.RedactionSpan
		if err = rows.Scan(&item.ID, &item.TranscriptionID, &item.Revision, &item.EntityType, &item.Marker, &item.WordStartIndex, &item.WordEndIndex, &item.StartSeconds, &item.EndSeconds, &item.Source, &item.ProviderPolicy, &item.CreatedByUserID); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) AnalysisContext(ctx context.Context, callID uuid.UUID) (*models.AnalysisRedactionContext, error) {
	state, err := s.GetCallState(ctx, callID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state.Status != "ready" || !state.PolicySnapshot.Enabled {
		return nil, nil
	}
	spans, err := s.RedactionSpans(ctx, callID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, span := range spans {
		counts[span.EntityType]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := &models.AnalysisRedactionContext{MarkerContract: state.MarkerContract}
	for _, key := range keys {
		result.PresentMarkers = append(result.PresentMarkers, models.AnalysisRedactionMarker{Marker: models.PrivacyMarkersRUv1[key], EntityType: key, Count: counts[key]})
	}
	return result, nil
}

func (s *Service) Capabilities(ctx context.Context, call models.Call, userID uuid.UUID) (models.PrivacyCapabilities, error) {
	state, err := s.EnsureCallState(ctx, call)
	if err != nil {
		return models.PrivacyCapabilities{}, err
	}
	cap := models.PrivacyCapabilities{CanReadOriginalMedia: true, CanRequestSanitizedMedia: state.PolicySnapshot.Enabled}
	if !state.PolicySnapshot.Enabled {
		return cap, nil
	}
	isUploader := call.UploadedByUserUUID.Valid && call.UploadedByUserUUID.UUID == userID
	isManager := false
	if call.CompanyUUID.Valid {
		isManager, err = s.CanManageCompany(ctx, call.CompanyUUID.UUID, userID)
		if err != nil {
			return cap, err
		}
	}
	switch state.PolicySnapshot.OriginalMediaAccess {
	case "uploader_only":
		cap.CanReadOriginalMedia = isUploader
	case "uploader_and_scope_managers":
		cap.CanReadOriginalMedia = isUploader || isManager
	default:
		cap.CanReadOriginalMedia = true
	}
	cap.CanReviewRedactions = isUploader || isManager
	cap.CanManagePrivacyPolicy = isManager || (!call.CompanyUUID.Valid && isUploader)
	return cap, nil
}

func ProviderPolicies(entityTypes []string) ([]string, error) {
	result := make([]string, 0, len(entityTypes)+2)
	for _, entity := range entityTypes {
		switch entity {
		case "address":
			result = append(result, "location_address", "location_address_street", "location_zip")
		default:
			if _, ok := models.PrivacyMarkersRUv1[entity]; !ok {
				return nil, fmt.Errorf("unknown privacy entity type %q", entity)
			}
			result = append(result, entity)
		}
	}
	sort.Strings(result)
	return result, nil
}
