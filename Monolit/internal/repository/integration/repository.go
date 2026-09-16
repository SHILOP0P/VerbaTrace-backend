package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"verbatrace/monolit/internal/integrationcrypto"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type Repository struct {
	db     *sql.DB
	cipher *integrationcrypto.Cipher
}

func NewRepository(db *sql.DB, cipher *integrationcrypto.Cipher) *Repository {
	return &Repository{db: db, cipher: cipher}
}

func (r *Repository) CreateConnection(ctx context.Context, in models.CreateIntegrationConnectionInput) (models.IntegrationConnection, error) {
	if in.ApplicationID == uuid.Nil || in.ActorID == uuid.Nil || strings.TrimSpace(in.Name) == "" || (in.Provider != "generic_api" && in.Provider != "bitrix24") {
		return models.IntegrationConnection{}, models.ErrInvalidBillingInput
	}
	if len(in.Settings) == 0 {
		in.Settings = json.RawMessage(`{"schema_version":1}`)
	}
	if !json.Valid(in.Settings) {
		return models.IntegrationConnection{}, models.ErrInvalidBillingInput
	}
	if in.DisablePolicy == "" {
		in.DisablePolicy = "pause"
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var ownerType string
	var ownerUser, ownerCompany uuid.NullUUID
	var environment, status string
	err = tx.QueryRowContext(ctx, `SELECT owner_type,user_uuid,company_uuid,environment,status FROM developer_applications WHERE application_uuid=$1 FOR UPDATE`, in.ApplicationID).Scan(&ownerType, &ownerUser, &ownerCompany, &environment, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationConnection{}, models.ErrIntegrationNotFound
	}
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	if status != "active" {
		return models.IntegrationConnection{}, models.ErrIntegrationDisabled
	}
	if ownerType == "user" {
		if in.Provider == "bitrix24" {
			return models.IntegrationConnection{}, models.ErrForbidden
		}
		if !ownerUser.Valid || ownerUser.UUID != in.ActorID || in.CompanyID.Valid {
			return models.IntegrationConnection{}, models.ErrForbidden
		}
	} else {
		if !ownerCompany.Valid || !in.CompanyID.Valid || ownerCompany.UUID != in.CompanyID.UUID {
			return models.IntegrationConnection{}, models.ErrForbidden
		}
		ok, authErr := companyManager(ctx, tx, ownerCompany.UUID, in.ActorID)
		if authErr != nil {
			return models.IntegrationConnection{}, authErr
		}
		if !ok {
			return models.IntegrationConnection{}, models.ErrForbidden
		}
	}
	if in.DepartmentID.Valid {
		var ok bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM departments WHERE department_uuid=$1 AND company_uuid=$2 AND archived_at IS NULL)`, in.DepartmentID.UUID, in.CompanyID.UUID).Scan(&ok)
		if err != nil || !ok {
			return models.IntegrationConnection{}, models.ErrInvalidCallPlacement
		}
	}
	if in.FolderID.Valid {
		placement := resolvedIngestPlacement{Scope: "personal", UserID: ownerUser}
		if in.CompanyID.Valid {
			placement = resolvedIngestPlacement{Scope: "company", CompanyID: in.CompanyID}
		}
		if in.DepartmentID.Valid {
			placement.Scope = "department"
			placement.DepartmentID = in.DepartmentID
		}
		ok, folderErr := folderMatchesPlacement(ctx, tx, in.FolderID.UUID, placement)
		if folderErr != nil {
			return models.IntegrationConnection{}, folderErr
		}
		if !ok {
			return models.IntegrationConnection{}, models.ErrInvalidCallPlacement
		}
	}
	id, _ := uuid.NewV7()
	connectionStatus := "active"
	if in.Provider == "bitrix24" {
		connectionStatus = "draft"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_connections(connection_uuid,application_uuid,company_uuid,department_uuid,folder_uuid,created_by_user_uuid,name,provider,status,disable_policy,allow_folder_override,settings) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, in.ApplicationID, nullUUID(in.CompanyID), nullUUID(in.DepartmentID), nullUUID(in.FolderID), in.ActorID, strings.TrimSpace(in.Name), in.Provider, connectionStatus, in.DisablePolicy, in.AllowFolderOverride, in.Settings)
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	if err = audit(ctx, tx, in.ApplicationID, uuid.NullUUID{UUID: id, Valid: true}, "user", uuid.NullUUID{UUID: in.ActorID, Valid: true}, "connection.created", "connection", id, map[string]any{"environment": environment, "provider": in.Provider}); err != nil {
		return models.IntegrationConnection{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationConnection{}, err
	}
	return r.GetConnection(ctx, id, in.ActorID)
}

func (r *Repository) ListConnections(ctx context.Context, applicationID, actorID uuid.UUID) ([]models.IntegrationConnection, error) {
	if err := r.authorizeApplication(ctx, applicationID, actorID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, connectionSelect+` WHERE c.application_uuid=$1 ORDER BY c.created_at DESC,c.connection_uuid DESC LIMIT 101`, applicationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanConnections(rows)
}

func (r *Repository) GetConnection(ctx context.Context, id, actorID uuid.UUID) (models.IntegrationConnection, error) {
	var item models.IntegrationConnection
	var settings []byte
	var lastEvent, lastSuccess sql.NullTime
	var lastError sql.NullString
	err := r.db.QueryRowContext(ctx, connectionSelect+` WHERE c.connection_uuid=$1 AND (`+actorAccessSQL+`)`, id, actorID).Scan(&item.ID, &item.ApplicationID, &item.CompanyID, &item.DepartmentID, &item.FolderID, &item.CreatedBy, &item.Name, &item.Provider, &item.Status, &item.DisablePolicy, &item.AllowFolderOverride, &item.SettingsVersion, &settings, &lastEvent, &lastSuccess, &lastError, &item.LockVersion, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrIntegrationNotFound
	}
	if err != nil {
		return item, err
	}
	item.Settings = settings
	item.LastEventAt = timePtr(lastEvent)
	item.LastSuccessAt = timePtr(lastSuccess)
	item.LastErrorCode = stringPtr(lastError)
	return item, nil
}

func (r *Repository) ChangeConnectionStatus(ctx context.Context, id, actorID uuid.UUID, status string, expected int64) (models.IntegrationConnection, error) {
	if status != "active" && status != "disabled" && status != "revoked" {
		return models.IntegrationConnection{}, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var appID uuid.UUID
	var current string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT c.application_uuid,c.status,c.lock_version FROM integration_connections c JOIN developer_applications a USING(application_uuid) WHERE c.connection_uuid=$1 AND (`+actorAccessSQL+`) FOR UPDATE`, id, actorID).Scan(&appID, &current, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationConnection{}, models.ErrIntegrationNotFound
	}
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	if expected > 0 && version != expected {
		return models.IntegrationConnection{}, models.ErrIntegrationConflict
	}
	if current == "revoked" && status != "revoked" {
		return models.IntegrationConnection{}, models.ErrIntegrationConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_connections SET status=$2,disabled_at=CASE WHEN $2='disabled' THEN now() ELSE NULL END,revoked_at=CASE WHEN $2='revoked' THEN now() ELSE NULL END,lock_version=lock_version+1,updated_at=now() WHERE connection_uuid=$1`, id, status)
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	if err = audit(ctx, tx, appID, uuid.NullUUID{UUID: id, Valid: true}, "user", uuid.NullUUID{UUID: actorID, Valid: true}, "connection."+status, "connection", id, map[string]any{"previous_status": current}); err != nil {
		return models.IntegrationConnection{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationConnection{}, err
	}
	return r.GetConnection(ctx, id, actorID)
}

func (r *Repository) UpdateConnection(ctx context.Context, input models.UpdateIntegrationConnectionInput) (models.IntegrationConnection, error) {
	if input.ConnectionID == uuid.Nil || input.ActorID == uuid.Nil || strings.TrimSpace(input.Name) == "" || input.ExpectedLockVersion < 1 || (input.DisablePolicy != "continue" && input.DisablePolicy != "pause" && input.DisablePolicy != "cancel") || !json.Valid(input.Settings) {
		return models.IntegrationConnection{}, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var appID uuid.UUID
	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT c.application_uuid,c.lock_version FROM integration_connections c JOIN developer_applications a USING(application_uuid) WHERE c.connection_uuid=$1 AND c.status<>'revoked' AND (`+actorAccessSQL+`) FOR UPDATE`, input.ConnectionID, input.ActorID).Scan(&appID, &currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationConnection{}, models.ErrIntegrationNotFound
	}
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	if currentVersion != input.ExpectedLockVersion {
		return models.IntegrationConnection{}, models.ErrIntegrationConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_connections SET name=$2,disable_policy=$3,settings=$4,settings_version=settings_version+1,lock_version=lock_version+1,updated_at=now() WHERE connection_uuid=$1`, input.ConnectionID, strings.TrimSpace(input.Name), input.DisablePolicy, input.Settings)
	if err != nil {
		return models.IntegrationConnection{}, err
	}
	if err = audit(ctx, tx, appID, uuid.NullUUID{UUID: input.ConnectionID, Valid: true}, "user", uuid.NullUUID{UUID: input.ActorID, Valid: true}, "connection.updated", "connection", input.ConnectionID, map[string]any{"previous_lock_version": currentVersion}); err != nil {
		return models.IntegrationConnection{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationConnection{}, err
	}
	return r.GetConnection(ctx, input.ConnectionID, input.ActorID)
}

func (r *Repository) AcceptURLIngest(ctx context.Context, p models.IntegrationPrincipal, in models.IngestCallInput, idempotency string, requestHash [32]byte, locator []byte) (models.IngestItem, bool, error) {
	return r.acceptIngest(ctx, p, in, idempotency, requestHash, locator, "url")
}
func (r *Repository) AcceptUploadIngest(ctx context.Context, p models.IntegrationPrincipal, in models.IngestCallInput, idempotency string, requestHash [32]byte, locator []byte) (models.IngestItem, bool, error) {
	return r.acceptIngest(ctx, p, in, idempotency, requestHash, locator, "upload")
}
func (r *Repository) acceptIngest(ctx context.Context, p models.IntegrationPrincipal, in models.IngestCallInput, idempotency string, requestHash [32]byte, locator []byte, sourceKind string) (models.IngestItem, bool, error) {
	if in.AIMode == "" {
		if p.Environment == "sandbox" {
			in.AIMode = "mock"
		} else {
			in.AIMode = "real"
		}
	}
	if strings.TrimSpace(idempotency) == "" || (in.SchemaVersion != 1 && in.SchemaVersion != 2) || strings.TrimSpace(in.ExternalEventID) == "" || strings.TrimSpace(in.ExternalCallID) == "" || strings.TrimSpace(in.Title) == "" {
		return models.IngestItem{}, false, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IngestItem{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var appID, accountID uuid.UUID
	var status string
	var settingsVersion int64
	var company, department, folder, ownerUser, ownerCompany uuid.NullUUID
	var createdBy uuid.UUID
	var ownerType string
	var allowFolderOverride bool
	err = tx.QueryRowContext(ctx, `SELECT c.application_uuid,a.billing_account_uuid,c.status,c.settings_version,c.company_uuid,c.department_uuid,c.folder_uuid,c.created_by_user_uuid,c.allow_folder_override,a.owner_type,a.user_uuid,a.company_uuid FROM integration_connections c JOIN developer_applications a USING(application_uuid) WHERE c.connection_uuid=$1 AND c.application_uuid=$2 FOR UPDATE`, p.ConnectionUUID, p.ApplicationUUID).Scan(&appID, &accountID, &status, &settingsVersion, &company, &department, &folder, &createdBy, &allowFolderOverride, &ownerType, &ownerUser, &ownerCompany)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IngestItem{}, false, models.ErrIntegrationNotFound
	}
	if err != nil {
		return models.IngestItem{}, false, err
	}
	if status != "active" {
		return models.IngestItem{}, false, models.ErrIntegrationDisabled
	}
	if accountID != p.BillingAccountUUID {
		return models.IngestItem{}, false, models.ErrForbidden
	}
	placement, err := resolveIngestPlacement(ctx, tx, in.Destination, ownerType, ownerUser, ownerCompany, company, department, folder, createdBy, allowFolderOverride, p.Environment == "sandbox")
	if err != nil {
		return models.IngestItem{}, false, err
	}
	// The call belongs to the person who made it. For a company connection that
	// means the mapped portal user; an unmapped call is refused instead of
	// quietly landing on whoever connected the portal.
	uploader, mappedDepartment, err := resolveMappedUploader(ctx, tx, p.ConnectionUUID, placement.CompanyID, in.Participants)
	if err != nil {
		return models.IngestItem{}, false, err
	}
	if mappedDepartment.Valid {
		placement.Scope = "department"
		placement.DepartmentID = mappedDepartment
	}
	placementJSON, _ := json.Marshal(placement)
	requestHash = sha256.Sum256(append(requestHash[:], placementJSON...))
	var existing models.IngestItem
	var existingHash []byte
	err = tx.QueryRowContext(ctx, `SELECT ingest_item_uuid,request_sha256 FROM ingest_items WHERE connection_uuid=$1 AND (idempotency_key=$2 OR external_call_id=$3)`, p.ConnectionUUID, idempotency, in.ExternalCallID).Scan(&existing.ID, &existingHash)
	if err == nil {
		if !equalHash(existingHash, requestHash[:]) {
			return models.IngestItem{}, false, models.ErrIntegrationConflict
		}
		item, getErr := getIngestTx(ctx, tx, existing.ID, p)
		return item, true, getErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.IngestItem{}, false, err
	}
	eventID, _ := uuid.NewV7()
	itemID, _ := uuid.NewV7()
	payload, _ := json.Marshal(in)
	payloadHash := sha256.Sum256(payload)
	metadata, _ := json.Marshal(in.Metadata)
	placementSnapshot, _ := json.Marshal(placement)
	instructionMode := strings.TrimSpace(in.InstructionMode)
	if instructionMode == "" {
		instructionMode = "folder"
	}
	instructionSnapshot, _ := json.Marshal(map[string]any{"mode": instructionMode, "inherit_scope_instructions": instructionMode == "scope_and_folder"})
	_, err = tx.ExecContext(ctx, `INSERT INTO ingest_events(event_uuid,connection_uuid,external_event_id,event_type,schema_version,payload_redacted,payload_sha256,accepted) VALUES($1,$2,$3,'call.created',$4,$5,$6,true)`, eventID, p.ConnectionUUID, in.ExternalEventID, in.SchemaVersion, redactPayload(in), payloadHash[:])
	if err != nil {
		return models.IngestItem{}, false, err
	}
	expires := time.Now().UTC().Add(15 * time.Minute)
	billingEnvironment := "production"
	if p.Environment == "sandbox" && in.AIMode == "mock" {
		billingEnvironment = "sandbox"
	}
	sourceRef := makeSourceRef(p.ConnectionUUID, in.ExternalCallID)
	_, err = tx.ExecContext(ctx, `INSERT INTO ingest_items(ingest_item_uuid,connection_uuid,event_uuid,application_uuid,billing_account_uuid,key_uuid,external_call_id,source_ref,idempotency_key,request_sha256,source_kind,recording_locator_ciphertext,recording_locator_key_version,locator_expires_at,title,original_filename,occurred_at,metadata_redacted,status,stage,connection_settings_version,placement_snapshot,instruction_snapshot,ai_mode,billing_environment,destination_scope,destination_user_uuid,destination_company_uuid,destination_department_uuid,destination_folder_uuid,placement_source,uploader_user_uuid) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,'received','received',$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30)`, itemID, p.ConnectionUUID, eventID, appID, accountID, nullUUID(uuid.NullUUID{UUID: p.KeyUUID, Valid: p.KeyUUID != uuid.Nil}), in.ExternalCallID, sourceRef, idempotency, requestHash[:], sourceKind, locator, r.cipher.Version(), expires, strings.TrimSpace(in.Title), nullableString(in.OriginalFilename), in.OccurredAt, metadata, settingsVersion, placementSnapshot, instructionSnapshot, in.AIMode, billingEnvironment, placement.Scope, nullUUID(placement.UserID), nullUUID(placement.CompanyID), nullUUID(placement.DepartmentID), nullUUID(placement.FolderID), placement.Source, nullUUID(uploader))
	if err != nil {
		return models.IngestItem{}, false, err
	}
	if err = audit(ctx, tx, appID, uuid.NullUUID{UUID: p.ConnectionUUID, Valid: true}, "service_account", uuid.NullUUID{UUID: p.ServiceAccountUUID, Valid: true}, "ingest.accepted", "ingest_item", itemID, map[string]any{"external_call_id": in.ExternalCallID}); err != nil {
		return models.IngestItem{}, false, err
	}
	if err = outbox(ctx, tx, appID, p.ConnectionUUID, "ingest.accepted", itemID, map[string]any{"ingest_item_uuid": itemID, "status": "received"}); err != nil {
		return models.IngestItem{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE integration_connections SET last_event_at=now(),updated_at=now() WHERE connection_uuid=$1`, p.ConnectionUUID); err != nil {
		return models.IngestItem{}, false, err
	}
	item, getErr := getIngestTx(ctx, tx, itemID, p)
	if getErr != nil {
		return models.IngestItem{}, false, getErr
	}
	if err = tx.Commit(); err != nil {
		return models.IngestItem{}, false, err
	}
	return item, false, nil
}

