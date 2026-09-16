package privacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var (
	ErrPolicyForbidden = errors.New("privacy policy forbidden")
	ErrPolicyConflict  = errors.New("privacy policy revision conflict")
	ErrPolicyInvalid   = errors.New("privacy policy invalid")
	ErrPreviewStale    = errors.New("privacy preview stale")
)

var allowedEntityTypes = func() map[string]struct{} {
	result := make(map[string]struct{}, len(models.PrivacyMarkersRUv1))
	for key := range models.PrivacyMarkersRUv1 {
		result[key] = struct{}{}
	}
	return result
}()

func CanonicalPrivacyConfig(input models.PrivacyPolicyConfig) (models.PrivacyPolicyConfig, []byte, error) {
	input.Enforcement = strings.TrimSpace(input.Enforcement)
	input.MarkerContract = strings.TrimSpace(input.MarkerContract)
	input.OriginalMediaAccess = strings.TrimSpace(input.OriginalMediaAccess)
	input.SanitizedMedia = strings.TrimSpace(input.SanitizedMedia)
	input.Exports = strings.TrimSpace(input.Exports)
	input.AnalysisInput = strings.TrimSpace(input.AnalysisInput)
	if input.SchemaVersion != 1 || input.Enforcement != "required" ||
		input.MarkerContract != models.PrivacyMarkerContractRUv1 || input.AnalysisInput != "redacted" {
		return models.PrivacyPolicyConfig{}, nil, ErrPolicyInvalid
	}
	if input.OriginalMediaAccess != "call_acl" && input.OriginalMediaAccess != "uploader_and_scope_managers" && input.OriginalMediaAccess != "uploader_only" {
		return models.PrivacyPolicyConfig{}, nil, ErrPolicyInvalid
	}
	if input.SanitizedMedia != "off" && input.SanitizedMedia != "on_demand" {
		return models.PrivacyPolicyConfig{}, nil, ErrPolicyInvalid
	}
	if input.Exports != "redacted_by_default" && input.Exports != "redacted_only" {
		return models.PrivacyPolicyConfig{}, nil, ErrPolicyInvalid
	}
	seen := make(map[string]struct{}, len(input.EntityTypes))
	entities := make([]string, 0, len(input.EntityTypes))
	for _, raw := range input.EntityTypes {
		entity := strings.TrimSpace(raw)
		if _, ok := allowedEntityTypes[entity]; !ok {
			return models.PrivacyPolicyConfig{}, nil, fmt.Errorf("%w: entity_type", ErrPolicyInvalid)
		}
		if _, ok := seen[entity]; ok {
			continue
		}
		seen[entity] = struct{}{}
		entities = append(entities, entity)
	}
	sort.Strings(entities)
	input.EntityTypes = entities
	if input.Enabled && len(entities) == 0 {
		return models.PrivacyPolicyConfig{}, nil, ErrPolicyInvalid
	}
	if input.Enabled && input.OriginalMediaAccess != "call_acl" && input.SanitizedMedia == "off" {
		return models.PrivacyPolicyConfig{}, nil, ErrPolicyInvalid
	}
	body, err := json.Marshal(input)
	if err != nil {
		return models.PrivacyPolicyConfig{}, nil, err
	}
	hash := sha256.Sum256(body)
	return input, hash[:], nil
}

func (s *Service) CanManageCompany(ctx context.Context, companyID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2
		AND role IN ('company_manager','company_deputy') AND status='active')`, companyID, userID).Scan(&exists)
	return exists, err
}

func (s *Service) DepartmentCompanyID(ctx context.Context, departmentID uuid.UUID) (uuid.UUID, error) {
	var companyID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT company_uuid FROM departments WHERE department_uuid=$1`, departmentID).Scan(&companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrPolicyInvalid
	}
	return companyID, err
}

