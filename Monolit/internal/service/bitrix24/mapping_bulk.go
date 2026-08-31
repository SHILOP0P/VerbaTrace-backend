package bitrix24

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const maxBulkMappingChanges = 500

func (s *Service) PreviewExternalUserMappings(ctx context.Context, connectionID, actor uuid.UUID, changes []models.BitrixMappingChange) (models.BitrixMappingPreview, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return models.BitrixMappingPreview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	preview, err := s.mappingPreview(ctx, tx, connectionID, actor, changes)
	if err != nil {
		return models.BitrixMappingPreview{}, err
	}
	return preview, tx.Commit()
}

func (s *Service) BulkUpdateExternalUserMappings(ctx context.Context, in models.BulkUpdateBitrixMappingsInput) (models.BitrixMappingBulkResult, error) {
	key := strings.TrimSpace(in.RequestKey)
	if len(key) < 8 || len(key) > 200 || strings.TrimSpace(in.PreviewHash) == "" {
		return models.BitrixMappingBulkResult{}, ErrInvalid
	}
	keyHash := sha256.Sum256([]byte(key))
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.BitrixMappingBulkResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID uuid.UUID
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT command_uuid,preview_hash FROM integration_mapping_bulk_commands WHERE connection_uuid=$1 AND requested_by_user_uuid=$2 AND idempotency_key_hash=$3`, in.ConnectionID, in.ActorID, keyHash[:]).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != in.PreviewHash {
			return models.BitrixMappingBulkResult{}, ErrConflict
		}
		mappings, loadErr := loadBulkMappingResults(ctx, tx, in.ConnectionID, in.Changes)
		if loadErr != nil {
			return models.BitrixMappingBulkResult{}, loadErr
		}
		return models.BitrixMappingBulkResult{CommandID: existingID, ConnectionID: in.ConnectionID, PreviewHash: existingHash, ChangesCount: len(mappings), Mappings: mappings, Created: false}, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.BitrixMappingBulkResult{}, err
	}

	preview, err := s.mappingPreview(ctx, tx, in.ConnectionID, in.ActorID, in.Changes)
	if err != nil {
		return models.BitrixMappingBulkResult{}, err
	}
	if preview.PreviewHash != in.PreviewHash || preview.ChangesCount == 0 {
		return models.BitrixMappingBulkResult{}, ErrConflict
	}

	now := s.now().UTC()
	for _, change := range in.Changes {
		if !mappingChangeIsDifferent(preview.Items, change.ExternalUserID) {
			continue
		}
		mappingID := uuid.New()
		result, execErr := tx.ExecContext(ctx, `INSERT INTO integration_external_user_mappings(mapping_uuid,connection_uuid,external_user_id,internal_user_uuid,department_uuid,status,external_display_snapshot,external_active,mapped_by_user_uuid,mapped_at,lock_version,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,'',true,$7,$8,1,$8,$8)
			ON CONFLICT(connection_uuid,external_user_id) DO UPDATE SET internal_user_uuid=EXCLUDED.internal_user_uuid,department_uuid=EXCLUDED.department_uuid,status=EXCLUDED.status,mapped_by_user_uuid=EXCLUDED.mapped_by_user_uuid,mapped_at=EXCLUDED.mapped_at,lock_version=integration_external_user_mappings.lock_version+1,updated_at=EXCLUDED.updated_at
			WHERE integration_external_user_mappings.lock_version=$9`, mappingID, in.ConnectionID, change.ExternalUserID, nullableUUID(change.InternalUserID), nullableUUID(change.DepartmentID), change.Status, in.ActorID, now, change.ExpectedLockVersion)
		if execErr != nil {
			return models.BitrixMappingBulkResult{}, execErr
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return models.BitrixMappingBulkResult{}, ErrConflict
		}
	}

	commandID := uuid.NewSHA1(in.ConnectionID, keyHash[:])
	if _, err = tx.ExecContext(ctx, `INSERT INTO integration_mapping_bulk_commands(command_uuid,connection_uuid,requested_by_user_uuid,idempotency_key_hash,preview_hash,changes_count) VALUES($1,$2,$3,$4,$5,$6)`, commandID, in.ConnectionID, in.ActorID, keyHash[:], preview.PreviewHash, preview.ChangesCount); err != nil {
		return models.BitrixMappingBulkResult{}, err
	}
	metadata, _ := json.Marshal(map[string]any{"changes_count": preview.ChangesCount, "preview_hash": preview.PreviewHash})
	if _, err = tx.ExecContext(ctx, `INSERT INTO integration_audit_events(audit_event_uuid,application_uuid,connection_uuid,actor_type,actor_uuid,event_type,entity_type,entity_uuid,metadata_safe) SELECT $1,c.application_uuid,c.connection_uuid,'user',$2,'mapping.bulk_updated','mapping_bulk_command',$3,$4 FROM integration_connections c WHERE c.connection_uuid=$5`, uuid.New(), in.ActorID, commandID, metadata, in.ConnectionID); err != nil {
		return models.BitrixMappingBulkResult{}, err
	}
	mappings, err := loadBulkMappingResults(ctx, tx, in.ConnectionID, in.Changes)
	if err != nil {
		return models.BitrixMappingBulkResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.BitrixMappingBulkResult{}, err
	}
	return models.BitrixMappingBulkResult{CommandID: commandID, ConnectionID: in.ConnectionID, PreviewHash: preview.PreviewHash, ChangesCount: preview.ChangesCount, Mappings: mappings, Created: true}, nil
}

type mappingPreviewRow struct {
	id           uuid.UUID
	externalID   string
	displayName  string
	internalID   uuid.NullUUID
	departmentID uuid.NullUUID
	status       string
	lockVersion  int64
}

func (s *Service) mappingPreview(ctx context.Context, tx *sql.Tx, connectionID, actor uuid.UUID, changes []models.BitrixMappingChange) (models.BitrixMappingPreview, error) {
	normalized, err := normalizeMappingChanges(changes)
	if err != nil {
		return models.BitrixMappingPreview{}, err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND `+managerAccessSQL+`)`, connectionID, actor).Scan(&allowed); err != nil || !allowed {
		return models.BitrixMappingPreview{}, ErrForbidden
	}
	items := make([]models.BitrixMappingDiff, 0, len(normalized))
	for _, change := range normalized {
		if err = validateMappingAssignment(ctx, tx, connectionID, change); err != nil {
			return models.BitrixMappingPreview{}, err
		}
		var current mappingPreviewRow
		err = tx.QueryRowContext(ctx, `SELECT mapping_uuid,external_user_id,external_display_snapshot,internal_user_uuid,department_uuid,status,lock_version FROM integration_external_user_mappings WHERE connection_uuid=$1 AND external_user_id=$2`, connectionID, change.ExternalUserID).
			Scan(&current.id, &current.externalID, &current.displayName, &current.internalID, &current.departmentID, &current.status, &current.lockVersion)
		if errors.Is(err, sql.ErrNoRows) {
			current.externalID = change.ExternalUserID
			current.displayName = "Bitrix24 user #" + change.ExternalUserID
			current.status = "unmapped"
			current.lockVersion = 0
		} else if err != nil {
			return models.BitrixMappingPreview{}, err
		}
		if current.lockVersion != change.ExpectedLockVersion {
			return models.BitrixMappingPreview{}, ErrConflict
		}
		changed := current.status != change.Status || current.internalID != change.InternalUserID || current.departmentID != change.DepartmentID
		items = append(items, models.BitrixMappingDiff{ExternalUserID: change.ExternalUserID, DisplayName: current.displayName, BeforeInternalUserID: current.internalID, BeforeDepartmentID: current.departmentID, BeforeStatus: current.status, AfterInternalUserID: change.InternalUserID, AfterDepartmentID: change.DepartmentID, AfterStatus: change.Status, LockVersion: current.lockVersion, Changed: changed})
	}
	canonical, _ := json.Marshal(struct {
		ConnectionID uuid.UUID                  `json:"connection_uuid"`
		Items        []models.BitrixMappingDiff `json:"items"`
	}{connectionID, items})
	hash := sha256.Sum256(canonical)
	count := 0
	for _, item := range items {
		if item.Changed {
			count++
		}
	}
	return models.BitrixMappingPreview{ConnectionID: connectionID, PreviewHash: hex.EncodeToString(hash[:]), ChangesCount: count, Items: items}, nil
}