type resolvedIngestPlacement struct {
	Scope        string        `json:"scope"`
	UserID       uuid.NullUUID `json:"user_uuid,omitempty"`
	CompanyID    uuid.NullUUID `json:"company_uuid,omitempty"`
	DepartmentID uuid.NullUUID `json:"department_uuid,omitempty"`
	FolderID     uuid.NullUUID `json:"folder_uuid"`
	Source       string        `json:"source"`
}

func resolveIngestPlacement(ctx context.Context, tx *sql.Tx, requested *models.IngestDestination, ownerType string, ownerUser, ownerCompany, connectionCompany, connectionDepartment, connectionFolder uuid.NullUUID, createdBy uuid.UUID, allowOverride, sandbox bool) (resolvedIngestPlacement, error) {
	p := resolvedIngestPlacement{Source: "connection_default"}
	if connectionCompany.Valid {
		p.Scope = "company"
		p.CompanyID = connectionCompany
		if connectionDepartment.Valid {
			p.Scope = "department"
			p.DepartmentID = connectionDepartment
		}
	} else {
		if ownerType != "user" || !ownerUser.Valid {
			return p, models.ErrInvalidCallPlacement
		}
		p.Scope = "personal"
		p.UserID = ownerUser
	}
	if sandbox {
		folderID, err := ensureSystemIngestFolder(ctx, tx, p, createdBy, "sandbox_test", "Тестовые звонки", "Системная папка для звонков из тестовой среды")
		if err != nil {
			return p, err
		}
		p.FolderID = uuid.NullUUID{UUID: folderID, Valid: true}
		p.Source = "system_sandbox"
		return p, nil
	}

	if requested != nil {
		p.Source = "request_override"
		switch requested.Scope {
		case "personal":
			if ownerType != "user" || !ownerUser.Valid || requested.CompanyID != uuid.Nil || requested.DepartmentID != uuid.Nil {
				return p, models.ErrForbidden
			}
			p.Scope, p.UserID = "personal", ownerUser
			p.CompanyID, p.DepartmentID = uuid.NullUUID{}, uuid.NullUUID{}
		case "company":
			if ownerType != "company" || !ownerCompany.Valid || requested.CompanyID != ownerCompany.UUID || requested.DepartmentID != uuid.Nil || connectionDepartment.Valid {
				return p, models.ErrForbidden
			}
			p.Scope = "company"
			p.UserID = uuid.NullUUID{}
			p.CompanyID = ownerCompany
			p.DepartmentID = uuid.NullUUID{}
		case "department":
			if ownerType != "company" || !ownerCompany.Valid || requested.CompanyID != ownerCompany.UUID || requested.DepartmentID == uuid.Nil {
				return p, models.ErrForbidden
			}
			if connectionDepartment.Valid && connectionDepartment.UUID != requested.DepartmentID {
				return p, models.ErrForbidden
			}
			var active bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM departments WHERE department_uuid=$1 AND company_uuid=$2 AND archived_at IS NULL)`, requested.DepartmentID, ownerCompany.UUID).Scan(&active); err != nil || !active {
				return p, models.ErrInvalidCallPlacement
			}
			p.Scope = "department"
			p.UserID = uuid.NullUUID{}
			p.CompanyID = ownerCompany
			p.DepartmentID = uuid.NullUUID{UUID: requested.DepartmentID, Valid: true}
		default:
			return p, models.ErrInvalidCallPlacement
		}
	}

	requestedFolder := uuid.Nil
	if requested != nil {
		requestedFolder = requested.FolderID
	}
	if requestedFolder != uuid.Nil {
		if !allowOverride && (!connectionFolder.Valid || connectionFolder.UUID != requestedFolder) {
			return p, models.ErrForbidden
		}
		valid, err := folderMatchesPlacement(ctx, tx, requestedFolder, p)
		if err != nil || !valid {
			return p, models.ErrInvalidCallPlacement
		}
		p.FolderID = uuid.NullUUID{UUID: requestedFolder, Valid: true}
		return p, nil
	}
	if requested == nil && connectionFolder.Valid {
		valid, err := folderMatchesPlacement(ctx, tx, connectionFolder.UUID, p)
		if err != nil || !valid {
			return p, models.ErrInvalidCallPlacement
		}
		p.FolderID = connectionFolder
		return p, nil
	}

	folderID, err := ensureExternalFolder(ctx, tx, p, createdBy)
	if err != nil {
		return p, err
	}
	p.FolderID = uuid.NullUUID{UUID: folderID, Valid: true}
	p.Source = "system_external"
	return p, nil
}

func folderMatchesPlacement(ctx context.Context, tx *sql.Tx, folderID uuid.UUID, p resolvedIngestPlacement) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM call_folders f
		WHERE f.folder_uuid=$1 AND f.deleted_at IS NULL AND f.scope=$2
		  AND f.user_uuid IS NOT DISTINCT FROM $3
		  AND f.company_uuid IS NOT DISTINCT FROM $4
		  AND f.department_uuid IS NOT DISTINCT FROM $5
	)`, folderID, p.Scope, nullUUID(p.UserID), nullUUID(p.CompanyID), nullUUID(p.DepartmentID)).Scan(&ok)
	return ok, err
}