func (s *Service) DepartmentBelongsToCompany(ctx context.Context, companyID, departmentID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM departments WHERE department_uuid=$1 AND company_uuid=$2)`, departmentID, companyID).Scan(&exists)
	return exists, err
}

func (s *Service) canManagePolicyScope(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID) (bool, error) {
	switch scope {
	case models.PrivacyScopePersonal:
		return scopeID == actorID, nil
	case models.PrivacyScopeCompany:
		return s.CanManageCompany(ctx, scopeID, actorID)
	case models.PrivacyScopeDepartment:
		companyID, err := s.DepartmentCompanyID(ctx, scopeID)
		if err != nil {
			return false, err
		}
		return s.CanManageCompany(ctx, companyID, actorID)
	default:
		return false, ErrPolicyInvalid
	}
}

func (s *Service) canViewPolicyScope(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID) (bool, error) {
	if scope == models.PrivacyScopePersonal {
		return scopeID == actorID, nil
	}
	companyID := scopeID
	if scope == models.PrivacyScopeDepartment {
		var err error
		companyID, err = s.DepartmentCompanyID(ctx, scopeID)
		if err != nil {
			return false, err
		}
	} else if scope != models.PrivacyScopeCompany {
		return false, ErrPolicyInvalid
	}
	var visible bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, companyID, actorID).Scan(&visible)
	return visible, err
}

func (s *Service) GetPolicy(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID) (models.PrivacyPolicyView, error) {
	canManage, err := s.canManagePolicyScope(ctx, scope, scopeID, actorID)
	if err != nil {
		return models.PrivacyPolicyView{}, err
	}
	visible, err := s.canViewPolicyScope(ctx, scope, scopeID, actorID)
	if err != nil {
		return models.PrivacyPolicyView{}, err
	}
	if !visible {
		return models.PrivacyPolicyView{}, ErrPolicyForbidden
	}
	view := models.PrivacyPolicyView{ScopeType: scope, ScopeID: scopeID, CanManage: canManage, MarkerContract: models.PrivacyMarkerContractRUv1}
	query := `SELECT privacy_policy_uuid,active_version_uuid FROM transcription_privacy_policies WHERE `
	args := []any{scopeID}
	switch scope {
	case models.PrivacyScopePersonal:
		query += `scope_type='personal' AND owner_user_uuid=$1`
	case models.PrivacyScopeCompany:
		query += `scope_type='company' AND company_uuid=$1`
	case models.PrivacyScopeDepartment:
		query += `scope_type='department' AND department_uuid=$1`
	default:
		return models.PrivacyPolicyView{}, ErrPolicyInvalid
	}
	var active uuid.NullUUID
	if err = s.db.QueryRowContext(ctx, query, args...).Scan(&view.PolicyID, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			view.PolicyID = uuid.Nil
		} else {
			return models.PrivacyPolicyView{}, err
		}
	}
	if active.Valid {
		var version models.PrivacyPolicyVersion
		version, err = s.getVersion(ctx, active.UUID)
		if err != nil {
			return models.PrivacyPolicyView{}, err
		}
		view.ActiveVersion = &version
	}
	if canManage && view.PolicyID != uuid.Nil {
		draft, err := s.getDraft(ctx, view.PolicyID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return models.PrivacyPolicyView{}, err
		}
		if err == nil {
			view.Draft = &draft
		}
	}
	if scope == models.PrivacyScopeDepartment {
		companyID, companyErr := s.DepartmentCompanyID(ctx, scopeID)
		if companyErr != nil {
			return models.PrivacyPolicyView{}, companyErr
		}
		inherited, inheritedErr := s.ActivePolicy(ctx, models.PrivacyScopeCompany, companyID)
		if inheritedErr != nil {
			return models.PrivacyPolicyView{}, inheritedErr
		}
		if inherited != nil {
			view.InheritedScopeType = models.PrivacyScopeCompany
			view.InheritedScopeID = companyID
			view.InheritedVersion = inherited
		}
	}
	return view, nil
}

func (s *Service) ensurePolicy(ctx context.Context, scope models.PrivacyScopeType, scopeID uuid.UUID) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	var result uuid.UUID
	switch scope {
	case models.PrivacyScopePersonal:
		err = s.db.QueryRowContext(ctx, `INSERT INTO transcription_privacy_policies(privacy_policy_uuid,scope_type,owner_user_uuid)
			VALUES($1,'personal',$2) ON CONFLICT (owner_user_uuid) WHERE scope_type='personal'
			DO UPDATE SET updated_at=transcription_privacy_policies.updated_at RETURNING privacy_policy_uuid`, id, scopeID).Scan(&result)
	case models.PrivacyScopeCompany:
		err = s.db.QueryRowContext(ctx, `INSERT INTO transcription_privacy_policies(privacy_policy_uuid,scope_type,company_uuid)
			VALUES($1,'company',$2) ON CONFLICT (company_uuid) WHERE scope_type='company'
			DO UPDATE SET updated_at=transcription_privacy_policies.updated_at RETURNING privacy_policy_uuid`, id, scopeID).Scan(&result)
	case models.PrivacyScopeDepartment:
		var companyID uuid.UUID
		companyID, err = s.DepartmentCompanyID(ctx, scopeID)
		if err == nil {
			err = s.db.QueryRowContext(ctx, `INSERT INTO transcription_privacy_policies(privacy_policy_uuid,scope_type,company_uuid,department_uuid)
				VALUES($1,'department',$2,$3) ON CONFLICT (department_uuid) WHERE scope_type='department'
				DO UPDATE SET updated_at=transcription_privacy_policies.updated_at RETURNING privacy_policy_uuid`, id, companyID, scopeID).Scan(&result)
		}
	default:
		err = ErrPolicyInvalid
	}
	return result, err
}

func (s *Service) SaveDraft(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID, expected *int64, input models.PrivacyPolicyConfig) (models.PrivacyPolicyDraft, error) {
	if ok, err := s.canManagePolicyScope(ctx, scope, scopeID, actorID); err != nil || !ok {
		if err != nil {
			return models.PrivacyPolicyDraft{}, err
		}
		return models.PrivacyPolicyDraft{}, ErrPolicyForbidden
	}
	config, hash, err := CanonicalPrivacyConfig(input)
	if err != nil {
		return models.PrivacyPolicyDraft{}, err
	}
	policyID, err := s.ensurePolicy(ctx, scope, scopeID)
	if err != nil {
		return models.PrivacyPolicyDraft{}, err
	}
	body, _ := json.Marshal(config)
	var draft models.PrivacyPolicyDraft
	var raw []byte
	if expected == nil {
		err = s.db.QueryRowContext(ctx, `INSERT INTO transcription_privacy_policy_drafts
			(privacy_policy_uuid,config_schema_version,config,config_sha256,updated_by_user_uuid)
			VALUES($1,1,$2,$3,$4) ON CONFLICT (privacy_policy_uuid) DO NOTHING
			RETURNING privacy_policy_uuid,config,config_sha256,lock_version,updated_by_user_uuid,updated_at`, policyID, body, hash, actorID).
			Scan(&draft.PolicyID, &raw, &draft.ConfigSHA256, &draft.LockVersion, &draft.UpdatedBy, &draft.UpdatedAt)
	} else {
		err = s.db.QueryRowContext(ctx, `UPDATE transcription_privacy_policy_drafts SET config=$2,config_sha256=$3,
			updated_by_user_uuid=$4,updated_at=now(),lock_version=lock_version+1 WHERE privacy_policy_uuid=$1 AND lock_version=$5
			RETURNING privacy_policy_uuid,config,config_sha256,lock_version,updated_by_user_uuid,updated_at`, policyID, body, hash, actorID, *expected).
			Scan(&draft.PolicyID, &raw, &draft.ConfigSHA256, &draft.LockVersion, &draft.UpdatedBy, &draft.UpdatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return models.PrivacyPolicyDraft{}, ErrPolicyConflict
	}
	if err != nil {
		return models.PrivacyPolicyDraft{}, err
	}
	if err = json.Unmarshal(raw, &draft.Config); err != nil {
		return models.PrivacyPolicyDraft{}, err
	}
	return draft, nil
}

func (s *Service) Preview(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID, input models.PrivacyPolicyConfig) (models.PrivacyPolicyConfig, string, error) {
	if ok, err := s.canManagePolicyScope(ctx, scope, scopeID, actorID); err != nil || !ok {
		if err != nil {
			return models.PrivacyPolicyConfig{}, "", err
		}
		return models.PrivacyPolicyConfig{}, "", ErrPolicyForbidden
	}
	config, hash, err := CanonicalPrivacyConfig(input)
	if err != nil {
		return models.PrivacyPolicyConfig{}, "", err
	}
	view, err := s.GetPolicy(ctx, scope, scopeID, actorID)
	if err != nil {
		return models.PrivacyPolicyConfig{}, "", err
	}
	return config, policyPreviewHash(hash, scope, scopeID, view.ActiveVersion), nil
}

func (s *Service) Publish(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID, previewHash, reason string, highImpactAcknowledged bool) (models.PrivacyPolicyVersion, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 3 || len([]rune(reason)) > 500 {
		return models.PrivacyPolicyVersion{}, ErrPolicyInvalid
	}
	if scope != models.PrivacyScopePersonal && len([]rune(reason)) < 10 {
		return models.PrivacyPolicyVersion{}, ErrPolicyInvalid
	}
	view, err := s.GetPolicy(ctx, scope, scopeID, actorID)
	if err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	if !view.CanManage || view.Draft == nil {
		return models.PrivacyPolicyVersion{}, ErrPolicyConflict
	}
	expectedHash := policyPreviewHash(view.Draft.ConfigSHA256, scope, scopeID, view.ActiveVersion)
	if previewHash != expectedHash {
		return models.PrivacyPolicyVersion{}, ErrPreviewStale
	}
	if containsPrivacyEntity(view.Draft.Config.EntityTypes, "money_amount") && !highImpactAcknowledged {
		return models.PrivacyPolicyVersion{}, ErrPolicyInvalid
	}
	id, err := uuid.NewV7()
	if err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM transcription_privacy_policies WHERE privacy_policy_uuid=$1 FOR UPDATE`, view.PolicyID); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM transcription_privacy_policy_versions WHERE privacy_policy_uuid=$1`, view.PolicyID).Scan(&version); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	body, _ := json.Marshal(view.Draft.Config)
	var result models.PrivacyPolicyVersion
	var raw []byte
	err = tx.QueryRowContext(ctx, `INSERT INTO transcription_privacy_policy_versions
		(privacy_policy_version_uuid,privacy_policy_uuid,version,config_schema_version,config,config_sha256,publish_reason,published_by_user_uuid)
		VALUES($1,$2,$3,1,$4,$5,$6,$7) RETURNING privacy_policy_version_uuid,privacy_policy_uuid,version,config,config_sha256,publish_reason,published_by_user_uuid,published_at`,
		id, view.PolicyID, version, body, view.Draft.ConfigSHA256, reason, actorID).
		Scan(&result.ID, &result.PolicyID, &result.Version, &raw, &result.ConfigSHA256, &result.PublishReason, &result.PublishedBy, &result.PublishedAt)
	if err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	if err = json.Unmarshal(raw, &result.Config); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE transcription_privacy_policies SET active_version_uuid=$2,lock_version=lock_version+1,updated_at=now() WHERE privacy_policy_uuid=$1`, view.PolicyID, result.ID); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM transcription_privacy_policy_drafts WHERE privacy_policy_uuid=$1`, view.PolicyID); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	if err = insertAudit(ctx, tx, scope, scopeID, uuid.NullUUID{}, actorID, "policy_published", "privacy_policy_version", result.ID, map[string]any{"version": result.Version}); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	return result, nil
}

func policyPreviewHash(configHash []byte, scope models.PrivacyScopeType, scopeID uuid.UUID, active *models.PrivacyPolicyVersion) string {
	activeID := "none"
	if active != nil {
		activeID = active.ID.String()
	}
	payload := strings.Join([]string{base64.RawURLEncoding.EncodeToString(configHash), string(scope), scopeID.String(), activeID, models.PrivacyMarkerContractRUv1}, ":")
	hash := sha256.Sum256([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
func containsPrivacyEntity(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) getDraft(ctx context.Context, policyID uuid.UUID) (models.PrivacyPolicyDraft, error) {
	var result models.PrivacyPolicyDraft
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT privacy_policy_uuid,config,config_sha256,lock_version,updated_by_user_uuid,updated_at FROM transcription_privacy_policy_drafts WHERE privacy_policy_uuid=$1`, policyID).
		Scan(&result.PolicyID, &raw, &result.ConfigSHA256, &result.LockVersion, &result.UpdatedBy, &result.UpdatedAt)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result.Config)
	return result, err
}

