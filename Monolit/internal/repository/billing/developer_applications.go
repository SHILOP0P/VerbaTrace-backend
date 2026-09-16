package billing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) CreateDeveloperApplication(ctx context.Context, input models.CreateDeveloperApplicationInput) (models.DeveloperApplication, error) {
	if input.OwnerUUID == uuid.Nil || input.CreatedByUserUUID == uuid.Nil || strings.TrimSpace(input.Name) == "" || (input.OwnerType != "user" && input.OwnerType != "company") || (input.Environment != "sandbox" && input.Environment != "production") {
		return models.DeveloperApplication{}, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.DeveloperApplication{}, err
	}
	defer func() { _ = tx.Rollback() }()
	accountID, _ := uuid.NewV7()
	if input.OwnerType == "user" {
		err = tx.QueryRowContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,user_uuid) VALUES($1,'user',$2) ON CONFLICT (user_uuid) WHERE user_uuid IS NOT NULL DO UPDATE SET updated_at=now() RETURNING billing_account_uuid`, accountID, input.OwnerUUID).Scan(&accountID)
	} else {
		err = tx.QueryRowContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid) VALUES($1,'company',$2) ON CONFLICT (company_uuid) WHERE company_uuid IS NOT NULL DO UPDATE SET updated_at=now() RETURNING billing_account_uuid`, accountID, input.OwnerUUID).Scan(&accountID)
	}
	if err != nil {
		return models.DeveloperApplication{}, fmt.Errorf("ensure developer billing account: %w", err)
	}
	appID, _ := uuid.NewV7()
	var userID, companyID any
	if input.OwnerType == "user" {
		userID = input.OwnerUUID
	} else {
		companyID = input.OwnerUUID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO developer_applications(application_uuid,owner_type,user_uuid,company_uuid,billing_account_uuid,name,environment,capabilities,daily_credit_limit,monthly_credit_limit,max_credits_per_operation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, appID, input.OwnerType, userID, companyID, accountID, strings.TrimSpace(input.Name), input.Environment, input.Capabilities, input.DailyCreditLimit, input.MonthlyCreditLimit, input.MaxCreditsPerOperation)
	if err != nil {
		return models.DeveloperApplication{}, fmt.Errorf("create developer application: %w", err)
	}
	connectionID, _ := uuid.NewV7()
	var connectionCompany any
	if input.OwnerType == "company" {
		connectionCompany = input.OwnerUUID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_connections(connection_uuid,application_uuid,company_uuid,created_by_user_uuid,name,provider,status) VALUES($1,$2,$3,$4,'Default API connection','generic_api','active')`, connectionID, appID, connectionCompany, input.CreatedByUserUUID)
	if err != nil {
		return models.DeveloperApplication{}, fmt.Errorf("create default connection: %w", err)
	}
	// Sandbox funds are isolated server-side test credits and never touch production money.
	if input.Environment == "sandbox" {
		if err = createSandboxGrant(ctx, tx, accountID, appID, 100000); err != nil {
			return models.DeveloperApplication{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return models.DeveloperApplication{}, err
	}
	return models.DeveloperApplication{ID: appID, OwnerType: input.OwnerType, UserUUID: uuid.NullUUID{UUID: input.OwnerUUID, Valid: input.OwnerType == "user"}, CompanyUUID: uuid.NullUUID{UUID: input.OwnerUUID, Valid: input.OwnerType == "company"}, BillingAccountUUID: accountID, Name: strings.TrimSpace(input.Name), Environment: input.Environment, Status: "active", Capabilities: input.Capabilities, DailyCreditLimit: input.DailyCreditLimit, MonthlyCreditLimit: input.MonthlyCreditLimit, MaxCreditsPerOperation: input.MaxCreditsPerOperation, LockVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func (r *Repository) AuthenticateIntegrationKey(ctx context.Context, plaintext, requestEnvironment, requiredScope string) (models.IntegrationPrincipal, error) {
	parts := strings.SplitN(strings.TrimSpace(plaintext), ".", 2)
	if len(parts) != 2 {
		return models.IntegrationPrincipal{}, models.ErrInvalidAPIKey
	}
	prefix := parts[0]
	keyEnvironment := "production"
	if strings.HasPrefix(prefix, "vt_test_") {
		keyEnvironment = "sandbox"
	} else if !strings.HasPrefix(prefix, "vt_live_") {
		return models.IntegrationPrincipal{}, models.ErrInvalidAPIKey
	}
	if keyEnvironment != requestEnvironment {
		return models.IntegrationPrincipal{}, models.ErrAPIKeyEnvironmentMismatch
	}
	digest := sha256.Sum256([]byte(plaintext))
	var principal models.IntegrationPrincipal
	var stored []byte
	var scopesJSON string
	var revoked, expires sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT k.key_uuid,a.application_uuid,c.connection_uuid,sa.service_account_uuid,a.billing_account_uuid,a.environment,to_json(k.scopes)::text,k.secret_hash,k.revoked_at,k.expires_at FROM integration_api_keys k JOIN integration_service_accounts sa USING(service_account_uuid) JOIN developer_applications a USING(application_uuid) JOIN integration_connections c ON c.connection_uuid=sa.connection_uuid WHERE k.prefix=$1 AND a.status='active' AND sa.status='active' AND c.status='active' AND (k.overlap_until IS NULL OR k.overlap_until>now())`, prefix).Scan(&principal.KeyUUID, &principal.ApplicationUUID, &principal.ConnectionUUID, &principal.ServiceAccountUUID, &principal.BillingAccountUUID, &principal.Environment, &scopesJSON, &stored, &revoked, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationPrincipal{}, models.ErrInvalidAPIKey
	}
	if err != nil {
		return models.IntegrationPrincipal{}, err
	}
	if subtle.ConstantTimeCompare(stored, digest[:]) != 1 || revoked.Valid || (expires.Valid && !expires.Time.After(time.Now().UTC())) {
		return models.IntegrationPrincipal{}, models.ErrInvalidAPIKey
	}
	if principal.Environment != requestEnvironment {
		return models.IntegrationPrincipal{}, models.ErrAPIKeyEnvironmentMismatch
	}
	if err = json.Unmarshal([]byte(scopesJSON), &principal.Scopes); err != nil {
		return models.IntegrationPrincipal{}, err
	}
	authorized := false
	for _, scope := range principal.Scopes {
		if scope == requiredScope {
			authorized = true
			break
		}
	}
	if !authorized {
		return models.IntegrationPrincipal{}, models.ErrAPIKeyScopeDenied
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE integration_api_keys SET last_used_at=now() WHERE prefix=$1 AND (last_used_at IS NULL OR last_used_at<now()-interval '5 minutes')`, prefix)
	return principal, nil
}

func (r *Repository) RevokeIntegrationAPIKey(ctx context.Context, keyID, actorID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `UPDATE integration_api_keys k SET revoked_at=COALESCE(k.revoked_at,now()) FROM integration_service_accounts sa JOIN developer_applications a USING(application_uuid) WHERE k.service_account_uuid=sa.service_account_uuid AND k.key_uuid=$1 AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members m WHERE m.company_uuid=a.company_uuid AND m.user_uuid=$2 AND m.status='active' AND m.role IN ('company_manager','company_deputy')))))`, keyID, actorID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return models.ErrForbidden
	}
	return nil
}
func (r *Repository) RotateIntegrationAPIKey(ctx context.Context, keyID, actorID uuid.UUID, overlap time.Duration) (models.IntegrationAPIKey, string, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var serviceID uuid.UUID
	var name, environment string
	var scopes []string
	var scopesJSON string
	var permanent, temporary sql.NullInt64
	var temporaryStart, temporaryEnd sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT sa.service_account_uuid,k.name,to_json(k.scopes)::text,a.environment,k.permanent_credit_limit,k.temporary_credit_limit,k.temporary_limit_starts_at,k.temporary_limit_ends_at FROM integration_api_keys k JOIN integration_service_accounts sa USING(service_account_uuid) JOIN developer_applications a USING(application_uuid) WHERE k.key_uuid=$1 AND k.revoked_at IS NULL AND sa.status='active' AND a.status='active' AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members m WHERE m.company_uuid=a.company_uuid AND m.user_uuid=$2 AND m.status='active' AND m.role IN ('company_manager','company_deputy'))))) FOR UPDATE OF k,sa,a`, keyID, actorID).Scan(&serviceID, &name, &scopesJSON, &environment, &permanent, &temporary, &temporaryStart, &temporaryEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationAPIKey{}, "", models.ErrForbidden
	}
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	if err = json.Unmarshal([]byte(scopesJSON), &scopes); err != nil {
		return models.IntegrationAPIKey{}, "", fmt.Errorf("decode key scopes: %w", err)
	}
	if overlap < 0 {
		overlap = 0
	}
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	randomPart := base64.RawURLEncoding.EncodeToString(secretBytes)
	prefixKind := "vt_live_"
	if environment == "sandbox" {
		prefixKind = "vt_test_"
	}
	prefix := prefixKind + randomPart[:10]
	plaintext := prefix + "." + randomPart
	digest := sha256.Sum256([]byte(plaintext))
	newID, _ := uuid.NewV7()
	createdAt := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_api_keys(key_uuid,service_account_uuid,name,prefix,secret_hash,hash_version,scopes,permanent_credit_limit,temporary_credit_limit,temporary_limit_starts_at,temporary_limit_ends_at,created_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10,$11)`, newID, serviceID, name+" (rotated)", prefix, digest[:], scopes, nullableInt64(permanent), nullableInt64(temporary), nullableTime(temporaryStart), nullableTime(temporaryEnd), createdAt)
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_api_keys SET overlap_until=now()+make_interval(secs=>$2) WHERE key_uuid=$1`, keyID, int64(overlap/time.Second))
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	return models.IntegrationAPIKey{ID: newID, ServiceAccountID: serviceID, Name: name + " (rotated)", Prefix: prefix, Scopes: scopes, PermanentCreditLimit: int64Ptr(permanent), TemporaryCreditLimit: int64Ptr(temporary), TemporaryLimitStartsAt: timePtr(temporaryStart), TemporaryLimitEndsAt: timePtr(temporaryEnd), CreatedAt: createdAt}, plaintext, nil
}