func ensureExternalFolder(ctx context.Context, tx *sql.Tx, p resolvedIngestPlacement, createdBy uuid.UUID) (uuid.UUID, error) {
	return ensureSystemIngestFolder(ctx, tx, p, createdBy, "external_ingest", "Внешняя", "Системная папка для звонков, поступивших через API")
}

func ensureSystemIngestFolder(ctx context.Context, tx *sql.Tx, p resolvedIngestPlacement, createdBy uuid.UUID, systemType, name, description string) (uuid.UUID, error) {
	id, _ := uuid.NewV7()
	_, err := tx.ExecContext(ctx, `INSERT INTO call_folders(folder_uuid,scope,user_uuid,company_uuid,department_uuid,name,description,created_by_user_uuid,created_by_actor_type,system_type)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,'system',$9)
		ON CONFLICT DO NOTHING`, id, p.Scope, nullUUID(p.UserID), nullUUID(p.CompanyID), nullUUID(p.DepartmentID), name, description, createdBy, systemType)
	if err != nil {
		return uuid.Nil, err
	}
	err = tx.QueryRowContext(ctx, `SELECT folder_uuid FROM call_folders
		WHERE deleted_at IS NULL AND scope=$1 AND system_type=$2
		  AND user_uuid IS NOT DISTINCT FROM $3
		  AND company_uuid IS NOT DISTINCT FROM $4
		  AND department_uuid IS NOT DISTINCT FROM $5`, p.Scope, systemType, nullUUID(p.UserID), nullUUID(p.CompanyID), nullUUID(p.DepartmentID)).Scan(&id)
	return id, err
}