func (s *Service) getVersion(ctx context.Context, id uuid.UUID) (models.PrivacyPolicyVersion, error) {
	var result models.PrivacyPolicyVersion
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT privacy_policy_version_uuid,privacy_policy_uuid,version,config,config_sha256,publish_reason,published_by_user_uuid,published_at FROM transcription_privacy_policy_versions WHERE privacy_policy_version_uuid=$1`, id).
		Scan(&result.ID, &result.PolicyID, &result.Version, &raw, &result.ConfigSHA256, &result.PublishReason, &result.PublishedBy, &result.PublishedAt)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result.Config)
	return result, err
}

func (s *Service) ListVersions(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID) ([]models.PrivacyPolicyVersion, error) {
	view, err := s.GetPolicy(ctx, scope, scopeID, actorID)
	if err != nil {
		return nil, err
	}
	if view.PolicyID == uuid.Nil {
		return []models.PrivacyPolicyVersion{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT privacy_policy_version_uuid,privacy_policy_uuid,version,config,config_sha256,publish_reason,published_by_user_uuid,published_at FROM transcription_privacy_policy_versions WHERE privacy_policy_uuid=$1 ORDER BY version DESC`, view.PolicyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.PrivacyPolicyVersion, 0)
	for rows.Next() {
		var item models.PrivacyPolicyVersion
		var raw []byte
		if err = rows.Scan(&item.ID, &item.PolicyID, &item.Version, &raw, &item.ConfigSHA256, &item.PublishReason, &item.PublishedBy, &item.PublishedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item.Config); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetVersionNumber(ctx context.Context, scope models.PrivacyScopeType, scopeID, actorID uuid.UUID, version int) (models.PrivacyPolicyVersion, error) {
	if version < 1 {
		return models.PrivacyPolicyVersion{}, ErrPolicyInvalid
	}
	view, err := s.GetPolicy(ctx, scope, scopeID, actorID)
	if err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	var id uuid.UUID
	if err = s.db.QueryRowContext(ctx, `SELECT privacy_policy_version_uuid FROM transcription_privacy_policy_versions WHERE privacy_policy_uuid=$1 AND version=$2`, view.PolicyID, version).Scan(&id); err != nil {
		return models.PrivacyPolicyVersion{}, err
	}
	return s.getVersion(ctx, id)
}

func (s *Service) PolicyVersionNumber(ctx context.Context, id uuid.UUID) (int, error) {
	if id == uuid.Nil {
		return 0, nil
	}
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM transcription_privacy_policy_versions WHERE privacy_policy_version_uuid=$1`, id).Scan(&version)
	return version, err
}

func insertAudit(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, scope models.PrivacyScopeType, scopeID uuid.UUID, callID uuid.NullUUID, actorID uuid.UUID, eventType, entityType string, entityID uuid.UUID, metadata any) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO privacy_audit_events(privacy_audit_uuid,scope_type,scope_uuid,call_uuid,actor_user_uuid,actor_type,event_type,entity_type,entity_uuid,metadata_redacted) VALUES($1,$2,$3,$4,$5,'user',$6,$7,$8,$9)`, id, scope, scopeID, callID, actorID, eventType, entityType, entityID, body)
	return err
}

func activePolicyQuery(scope models.PrivacyScopeType) string {
	switch scope {
	case models.PrivacyScopePersonal:
		return `p.scope_type='personal' AND p.owner_user_uuid=$1`
	case models.PrivacyScopeCompany:
		return `p.scope_type='company' AND p.company_uuid=$1`
	case models.PrivacyScopeDepartment:
		return `p.scope_type='department' AND p.department_uuid=$1`
	default:
		return `FALSE`
	}
}

func (s *Service) ActivePolicy(ctx context.Context, scope models.PrivacyScopeType, scopeID uuid.UUID) (*models.PrivacyPolicyVersion, error) {
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT p.active_version_uuid FROM transcription_privacy_policies p WHERE `+activePolicyQuery(scope)+` AND p.active_version_uuid IS NOT NULL`, scopeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := s.getVersion(ctx, id)
	return &v, err
}