func normalizeMappingChanges(changes []models.BitrixMappingChange) ([]models.BitrixMappingChange, error) {
	if len(changes) == 0 || len(changes) > maxBulkMappingChanges {
		return nil, ErrInvalid
	}
	result := append([]models.BitrixMappingChange(nil), changes...)
	for i := range result {
		result[i].ExternalUserID = strings.TrimSpace(result[i].ExternalUserID)
		if result[i].ExternalUserID == "" || result[i].ExpectedLockVersion < 0 || (result[i].Status != "mapped" && result[i].Status != "ignored" && result[i].Status != "unmapped") {
			return nil, ErrInvalid
		}
		if result[i].Status == "mapped" {
			if !result[i].InternalUserID.Valid {
				return nil, ErrInvalid
			}
		} else if result[i].InternalUserID.Valid || result[i].DepartmentID.Valid {
			return nil, ErrInvalid
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExternalUserID < result[j].ExternalUserID })
	for i := 1; i < len(result); i++ {
		if result[i-1].ExternalUserID == result[i].ExternalUserID {
			return nil, ErrInvalid
		}
	}
	return result, nil
}

func validateMappingAssignment(ctx context.Context, tx *sql.Tx, connectionID uuid.UUID, change models.BitrixMappingChange) error {
	if change.Status != "mapped" {
		return nil
	}
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1
		FROM integration_connections c
		JOIN company_members cm ON cm.company_uuid=c.company_uuid AND cm.user_uuid=$3 AND cm.status='active'
		WHERE c.connection_uuid=$1
		  AND c.provider='bitrix24'
		  AND (
			($4 AND EXISTS(
				SELECT 1
				FROM departments d
				JOIN department_members dm ON dm.department_uuid=d.department_uuid
					AND dm.user_uuid=$3
					AND dm.status='active'
				WHERE d.department_uuid=$2
				  AND d.company_uuid=c.company_uuid
				  AND d.deleted_at IS NULL
			))
			OR
			(NOT $4 AND (cm.role='company_manager' OR c.company_uuid IN (
				SELECT company_uuid FROM companies WHERE manager_user_uuid=$3 AND deleted_at IS NULL
			)))
		  )
	)`, connectionID, change.DepartmentID.UUID, change.InternalUserID.UUID, change.DepartmentID.Valid).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrInvalid
	}
	return nil
}

func mappingChangeIsDifferent(items []models.BitrixMappingDiff, externalID string) bool {
	for _, item := range items {
		if item.ExternalUserID == externalID {
			return item.Changed
		}
	}
	return false
}

func loadBulkMappingResults(ctx context.Context, tx *sql.Tx, connectionID uuid.UUID, changes []models.BitrixMappingChange) ([]models.BitrixExternalUser, error) {
	normalized, err := normalizeMappingChanges(changes)
	if err != nil {
		return nil, err
	}
	result := make([]models.BitrixExternalUser, 0, len(normalized))
	for _, change := range normalized {
		var item models.BitrixExternalUser
		err = tx.QueryRowContext(ctx, `SELECT external_user_id,external_display_snapshot,external_active,mapping_uuid,internal_user_uuid,department_uuid,status,lock_version FROM integration_external_user_mappings WHERE connection_uuid=$1 AND external_user_id=$2`, connectionID, change.ExternalUserID).
			Scan(&item.ExternalUserID, &item.DisplayName, &item.Active, &item.MappingID, &item.InternalUserID, &item.DepartmentID, &item.MappingStatus, &item.LockVersion)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