func (r *Repository) GetIngest(ctx context.Context, id uuid.UUID, p models.IntegrationPrincipal) (models.IngestItem, error) {
	return getIngestRow(r.db.QueryRowContext(ctx, ingestSelect+` WHERE i.ingest_item_uuid=$1 AND i.application_uuid=$2 AND i.connection_uuid=$3`, id, p.ApplicationUUID, p.ConnectionUUID))
}

func (r *Repository) ListDestinations(ctx context.Context, p models.IntegrationPrincipal) ([]models.IntegrationDestination, error) {
	var ownerType string
	var ownerUser, ownerCompany, connectionCompany, connectionDepartment uuid.NullUUID
	err := r.db.QueryRowContext(ctx, `SELECT a.owner_type,a.user_uuid,a.company_uuid,c.company_uuid,c.department_uuid
		FROM integration_connections c JOIN developer_applications a USING(application_uuid)
		WHERE c.connection_uuid=$1 AND c.application_uuid=$2 AND c.status='active' AND a.status='active'`, p.ConnectionUUID, p.ApplicationUUID).
		Scan(&ownerType, &ownerUser, &ownerCompany, &connectionCompany, &connectionDepartment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrIntegrationNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerType == "user" {
		return []models.IntegrationDestination{{Scope: "personal", UserID: ownerUser, Name: "Личный профиль"}}, nil
	}
	if !ownerCompany.Valid || !connectionCompany.Valid || ownerCompany.UUID != connectionCompany.UUID {
		return nil, models.ErrForbidden
	}
	if connectionDepartment.Valid {
		var name string
		if err = r.db.QueryRowContext(ctx, `SELECT name FROM departments WHERE department_uuid=$1 AND company_uuid=$2 AND archived_at IS NULL`, connectionDepartment.UUID, ownerCompany.UUID).Scan(&name); err != nil {
			return nil, models.ErrInvalidCallPlacement
		}
		return []models.IntegrationDestination{{Scope: "department", CompanyID: ownerCompany, DepartmentID: connectionDepartment, Name: name}}, nil
	}
	var companyName string
	if err = r.db.QueryRowContext(ctx, `SELECT name FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL`, ownerCompany.UUID).Scan(&companyName); err != nil {
		return nil, models.ErrForbidden
	}
	out := []models.IntegrationDestination{{Scope: "company", CompanyID: ownerCompany, Name: companyName}}
	rows, err := r.db.QueryContext(ctx, `SELECT department_uuid,name FROM departments WHERE company_uuid=$1 AND archived_at IS NULL ORDER BY name,department_uuid`, ownerCompany.UUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out = append(out, models.IntegrationDestination{Scope: "department", CompanyID: ownerCompany, DepartmentID: uuid.NullUUID{UUID: id, Valid: true}, Name: name})
	}
	return out, rows.Err()
}

func (r *Repository) ListFolders(ctx context.Context, p models.IntegrationPrincipal, scope string, companyID, departmentID uuid.UUID) ([]models.IntegrationFolder, error) {
	destinations, err := r.ListDestinations(ctx, p)
	if err != nil {
		return nil, err
	}
	var selected *models.IntegrationDestination
	for i := range destinations {
		d := &destinations[i]
		if d.Scope == scope && (!d.CompanyID.Valid || d.CompanyID.UUID == companyID) && (!d.DepartmentID.Valid || d.DepartmentID.UUID == departmentID) {
			selected = d
			break
		}
	}
	if selected == nil {
		return nil, models.ErrForbidden
	}
	rows, err := r.db.QueryContext(ctx, `SELECT folder_uuid,name,system_type FROM call_folders
		WHERE deleted_at IS NULL AND scope=$1
		  AND user_uuid IS NOT DISTINCT FROM $2
		  AND company_uuid IS NOT DISTINCT FROM $3
		  AND department_uuid IS NOT DISTINCT FROM $4
		ORDER BY (system_type='external_ingest') DESC,name,folder_uuid`, scope, nullUUID(selected.UserID), nullUUID(selected.CompanyID), nullUUID(selected.DepartmentID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []models.IntegrationFolder
	for rows.Next() {
		var item models.IntegrationFolder
		var system sql.NullString
		if err = rows.Scan(&item.ID, &item.Name, &system); err != nil {
			return nil, err
		}
		if system.Valid {
			item.SystemType = &system.String
			item.IsSystem = true
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetCall(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IntegrationCallView, error) {
	var item models.IntegrationCallView
	err := r.db.QueryRowContext(ctx, `SELECT c.call_uuid,c.title,c.status,c.duration_seconds,c.visibility_scope,c.company_uuid,c.department_uuid,i.destination_folder_uuid,c.created_at,c.updated_at,i.external_call_id,i.source_ref
		FROM calls c JOIN ingest_items i ON i.call_uuid=c.call_uuid
		WHERE c.call_uuid=$1 AND i.application_uuid=$2 AND i.connection_uuid=$3`, id, p.ApplicationUUID, p.ConnectionUUID).
		Scan(&item.ID, &item.Title, &item.Status, &item.DurationSeconds, &item.VisibilityScope, &item.CompanyID, &item.DepartmentID, &item.FolderID, &item.CreatedAt, &item.UpdatedAt, &item.ExternalCallID, &item.SourceRef)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrIngestNotFound
	}
	return item, err
}

func (r *Repository) GetCallBySourceRef(ctx context.Context, p models.IntegrationPrincipal, sourceRef string) (models.IntegrationCallView, error) {
	var id uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT call_uuid FROM ingest_items WHERE source_ref=$1 AND application_uuid=$2 AND connection_uuid=$3 AND call_uuid IS NOT NULL`, sourceRef, p.ApplicationUUID, p.ConnectionUUID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationCallView{}, models.ErrIngestNotFound
	}
	if err != nil {
		return models.IntegrationCallView{}, err
	}
	return r.GetCall(ctx, p, id)
}

func (r *Repository) ListCalls(ctx context.Context, p models.IntegrationPrincipal, filter models.IntegrationCallFilter) ([]models.IntegrationCallView, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.call_uuid,c.title,c.status,c.duration_seconds,c.visibility_scope,c.company_uuid,c.department_uuid,i.destination_folder_uuid,c.created_at,c.updated_at,i.external_call_id,i.source_ref
		FROM calls c JOIN ingest_items i ON i.call_uuid=c.call_uuid
		WHERE i.application_uuid=$1 AND i.connection_uuid=$2
		AND ($3::timestamptz IS NULL OR c.updated_at >= $3)
		AND ($4::timestamptz IS NULL OR c.created_at >= $4)
		AND ($5::timestamptz IS NULL OR c.created_at < $5)
		AND ($6='' OR c.status=$6)
		AND ($7::timestamptz IS NULL OR (c.updated_at,c.call_uuid) < ($7,$8))
		ORDER BY c.updated_at DESC,c.call_uuid DESC LIMIT $9`, p.ApplicationUUID, p.ConnectionUUID, filter.UpdatedSince, filter.From, filter.To, filter.Status, filter.CursorUpdatedAt, filter.CursorID, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.IntegrationCallView, 0, filter.Limit)
	for rows.Next() {
		var item models.IntegrationCallView
		if err = rows.Scan(&item.ID, &item.Title, &item.Status, &item.DurationSeconds, &item.VisibilityScope, &item.CompanyID, &item.DepartmentID, &item.FolderID, &item.CreatedAt, &item.UpdatedAt, &item.ExternalCallID, &item.SourceRef); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetTranscription(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IntegrationTranscriptionView, error) {
	var item models.IntegrationTranscriptionView
	var text, language sql.NullString
	var segments, words []byte
	err := r.db.QueryRowContext(ctx, `SELECT t.transcription_uuid,t.call_uuid,t.status,t.text,t.segments,t.words,t.language,t.updated_at
		FROM call_transcriptions t JOIN ingest_items i ON i.call_uuid=t.call_uuid
		WHERE t.call_uuid=$1 AND i.application_uuid=$2 AND i.connection_uuid=$3`, id, p.ApplicationUUID, p.ConnectionUUID).
		Scan(&item.ID, &item.CallID, &item.Status, &text, &segments, &words, &language, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrIngestNotFound
	}
	if err != nil {
		return item, err
	}
	item.Text = stringPtr(text)
	item.Language = stringPtr(language)
	item.Segments = segments
	item.Words = words
	return item, nil
}

func (r *Repository) GetAnalysis(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IntegrationAnalysisView, error) {
	var item models.IntegrationAnalysisView
	var model, resultText sql.NullString
	var resultJSON []byte
	err := r.db.QueryRowContext(ctx, `SELECT a.analysis_uuid,a.call_uuid,a.status,a.model,a.result_json,a.result_text,a.updated_at
		FROM call_analyses a JOIN ingest_items i ON i.call_uuid=a.call_uuid
		WHERE a.call_uuid=$1 AND i.application_uuid=$2 AND i.connection_uuid=$3`, id, p.ApplicationUUID, p.ConnectionUUID).
		Scan(&item.ID, &item.CallID, &item.Status, &model, &resultJSON, &resultText, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrIngestNotFound
	}
	if err != nil {
		return item, err
	}
	item.Model = stringPtr(model)
	item.ResultJSON = resultJSON
	item.ResultText = stringPtr(resultText)
	return item, nil
}

func (r *Repository) GetUsage(ctx context.Context, p models.IntegrationPrincipal, now time.Time) (models.IntegrationUsageView, error) {
	result := models.IntegrationUsageView{Environment: p.Environment, MaximumUploadBytes: 500 << 20}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(sum(x.balance),0) FROM (
		SELECT COALESCE(sum(lp.amount_credits),0) AS balance
		FROM credit_ledger_accounts la JOIN credit_grants g USING(credit_grant_uuid)
		LEFT JOIN credit_ledger_postings lp USING(credit_ledger_account_uuid)
		WHERE la.billing_account_uuid=$1 AND la.environment=$2 AND la.account_type='customer_available'
		AND (g.application_uuid IS NULL OR g.application_uuid=$3) AND (g.expires_at IS NULL OR g.expires_at>$4)
		GROUP BY la.credit_ledger_account_uuid) x`, p.BillingAccountUUID, p.Environment, p.ApplicationUUID, now).Scan(&result.AvailableCredits)
	if err != nil {
		return result, err
	}
	var permanent, temporary sql.NullInt64
	var starts, ends sql.NullTime
	err = r.db.QueryRowContext(ctx, `SELECT permanent_credit_limit,temporary_credit_limit,temporary_limit_starts_at,temporary_limit_ends_at FROM integration_api_keys WHERE key_uuid=$1`, p.KeyUUID).Scan(&permanent, &temporary, &starts, &ends)
	if errors.Is(err, sql.ErrNoRows) {
		return result, models.ErrInvalidAPIKey
	}
	if err != nil {
		return result, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END),0) FROM usage_operations WHERE key_uuid=$1 AND status IN ('reserved','provider_running','settled','reconciling')`, p.KeyUUID).Scan(&result.KeyCreditsUsed)
	if err != nil {
		return result, err
	}
	if permanent.Valid {
		value := permanent.Int64
		remaining := maxInt64(0, value-result.KeyCreditsUsed)
		result.PermanentCreditLimit, result.PermanentCreditsRemaining = &value, &remaining
	}
	if temporary.Valid && starts.Valid && ends.Valid {
		var used int64
		if err = r.db.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END),0) FROM usage_operations WHERE key_uuid=$1 AND started_at >= $2 AND started_at < $3 AND status IN ('reserved','provider_running','settled','reconciling')`, p.KeyUUID, starts.Time, ends.Time).Scan(&used); err != nil {
			return result, err
		}
		limit, remaining, start, end := temporary.Int64, maxInt64(0, temporary.Int64-used), starts.Time, ends.Time
		result.TemporaryCreditLimit, result.TemporaryCreditsUsed, result.TemporaryCreditsRemaining, result.TemporaryLimitStartsAt, result.TemporaryLimitEndsAt = &limit, &used, &remaining, &start, &end
	}
	err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM ingest_items WHERE key_uuid=$1 AND status IN ('received','processing')`, p.KeyUUID).Scan(&result.ActiveIngestItems)
	return result, err
}

func (r *Repository) ListIngest(ctx context.Context, connectionID, actorID uuid.UUID, limit, offset int) ([]models.IngestItem, int, error) {
	var appID uuid.UUID
	if err := r.db.QueryRowContext(ctx, `SELECT application_uuid FROM integration_connections WHERE connection_uuid=$1`, connectionID).Scan(&appID); err != nil {
		return nil, 0, models.ErrIntegrationNotFound
	}
	if err := r.authorizeApplication(ctx, appID, actorID); err != nil {
		return nil, 0, err
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM ingest_items WHERE connection_uuid=$1`, connectionID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, ingestSelect+` WHERE i.connection_uuid=$1 ORDER BY i.created_at DESC LIMIT $2 OFFSET $3`, connectionID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var items []models.IngestItem
	for rows.Next() {
		item, scanErr := getIngestRow(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Repository) RetryIngest(ctx context.Context, id, actorID uuid.UUID) (models.IngestItem, error) {
	return r.commandIngest(ctx, id, actorID, "retry")
}

func (r *Repository) CancelIngest(ctx context.Context, id, actorID uuid.UUID) (models.IngestItem, error) {
	return r.commandIngest(ctx, id, actorID, "cancel")
}

func (r *Repository) commandIngest(ctx context.Context, id, actorID uuid.UUID, command string) (models.IngestItem, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IngestItem{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var item models.IngestItem
	err = tx.QueryRowContext(ctx, `SELECT i.application_uuid,i.connection_uuid,i.status FROM ingest_items i WHERE i.ingest_item_uuid=$1 FOR UPDATE`, id).Scan(&item.ApplicationID, &item.ConnectionID, &item.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrIngestNotFound
	}
	if err != nil {
		return item, err
	}
	if err = r.authorizeApplicationTx(ctx, tx, item.ApplicationID, actorID); err != nil {
		return item, err
	}
	if command == "retry" {
		if item.Status != "failed" {
			return item, models.ErrIntegrationConflict
		}
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE ingest_items SET status='received',stage='received',attempts=0,available_at=now(),error_code=NULL,error_message_safe=NULL,cancelled_at=NULL,updated_at=now() WHERE ingest_item_uuid=$1 AND recording_locator_ciphertext IS NOT NULL`, id)
		if err == nil {
			if changed, rowsErr := result.RowsAffected(); rowsErr != nil {
				err = rowsErr
			} else if changed != 1 {
				return item, models.ErrIntegrationConflict
			}
		}
	} else {
		if item.Status != "received" && item.Status != "retry_wait" && item.Status != "failed" {
			return item, models.ErrIntegrationConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE ingest_items SET status='cancelled',stage='cancelled',cancelled_at=now(),updated_at=now() WHERE ingest_item_uuid=$1`, id)
	}
	if err != nil {
		return item, err
	}
	auditID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_audit_events(audit_event_uuid,application_uuid,connection_uuid,actor_type,actor_uuid,event_type,entity_type,entity_uuid,metadata) VALUES($1,$2,$3,'user',$4,$5,'ingest_item',$6,'{}')`, auditID, item.ApplicationID, item.ConnectionID, actorID, "ingest."+command, id)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return r.GetIngestForActor(ctx, id, actorID)
}

func (r *Repository) GetIngestForActor(ctx context.Context, id, actorID uuid.UUID) (models.IngestItem, error) {
	var item models.IngestItem
	item, err := getIngestRow(r.db.QueryRowContext(ctx, ingestSelect+` WHERE i.ingest_item_uuid=$1`, id))
	if err != nil {
		return item, err
	}
	if err = r.authorizeApplication(ctx, item.ApplicationID, actorID); err != nil {
		return models.IngestItem{}, err
	}
	return item, nil
}

func (r *Repository) authorizeApplicationTx(ctx context.Context, tx *sql.Tx, app, actor uuid.UUID) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM developer_applications a WHERE a.application_uuid=$1 AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members m WHERE m.company_uuid=a.company_uuid AND m.user_uuid=$2 AND m.status='active' AND m.role IN ('company_manager','company_deputy'))))))`, app, actor).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return models.ErrForbidden
	}
	return nil
}

func (r *Repository) ListAudit(ctx context.Context, connectionID, actorID uuid.UUID, limit, offset int) ([]models.IntegrationAuditEvent, int, error) {
	var appID uuid.UUID
	if err := r.db.QueryRowContext(ctx, `SELECT application_uuid FROM integration_connections WHERE connection_uuid=$1`, connectionID).Scan(&appID); err != nil {
		return nil, 0, models.ErrIntegrationNotFound
	}
	if err := r.authorizeApplication(ctx, appID, actorID); err != nil {
		return nil, 0, err
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM integration_audit_events WHERE connection_uuid=$1`, connectionID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT audit_event_uuid,application_uuid,connection_uuid,actor_type,actor_uuid,event_type,entity_type,entity_uuid,metadata_safe,request_id,created_at FROM integration_audit_events WHERE connection_uuid=$1 ORDER BY created_at DESC,audit_event_uuid DESC LIMIT $2 OFFSET $3`, connectionID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var events []models.IntegrationAuditEvent
	for rows.Next() {
		var event models.IntegrationAuditEvent
		if err = rows.Scan(&event.ID, &event.ApplicationID, &event.ConnectionID, &event.ActorType, &event.ActorID, &event.EventType, &event.EntityType, &event.EntityID, &event.Metadata, &event.RequestID, &event.CreatedAt); err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, total, rows.Err()
}

func (r *Repository) authorizeApplication(ctx context.Context, app, actor uuid.UUID) error {
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM developer_applications a WHERE a.application_uuid=$1 AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members m WHERE m.company_uuid=a.company_uuid AND m.user_uuid=$2 AND m.status='active' AND m.role IN ('company_manager','company_deputy'))))))`, app, actor).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return models.ErrForbidden
	}
	return nil
}