func createSandboxGrant(ctx context.Context, tx *sql.Tx, accountID, appID uuid.UUID, credits int64) error {
	grantID, _ := uuid.NewV7()
	availableID, _ := uuid.NewV7()
	fundingID, _ := uuid.NewV7()
	transactionID, _ := uuid.NewV7()
	reference := "sandbox-application:" + appID.String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_grants(credit_grant_uuid,billing_account_uuid,application_uuid,grant_type,environment,original_credits,source_reference) VALUES($1,$2,$3,'sandbox','sandbox',$4,$5)`, grantID, accountID, appID, credits, reference); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,billing_account_uuid,credit_grant_uuid,environment,account_type) VALUES($1,$2,$3,'sandbox','customer_available')`, availableID, accountID, grantID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,environment,account_type) VALUES($1,'sandbox','funding_source') ON CONFLICT (environment,account_type) WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL DO UPDATE SET environment=EXCLUDED.environment RETURNING credit_ledger_account_uuid`, fundingID).Scan(&fundingID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger_transactions(credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,reason,source_reference) VALUES($1,'grant',$2,'system','sandbox test wallet',$3)`, transactionID, reference, reference); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger_postings(credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits) VALUES(gen_random_uuid(),$1,$2,$4::bigint),(gen_random_uuid(),$1,$3,-($4::bigint))`, transactionID, availableID, fundingID, credits)
	return err
}

func (r *Repository) ListDeveloperApplications(ctx context.Context, ownerType string, ownerID uuid.UUID) ([]models.DeveloperApplication, error) {
	column := "user_uuid"
	if ownerType == "company" {
		column = "company_uuid"
	}
	rows, err := r.db.QueryContext(ctx, `SELECT application_uuid,owner_type,user_uuid,company_uuid,billing_account_uuid,name,environment,status,to_json(capabilities)::text,daily_credit_limit,monthly_credit_limit,max_credits_per_operation,lock_version,created_at,updated_at FROM developer_applications WHERE `+column+`=$1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []models.DeveloperApplication
	for rows.Next() {
		var item models.DeveloperApplication
		var capabilitiesJSON string
		if err = rows.Scan(&item.ID, &item.OwnerType, &item.UserUUID, &item.CompanyUUID, &item.BillingAccountUUID, &item.Name, &item.Environment, &item.Status, &capabilitiesJSON, &item.DailyCreditLimit, &item.MonthlyCreditLimit, &item.MaxCreditsPerOperation, &item.LockVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(capabilitiesJSON), &item.Capabilities); err != nil {
			return nil, fmt.Errorf("decode application capabilities: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) UpdateDeveloperApplication(ctx context.Context, input models.UpdateDeveloperApplicationInput) (models.DeveloperApplication, error) {
	if input.ApplicationUUID == uuid.Nil || input.ActorUUID == uuid.Nil || strings.TrimSpace(input.Name) == "" || input.ExpectedLockVersion < 1 || negativeLimit(input.DailyCreditLimit) || negativeLimit(input.MonthlyCreditLimit) || negativeLimit(input.MaxCreditsPerOperation) {
		return models.DeveloperApplication{}, models.ErrInvalidBillingInput
	}
	result, err := r.db.ExecContext(ctx, `UPDATE developer_applications a SET name=$3,capabilities=$4,daily_credit_limit=$5,monthly_credit_limit=$6,max_credits_per_operation=$7,lock_version=lock_version+1,updated_at=now() WHERE a.application_uuid=$1 AND a.lock_version=$8 AND a.status<>'revoked' AND `+developerApplicationActorACL, input.ApplicationUUID, input.ActorUUID, strings.TrimSpace(input.Name), input.Capabilities, input.DailyCreditLimit, input.MonthlyCreditLimit, input.MaxCreditsPerOperation, input.ExpectedLockVersion)
	if err != nil {
		return models.DeveloperApplication{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		if _, getErr := r.GetDeveloperApplication(ctx, input.ApplicationUUID, input.ActorUUID); getErr != nil {
			return models.DeveloperApplication{}, getErr
		}
		return models.DeveloperApplication{}, models.ErrIntegrationConflict
	}
	return r.GetDeveloperApplication(ctx, input.ApplicationUUID, input.ActorUUID)
}

func negativeLimit(value *int64) bool { return value != nil && *value < 0 }
func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}
func nullableTime(value sql.NullTime) any {
	if value.Valid {
		return value.Time
	}
	return nil
}
func int64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func (r *Repository) GetDeveloperApplication(ctx context.Context, appID, actorID uuid.UUID) (models.DeveloperApplication, error) {
	return scanDeveloperApplication(r.db.QueryRowContext(ctx, developerApplicationSelect+` WHERE a.application_uuid=$1 AND `+developerApplicationActorACL, appID, actorID))
}

func (r *Repository) ChangeDeveloperApplicationStatus(ctx context.Context, appID, actorID uuid.UUID, status string) (models.DeveloperApplication, error) {
	if status != "active" && status != "disabled" && status != "revoked" {
		return models.DeveloperApplication{}, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.DeveloperApplication{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT a.status FROM developer_applications a WHERE a.application_uuid=$1 AND `+developerApplicationActorACL+` FOR UPDATE`, appID, actorID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return models.DeveloperApplication{}, models.ErrForbidden
	}
	if err != nil {
		return models.DeveloperApplication{}, err
	}
	if current == "revoked" && status != "revoked" {
		return models.DeveloperApplication{}, models.ErrIntegrationConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE developer_applications SET status=$2,revoked_at=CASE WHEN $2='revoked' THEN COALESCE(revoked_at,now()) ELSE NULL END,lock_version=lock_version+1,updated_at=now() WHERE application_uuid=$1`, appID, status)
	if err != nil {
		return models.DeveloperApplication{}, err
	}
	if status == "revoked" {
		if _, err = tx.ExecContext(ctx, `UPDATE integration_api_keys SET revoked_at=COALESCE(revoked_at,now()) WHERE service_account_uuid IN (SELECT service_account_uuid FROM integration_service_accounts WHERE application_uuid=$1)`, appID); err != nil {
			return models.DeveloperApplication{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE integration_service_accounts SET status='revoked' WHERE application_uuid=$1`, appID); err != nil {
			return models.DeveloperApplication{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE integration_connections SET status='revoked',revoked_at=COALESCE(revoked_at,now()),lock_version=lock_version+1,updated_at=now() WHERE application_uuid=$1`, appID); err != nil {
			return models.DeveloperApplication{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return models.DeveloperApplication{}, err
	}
	return r.GetDeveloperApplication(ctx, appID, actorID)
}

const developerApplicationSelect = `SELECT a.application_uuid,a.owner_type,a.user_uuid,a.company_uuid,a.billing_account_uuid,a.name,a.environment,a.status,to_json(a.capabilities)::text,a.daily_credit_limit,a.monthly_credit_limit,a.max_credits_per_operation,a.lock_version,a.created_at,a.updated_at FROM developer_applications a`
const developerApplicationActorACL = `((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy')))))`

func scanDeveloperApplication(row interface{ Scan(...any) error }) (models.DeveloperApplication, error) {
	var item models.DeveloperApplication
	var capabilitiesJSON string
	err := row.Scan(&item.ID, &item.OwnerType, &item.UserUUID, &item.CompanyUUID, &item.BillingAccountUUID, &item.Name, &item.Environment, &item.Status, &capabilitiesJSON, &item.DailyCreditLimit, &item.MonthlyCreditLimit, &item.MaxCreditsPerOperation, &item.LockVersion, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrForbidden
	}
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(capabilitiesJSON), &item.Capabilities); err != nil {
		return item, err
	}
	return item, nil
}

func (r *Repository) CreateIntegrationServiceAccount(ctx context.Context, connectionID, actorID uuid.UUID, name string, scopes []string) (models.IntegrationServiceAccount, error) {
	if connectionID == uuid.Nil || actorID == uuid.Nil || strings.TrimSpace(name) == "" || len(scopes) == 0 {
		return models.IntegrationServiceAccount{}, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IntegrationServiceAccount{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var appID uuid.UUID
	var capabilitiesJSON string
	err = tx.QueryRowContext(ctx, `SELECT a.application_uuid,to_json(a.capabilities)::text FROM integration_connections c JOIN developer_applications a USING(application_uuid) WHERE c.connection_uuid=$1 AND c.status<>'revoked' AND a.status='active' AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))))) FOR UPDATE OF c,a`, connectionID, actorID).Scan(&appID, &capabilitiesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationServiceAccount{}, models.ErrForbidden
	}
	if err != nil {
		return models.IntegrationServiceAccount{}, err
	}
	var capabilities []string
	if err = json.Unmarshal([]byte(capabilitiesJSON), &capabilities); err != nil || !subset(scopes, capabilities) {
		return models.IntegrationServiceAccount{}, models.ErrAPIKeyScopeDenied
	}
	id, _ := uuid.NewV7()
	createdAt := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_service_accounts(service_account_uuid,application_uuid,connection_uuid,name,status,scopes,created_by_user_uuid,created_at) VALUES($1,$2,$3,$4,'active',$5,$6,$7)`, id, appID, connectionID, strings.TrimSpace(name), scopes, actorID, createdAt)
	if err != nil {
		return models.IntegrationServiceAccount{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationServiceAccount{}, err
	}
	return models.IntegrationServiceAccount{ID: id, ApplicationID: appID, ConnectionID: connectionID, CreatedBy: actorID, Name: strings.TrimSpace(name), Status: "active", Scopes: scopes, CreatedAt: createdAt}, nil
}

func (r *Repository) ListIntegrationServiceAccounts(ctx context.Context, connectionID, actorID uuid.UUID) ([]models.IntegrationServiceAccount, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT sa.service_account_uuid,sa.application_uuid,sa.connection_uuid,sa.created_by_user_uuid,sa.name,sa.status,to_json(sa.scopes)::text,sa.created_at,max(k.last_used_at) FROM integration_service_accounts sa JOIN developer_applications a USING(application_uuid) LEFT JOIN integration_api_keys k USING(service_account_uuid) WHERE sa.connection_uuid=$1 AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))))) GROUP BY sa.service_account_uuid ORDER BY sa.created_at DESC`, connectionID, actorID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []models.IntegrationServiceAccount
	for rows.Next() {
		var item models.IntegrationServiceAccount
		var scopesJSON string
		var lastUsed sql.NullTime
		if err = rows.Scan(&item.ID, &item.ApplicationID, &item.ConnectionID, &item.CreatedBy, &item.Name, &item.Status, &scopesJSON, &item.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(scopesJSON), &item.Scopes); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			item.LastUsedAt = &lastUsed.Time
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) ListIntegrationAPIKeys(ctx context.Context, serviceID, actorID uuid.UUID) ([]models.IntegrationAPIKey, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT k.key_uuid,k.service_account_uuid,k.name,k.prefix,to_json(k.scopes)::text,k.expires_at,k.last_used_at,k.revoked_at,k.created_at,k.permanent_credit_limit,k.temporary_credit_limit,k.temporary_limit_starts_at,k.temporary_limit_ends_at FROM integration_api_keys k JOIN integration_service_accounts sa USING(service_account_uuid) JOIN developer_applications a USING(application_uuid) WHERE k.service_account_uuid=$1 AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))))) ORDER BY k.created_at DESC`, serviceID, actorID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []models.IntegrationAPIKey
	for rows.Next() {
		var item models.IntegrationAPIKey
		var scopesJSON string
		var expires, lastUsed, revoked, temporaryStart, temporaryEnd sql.NullTime
		var permanent, temporary sql.NullInt64
		if err = rows.Scan(&item.ID, &item.ServiceAccountID, &item.Name, &item.Prefix, &scopesJSON, &expires, &lastUsed, &revoked, &item.CreatedAt, &permanent, &temporary, &temporaryStart, &temporaryEnd); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(scopesJSON), &item.Scopes); err != nil {
			return nil, err
		}
		if expires.Valid {
			item.ExpiresAt = &expires.Time
		}
		if lastUsed.Valid {
			item.LastUsed = &lastUsed.Time
		}
		if revoked.Valid {
			item.RevokedAt = &revoked.Time
		}
		if permanent.Valid {
			item.PermanentCreditLimit = &permanent.Int64
		}
		if temporary.Valid {
			item.TemporaryCreditLimit = &temporary.Int64
		}
		if temporaryStart.Valid {
			item.TemporaryLimitStartsAt = &temporaryStart.Time
		}
		if temporaryEnd.Valid {
			item.TemporaryLimitEndsAt = &temporaryEnd.Time
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) RevokeIntegrationServiceAccount(ctx context.Context, serviceID, actorID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var appID, connectionID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT sa.application_uuid,sa.connection_uuid FROM integration_service_accounts sa JOIN developer_applications a USING(application_uuid) WHERE sa.service_account_uuid=$1 AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies c WHERE c.company_uuid=a.company_uuid AND c.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members m WHERE m.company_uuid=a.company_uuid AND m.user_uuid=$2 AND m.status='active' AND m.role IN ('company_manager','company_deputy'))))) FOR UPDATE OF sa`, serviceID, actorID).Scan(&appID, &connectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrForbidden
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE integration_service_accounts SET status='revoked',revoked_at=COALESCE(revoked_at,now()) WHERE service_account_uuid=$1`, serviceID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE integration_api_keys SET revoked_at=COALESCE(revoked_at,now()) WHERE service_account_uuid=$1`, serviceID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_audit_events(audit_event_uuid,application_uuid,connection_uuid,actor_type,actor_uuid,event_type,entity_type,entity_uuid,metadata_safe) VALUES(gen_random_uuid(),$1,$2,'user',$3,'service_account.revoked','service_account',$4,'{}')`, appID, connectionID, actorID, serviceID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CreateIntegrationAPIKeyForServiceAccount(ctx context.Context, serviceID, actorID uuid.UUID, input models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error) {
	if !validKeyCreationInput(input) {
		return models.IntegrationAPIKey{}, "", models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var environment, serviceScopesJSON string
	err = tx.QueryRowContext(ctx, `SELECT a.environment,to_json(sa.scopes)::text FROM integration_service_accounts sa JOIN developer_applications a USING(application_uuid) WHERE sa.service_account_uuid=$1 AND sa.status='active' AND a.status='active' AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))))) FOR UPDATE OF sa,a`, serviceID, actorID).Scan(&environment, &serviceScopesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IntegrationAPIKey{}, "", models.ErrForbidden
	}
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	var serviceScopes []string
	if err = json.Unmarshal([]byte(serviceScopesJSON), &serviceScopes); err != nil || !subset(input.Scopes, serviceScopes) {
		return models.IntegrationAPIKey{}, "", models.ErrAPIKeyScopeDenied
	}
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	randomPart := base64.RawURLEncoding.EncodeToString(secretBytes)
	prefixKind := "vt_live_"
	if environment == "sandbox" {
		prefixKind = "vt_test_"
	}
	prefix := prefixKind + randomPart[:10]
	plaintext := prefix + "." + randomPart
	digest := sha256.Sum256([]byte(plaintext))
	id, _ := uuid.NewV7()
	createdAt := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_api_keys(key_uuid,service_account_uuid,name,prefix,secret_hash,hash_version,scopes,expires_at,permanent_credit_limit,temporary_credit_limit,temporary_limit_starts_at,temporary_limit_ends_at,created_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10,$11,$12)`, id, serviceID, strings.TrimSpace(input.Name), prefix, digest[:], input.Scopes, input.ExpiresAt, input.PermanentCreditLimit, input.TemporaryCreditLimit, input.TemporaryLimitStartsAt, input.TemporaryLimitEndsAt, createdAt)
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	return models.IntegrationAPIKey{ID: id, ServiceAccountID: serviceID, Name: strings.TrimSpace(input.Name), Prefix: prefix, Scopes: input.Scopes, ExpiresAt: input.ExpiresAt, PermanentCreditLimit: input.PermanentCreditLimit, TemporaryCreditLimit: input.TemporaryCreditLimit, TemporaryLimitStartsAt: input.TemporaryLimitStartsAt, TemporaryLimitEndsAt: input.TemporaryLimitEndsAt, CreatedAt: createdAt}, plaintext, nil
}