const connectionSelect = `SELECT c.connection_uuid,c.application_uuid,c.company_uuid,c.department_uuid,c.folder_uuid,c.created_by_user_uuid,c.name,c.provider,c.status,c.disable_policy,c.allow_folder_override,c.settings_version,c.settings,c.last_event_at,c.last_success_at,c.last_error_code,c.lock_version,c.created_at,c.updated_at FROM integration_connections c JOIN developer_applications a USING(application_uuid)`
const actorAccessSQL = `(a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))))`
const ingestSelect = `SELECT i.ingest_item_uuid,i.application_uuid,i.connection_uuid,i.event_uuid,i.billing_account_uuid,i.external_call_id,i.source_ref,i.key_uuid,i.idempotency_key,i.source_kind,i.title,i.original_filename,i.occurred_at,i.metadata_redacted,i.status,i.stage,i.attempts,i.max_attempts,i.available_at,i.call_uuid,i.error_code,i.error_message_safe,i.created_at,i.updated_at,i.completed_at,i.cancelled_at,i.ai_mode,i.billing_environment,i.destination_scope,i.destination_user_uuid,i.destination_company_uuid,i.destination_department_uuid,i.destination_folder_uuid,i.placement_source,COALESCE((i.instruction_snapshot->>'inherit_scope_instructions')::boolean,false) FROM ingest_items i`