func subset(requested, allowed []string) bool {
	for _, value := range requested {
		found := false
		for _, candidate := range allowed {
			if value == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// CreateIntegrationAPIKey returns the plaintext once. Only its SHA-256 digest is persisted.
func (r *Repository) CreateIntegrationAPIKey(ctx context.Context, applicationID, actorID uuid.UUID, input models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error) {
	if !validKeyCreationInput(input) {
		return models.IntegrationAPIKey{}, "", models.ErrInvalidBillingInput
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	randomPart := base64.RawURLEncoding.EncodeToString(secretBytes)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var environment string
	var ownerType string
	var userID, companyID uuid.NullUUID
	var capabilities []string
	var capabilitiesJSON string
	if err = tx.QueryRowContext(ctx, `SELECT environment,owner_type,user_uuid,company_uuid,to_json(capabilities)::text FROM developer_applications WHERE application_uuid=$1 AND status='active' FOR UPDATE`, applicationID).Scan(&environment, &ownerType, &userID, &companyID, &capabilitiesJSON); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	if err = json.Unmarshal([]byte(capabilitiesJSON), &capabilities); err != nil {
		return models.IntegrationAPIKey{}, "", fmt.Errorf("decode application capabilities: %w", err)
	}
	for _, requested := range input.Scopes {
		allowed := false
		for _, capability := range capabilities {
			if requested == capability {
				allowed = true
				break
			}
		}
		if !allowed {
			return models.IntegrationAPIKey{}, "", models.ErrAPIKeyScopeDenied
		}
	}
	if ownerType == "user" && (!userID.Valid || userID.UUID != actorID) {
		return models.IntegrationAPIKey{}, "", models.ErrForbidden
	}
	if ownerType == "company" {
		var authorized bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM companies WHERE company_uuid=$1 AND manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active' AND role IN ('company_manager','company_deputy'))`, companyID.UUID, actorID).Scan(&authorized); err != nil || !authorized {
			return models.IntegrationAPIKey{}, "", models.ErrForbidden
		}
	}
	connectionID, _ := uuid.NewV7()
	if err = tx.QueryRowContext(ctx, `SELECT connection_uuid FROM integration_connections WHERE application_uuid=$1 AND status='active' ORDER BY created_at LIMIT 1`, applicationID).Scan(&connectionID); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	serviceID, _ := uuid.NewV7()
	err = tx.QueryRowContext(ctx, `SELECT service_account_uuid FROM integration_service_accounts WHERE application_uuid=$1 AND connection_uuid=$2 AND status='active' AND scopes @> $3::text[] ORDER BY created_at LIMIT 1`, applicationID, connectionID, input.Scopes).Scan(&serviceID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO integration_service_accounts(service_account_uuid,application_uuid,connection_uuid,name,status,scopes,created_by_user_uuid) VALUES($1,$2,$3,$4,'active',$5,$6)`, serviceID, applicationID, connectionID, "Default application service account", input.Scopes, actorID); err != nil {
			return models.IntegrationAPIKey{}, "", err
		}
	} else if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	prefixKind := "vt_live_"
	if environment == "sandbox" {
		prefixKind = "vt_test_"
	}
	prefix := prefixKind + randomPart[:10]
	plaintext := prefix + "." + randomPart
	digest := sha256.Sum256([]byte(plaintext))
	keyID, _ := uuid.NewV7()
	createdAt := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_api_keys(key_uuid,service_account_uuid,name,prefix,secret_hash,hash_version,scopes,expires_at,permanent_credit_limit,temporary_credit_limit,temporary_limit_starts_at,temporary_limit_ends_at,created_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10,$11,$12)`, keyID, serviceID, strings.TrimSpace(input.Name), prefix, digest[:], input.Scopes, input.ExpiresAt, input.PermanentCreditLimit, input.TemporaryCreditLimit, input.TemporaryLimitStartsAt, input.TemporaryLimitEndsAt, createdAt)
	if err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return models.IntegrationAPIKey{}, "", err
	}
	return models.IntegrationAPIKey{ID: keyID, ServiceAccountID: serviceID, Name: strings.TrimSpace(input.Name), Prefix: prefix, Scopes: input.Scopes, ExpiresAt: input.ExpiresAt, PermanentCreditLimit: input.PermanentCreditLimit, TemporaryCreditLimit: input.TemporaryCreditLimit, TemporaryLimitStartsAt: input.TemporaryLimitStartsAt, TemporaryLimitEndsAt: input.TemporaryLimitEndsAt, CreatedAt: createdAt}, plaintext, nil
}

func validKeyCreationInput(input models.CreateIntegrationAPIKeyInput) bool {
	if input.PermanentCreditLimit != nil && input.TemporaryCreditLimit != nil {
		return false
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Scopes) == 0 || negativeLimit(input.PermanentCreditLimit) || negativeLimit(input.TemporaryCreditLimit) {
		return false
	}
	temporaryEmpty := input.TemporaryCreditLimit == nil && input.TemporaryLimitStartsAt == nil && input.TemporaryLimitEndsAt == nil
	temporaryComplete := input.TemporaryCreditLimit != nil && input.TemporaryLimitStartsAt != nil && input.TemporaryLimitEndsAt != nil && input.TemporaryLimitEndsAt.After(*input.TemporaryLimitStartsAt)
	return temporaryEmpty || temporaryComplete
}