type rowScanner interface{ Scan(...any) error }

func getIngestRow(row rowScanner) (models.IngestItem, error) {
	var i models.IngestItem
	var original, errorCode, errorMessage sql.NullString
	var occurred, completed, cancelled sql.NullTime
	var metadata []byte
	err := row.Scan(&i.ID, &i.ApplicationID, &i.ConnectionID, &i.EventID, &i.BillingAccountID, &i.ExternalCallID, &i.SourceRef, &i.KeyID, &i.IdempotencyKey, &i.SourceKind, &i.Title, &original, &occurred, &metadata, &i.Status, &i.Stage, &i.Attempts, &i.MaxAttempts, &i.AvailableAt, &i.CallID, &errorCode, &errorMessage, &i.CreatedAt, &i.UpdatedAt, &completed, &cancelled, &i.AIMode, &i.BillingEnvironment, &i.DestinationScope, &i.DestinationUserID, &i.DestinationCompanyID, &i.DestinationDepartmentID, &i.DestinationFolderID, &i.PlacementSource, &i.InheritScopeInstructions)
	if errors.Is(err, sql.ErrNoRows) {
		return i, models.ErrIngestNotFound
	}
	if err != nil {
		return i, err
	}
	i.OriginalFilename = stringPtr(original)
	i.OccurredAt = timePtr(occurred)
	i.Metadata = metadata
	i.ErrorCode = stringPtr(errorCode)
	i.ErrorMessage = stringPtr(errorMessage)
	i.CompletedAt = timePtr(completed)
	i.CancelledAt = timePtr(cancelled)
	return i, nil
}
func getIngestTx(ctx context.Context, tx *sql.Tx, id uuid.UUID, p models.IntegrationPrincipal) (models.IngestItem, error) {
	return getIngestRow(tx.QueryRowContext(ctx, ingestSelect+` WHERE i.ingest_item_uuid=$1 AND i.application_uuid=$2 AND i.connection_uuid=$3`, id, p.ApplicationUUID, p.ConnectionUUID))
}
func scanConnections(rows *sql.Rows) ([]models.IntegrationConnection, error) {
	var out []models.IntegrationConnection
	for rows.Next() {
		var i models.IntegrationConnection
		var settings []byte
		var le, ls sql.NullTime
		var er sql.NullString
		if err := rows.Scan(&i.ID, &i.ApplicationID, &i.CompanyID, &i.DepartmentID, &i.FolderID, &i.CreatedBy, &i.Name, &i.Provider, &i.Status, &i.DisablePolicy, &i.AllowFolderOverride, &i.SettingsVersion, &settings, &le, &ls, &er, &i.LockVersion, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		i.Settings = settings
		i.LastEventAt = timePtr(le)
		i.LastSuccessAt = timePtr(ls)
		i.LastErrorCode = stringPtr(er)
		out = append(out, i)
	}
	return out, rows.Err()
}
func companyManager(ctx context.Context, tx *sql.Tx, company, actor uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM companies WHERE company_uuid=$1 AND manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active' AND role IN ('company_manager','company_deputy'))`, company, actor).Scan(&ok)
	return ok, err
}
func audit(ctx context.Context, tx *sql.Tx, app uuid.UUID, connection uuid.NullUUID, actorType string, actor uuid.NullUUID, event, entity string, entityID uuid.UUID, metadata any) error {
	id, _ := uuid.NewV7()
	raw, _ := json.Marshal(metadata)
	_, err := tx.ExecContext(ctx, `INSERT INTO integration_audit_events(audit_event_uuid,application_uuid,connection_uuid,actor_type,actor_uuid,event_type,entity_type,entity_uuid,metadata_safe) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, app, nullUUID(connection), actorType, nullUUID(actor), event, entity, entityID, raw)
	return err
}
func outbox(ctx context.Context, tx *sql.Tx, app, connection uuid.UUID, event string, aggregate uuid.UUID, payload any) error {
	id, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
	raw, _ := json.Marshal(payload)
	_, err := tx.ExecContext(ctx, `INSERT INTO integration_outbox(outbox_uuid,application_uuid,connection_uuid,event_id,event_type,aggregate_uuid,payload,status) VALUES($1,$2,$3,$4,$5,$6,$7,'pending')`, id, app, connection, eventID, event, aggregate, raw)
	return err
}
func redactPayload(in models.IngestCallInput) map[string]any {
	return map[string]any{"schema_version": in.SchemaVersion, "external_event_id": in.ExternalEventID, "external_call_id": in.ExternalCallID, "title": in.Title, "original_filename": in.OriginalFilename, "occurred_at": in.OccurredAt, "participants": in.Participants, "metadata": in.Metadata, "destination": in.Destination, "instruction_mode": in.InstructionMode}
}

func nullUUID(v uuid.NullUUID) any {
	if v.Valid {
		return v.UUID
	}
	return nil
}
func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}
func makeSourceRef(connection uuid.UUID, externalID string) string {
	return "vtsrc_" + strings.ReplaceAll(connection.String(), "-", "") + "_" + hex.EncodeToString([]byte(strings.TrimSpace(externalID)))
}
func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	x := v.Time
	return &x
}
func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}
func equalHash(a, b []byte) bool { return len(a) == len(b) && string(a) == string(b) }
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
