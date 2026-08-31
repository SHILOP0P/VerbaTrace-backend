package bitrix24

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/integrationcrypto"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("bitrix24 connector unavailable")
	ErrForbidden   = errors.New("bitrix24 connector forbidden")
	ErrInvalid     = errors.New("invalid bitrix24 input")
	ErrNotFound    = errors.New("bitrix24 connection not found")
	ErrConflict    = errors.New("bitrix24 connector conflict")
	ErrOAuthState  = errors.New("invalid or expired oauth state")
)

type Config struct {
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	TokenURL      string
	PublicBaseURL string
	EventToken    string
}

type Service struct {
	db       *sql.DB
	cipher   *integrationcrypto.Cipher
	config   Config
	client   *http.Client
	resolver *net.Resolver
	now      func() time.Time
}

func NewService(db *sql.DB, cipher *integrationcrypto.Cipher, config Config) *Service {
	if config.TokenURL == "" {
		config.TokenURL = "https://oauth.bitrix.info/oauth/token/"
	}
	return &Service{db: db, cipher: cipher, config: config, client: &http.Client{Timeout: 15 * time.Second}, resolver: net.DefaultResolver, now: time.Now}
}

func (s *Service) OAuthCallbackOrigin() string {
	parsed, err := url.Parse(strings.TrimSpace(s.config.PublicBaseURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (s *Service) StartOAuth(ctx context.Context, connectionID, actor uuid.UUID, portalDomain string, expectedVersion int64) (models.BitrixOAuthStart, error) {
	if s.cipher == nil || s.config.ClientID == "" || s.config.ClientSecret == "" || s.config.RedirectURI == "" {
		return models.BitrixOAuthStart{}, ErrUnavailable
	}
	portalDomain, err := s.validatePortalDomain(ctx, portalDomain)
	if err != nil || expectedVersion < 1 {
		return models.BitrixOAuthStart{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.BitrixOAuthStart{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var actualVersion int64
	if err = tx.QueryRowContext(ctx, `SELECT c.lock_version FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status<>'revoked' AND `+managerAccessSQL+` FOR UPDATE`, connectionID, actor).Scan(&actualVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.BitrixOAuthStart{}, ErrNotFound
		}
		return models.BitrixOAuthStart{}, err
	}
	if actualVersion != expectedVersion {
		return models.BitrixOAuthStart{}, ErrConflict
	}
	stateBytes := make([]byte, 32)
	if _, err = rand.Read(stateBytes); err != nil {
		return models.BitrixOAuthStart{}, err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	stateHash := sha256.Sum256([]byte(state))
	now := s.now().UTC()
	expires := now.Add(10 * time.Minute)
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_oauth_states(oauth_state_uuid,connection_uuid,requested_by_user_uuid,state_hash,redirect_uri,expires_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), connectionID, actor, stateHash[:], s.config.RedirectURI, expires, now)
	if err != nil {
		return models.BitrixOAuthStart{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_connections SET status='authorizing',settings=jsonb_set(settings,'{portal_domain_display}',to_jsonb($2::text),true),lock_version=lock_version+1,updated_at=$3 WHERE connection_uuid=$1`, connectionID, portalDomain, now)
	if err != nil {
		return models.BitrixOAuthStart{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.BitrixOAuthStart{}, err
	}
	authorize := url.URL{Scheme: "https", Host: portalDomain, Path: "/oauth/authorize/"}
	query := authorize.Query()
	query.Set("client_id", s.config.ClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", s.config.RedirectURI)
	query.Set("state", state)
	authorize.RawQuery = query.Encode()
	return models.BitrixOAuthStart{AuthorizationURL: authorize.String(), ExpiresAt: expires}, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	MemberID     string `json:"member_id"`
	Domain       string `json:"domain"`
	UserID       int64  `json:"user_id"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (s *Service) CompleteOAuth(ctx context.Context, state, code string) (uuid.UUID, error) {
	if state == "" || code == "" || s.cipher == nil {
		return uuid.Nil, ErrOAuthState
	}
	hash := sha256.Sum256([]byte(state))
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return uuid.Nil, err
	}
	var connectionID, actor uuid.UUID
	var redirectURI, expectedPortalDomain string
	err = tx.QueryRowContext(ctx, `SELECT s.connection_uuid,s.requested_by_user_uuid,s.redirect_uri,COALESCE(c.settings->>'portal_domain_display','')
		FROM integration_oauth_states s JOIN integration_connections c ON c.connection_uuid=s.connection_uuid
		WHERE s.state_hash=$1 AND s.consumed_at IS NULL AND s.expires_at>$2 FOR UPDATE OF s`, hash[:], now).Scan(&connectionID, &actor, &redirectURI, &expectedPortalDomain)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return uuid.Nil, ErrOAuthState
	}
	if err != nil {
		_ = tx.Rollback()
		return uuid.Nil, err
	}
	var managerStillActive bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections c WHERE c.connection_uuid=$1 AND c.status='authorizing' AND `+managerAccessSQL+`)`, connectionID, actor).Scan(&managerStillActive)
	if err != nil || !managerStillActive {
		_ = tx.Rollback()
		return uuid.Nil, ErrForbidden
	}
	if _, err = tx.ExecContext(ctx, `UPDATE integration_oauth_states SET consumed_at=$2 WHERE state_hash=$1`, hash[:], now); err != nil {
		_ = tx.Rollback()
		return uuid.Nil, err
	}
	if err = tx.Commit(); err != nil {
		return uuid.Nil, err
	}

	token, err := s.exchangeCode(ctx, code, redirectURI)
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE integration_connections SET status='reconnect_required',last_error_code='bitrix_oauth_exchange_failed',last_health_at=$2,updated_at=$2 WHERE connection_uuid=$1`, connectionID, now)
		return uuid.Nil, err
	}
	// OAuth token responses for local Bitrix24 applications may identify the
	// authorization server (for example oauth.bitrix.info) in `domain`. REST
	// calls must stay bound to the portal that initiated StartOAuth instead.
	domain, err := s.validatePortalDomain(ctx, expectedPortalDomain)
	if err != nil || token.MemberID == "" || token.AccessToken == "" {
		return uuid.Nil, ErrInvalid
	}
	accessCipher, accessNonce, err := s.encryptToken(token.AccessToken, connectionID, "access")
	if err != nil {
		return uuid.Nil, ErrUnavailable
	}
	refreshCipher, refreshNonce, err := s.encryptToken(token.RefreshToken, connectionID, "refresh")
	if err != nil {
		return uuid.Nil, ErrUnavailable
	}
	expiresIn := token.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	expiresAt := now.Add(time.Duration(expiresIn) * time.Second)
	tx, err = s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_oauth_credentials(connection_uuid,access_token_ciphertext,access_token_nonce,refresh_token_ciphertext,refresh_token_nonce,key_version,expires_at,last_refreshed_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$8) ON CONFLICT(connection_uuid) DO UPDATE SET access_token_ciphertext=EXCLUDED.access_token_ciphertext,access_token_nonce=EXCLUDED.access_token_nonce,refresh_token_ciphertext=EXCLUDED.refresh_token_ciphertext,refresh_token_nonce=EXCLUDED.refresh_token_nonce,key_version=EXCLUDED.key_version,expires_at=EXCLUDED.expires_at,refresh_state='ready',last_error_code=NULL,last_refreshed_at=EXCLUDED.last_refreshed_at,updated_at=EXCLUDED.updated_at`, connectionID, accessCipher, accessNonce, refreshCipher, refreshNonce, s.cipher.Version(), expiresAt, now)
	if err != nil {
		return uuid.Nil, err
	}
	scopes := strings.FieldsFunc(token.Scope, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	settingsPatch, _ := json.Marshal(map[string]any{"portal_member_id": token.MemberID, "portal_domain_display": domain, "oauth_user_external_id": strconv.FormatInt(token.UserID, 10), "oauth_scope": scopes, "provider_contract_version": 1})
	result, err := tx.ExecContext(ctx, `UPDATE integration_connections SET settings=settings||$2::jsonb,status='testing',last_error_code=NULL,updated_at=$3,lock_version=lock_version+1 WHERE connection_uuid=$1 AND provider='bitrix24' AND status='authorizing'`, connectionID, settingsPatch, now)
	if err != nil {
		return uuid.Nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return uuid.Nil, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "uq_bitrix24_portal_member_active") {
			return uuid.Nil, ErrConflict
		}
		return uuid.Nil, err
	}
	return connectionID, nil
}

func (s *Service) TestConnection(ctx context.Context, connectionID, actor uuid.UUID) (models.BitrixConnectionHealth, error) {
	info, token, err := s.connectionToken(ctx, connectionID, actor)
	if err != nil {
		return models.BitrixConnectionHealth{}, err
	}
	// method.get checks one method at a time and requires its name parameter.
	// Calling it without a name returns an invalid request, which previously made
	// every capability look unavailable even after a successful OAuth exchange.
	hasCallsMethod, callsMethodErr := s.methodAvailable(ctx, info.Domain, token, "voximplant.statistic.get")
	hasUsersMethod, usersMethodErr := s.methodAvailable(ctx, info.Domain, token, "user.get")
	hasTaskMethod, taskMethodErr := s.methodAvailable(ctx, info.Domain, token, "tasks.task.add")
	methodsErr := firstError(callsMethodErr, usersMethodErr, taskMethodErr)
	var callsResult []statisticRecord
	callsErr := ErrUnavailable
	if hasCallsMethod {
		callsErr = s.call(ctx, info.Domain, "voximplant.statistic.get", token, map[string]any{"SORT": "CALL_START_DATE", "ORDER": "DESC", "start": 0}, &callsResult)
	}
	var users []map[string]any
	usersErr := ErrUnavailable
	if hasUsersMethod {
		usersErr = s.call(ctx, info.Domain, "user.get", token, map[string]any{"FILTER": map[string]any{"ACTIVE": true}}, &users)
	}
	// method.get reports availability for this OAuth application, so it is the
	// authoritative check for the task permission. The token's `scope` field can
	// contain the generic value "app" for local Bitrix24 applications.
	tasksWritable := hasTaskMethod && taskMethodErr == nil
	callObserved := false
	for _, record := range callsResult {
		if strings.TrimSpace(record.ID) != "" && strings.TrimSpace(record.StartedAt) != "" {
			callObserved = true
			break
		}
	}
	now := s.now().UTC()
	status := "active"
	var errorCode *string
	if methodsErr != nil || callsErr != nil || usersErr != nil || !tasksWritable {
		status = "degraded"
		value := "bitrix_permission_or_api_error"
		errorCode = &value
	}
	// A read-only capability probe cannot prove that a real routed call has an
	// accessible recording, can be imported, or that task write-back completes.
	// connector_verified is reserved for the separately evidenced portal pilot.
	connectorVerified := false
	capabilities, _ := json.Marshal(map[string]any{"calls_readable": callsErr == nil, "users_readable": usersErr == nil, "tasks_writable": tasksWritable, "methods_discovered": methodsErr == nil, "call_observed": callObserved, "real_call_verified": false, "connector_verified": connectorVerified, "checked_at": now})
	_, _ = s.db.ExecContext(ctx, `UPDATE integration_connections SET status=$2,settings=jsonb_set(settings,'{capabilities}',$5::jsonb,true),last_health_at=$3,last_success_at=CASE WHEN $2='active' THEN $3 ELSE last_success_at END,last_error_code=$4,updated_at=$3,lock_version=lock_version+1 WHERE connection_uuid=$1`, connectionID, status, now, errorCode, string(capabilities))
	return models.BitrixConnectionHealth{ConnectionID: connectionID, Status: status, PortalDomain: info.Domain, CallsReadable: callsErr == nil, UsersReadable: usersErr == nil, TasksWritable: tasksWritable, OAuthConfigured: true, ConnectorVerified: connectorVerified, LastErrorCode: errorCode}, nil
}

func (s *Service) Health(ctx context.Context, connectionID, actor uuid.UUID) (models.BitrixConnectionHealth, error) {
	var item models.BitrixConnectionHealth
	var settingsJSON string
	var credentialExists bool
	err := s.db.QueryRowContext(ctx, `SELECT c.status,c.settings::text,c.last_success_at,c.last_error_code,EXISTS(SELECT 1 FROM integration_oauth_credentials oc WHERE oc.connection_uuid=c.connection_uuid AND oc.refresh_state<>'revoked') FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND `+managerOrLeaderAccessSQL, connectionID, actor).
		Scan(&item.Status, &settingsJSON, &item.LastSuccessAt, &item.LastErrorCode, &credentialExists)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	var settings map[string]any
	_ = json.Unmarshal([]byte(settingsJSON), &settings)
	item.ConnectionID = connectionID
	item.PortalDomain, _ = settings["portal_domain_display"].(string)
	item.OAuthConfigured = credentialExists
	item.ReconnectRequired = item.Status == "reconnect_required"
	if capabilities, ok := settings["capabilities"].(map[string]any); ok {
		item.TasksWritable = boolValue(capabilities["tasks_writable"])
		item.CallsReadable = boolValue(capabilities["calls_readable"])
		item.UsersReadable = boolValue(capabilities["users_readable"])
		item.ConnectorVerified = boolValue(capabilities["connector_verified"])
	}
	return item, nil
}

func (s *Service) PauseConnection(ctx context.Context, connectionID, actor uuid.UUID, expectedVersion int64) (models.BitrixConnectionHealth, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE integration_connections c SET status='paused',lock_version=lock_version+1,updated_at=now()
		WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status IN ('active','degraded','reconnect_required') AND c.lock_version=$3 AND `+managerAccessSQL, connectionID, actor, expectedVersion)
	if err != nil {
		return models.BitrixConnectionHealth{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return models.BitrixConnectionHealth{}, ErrConflict
	}
	return s.Health(ctx, connectionID, actor)
}

func (s *Service) ResumeConnection(ctx context.Context, connectionID, actor uuid.UUID, expectedVersion int64) (models.BitrixConnectionHealth, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE integration_connections c SET status='testing',lock_version=lock_version+1,updated_at=now()
		WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status='paused' AND c.lock_version=$3 AND EXISTS(SELECT 1 FROM integration_oauth_credentials oc WHERE oc.connection_uuid=c.connection_uuid AND oc.refresh_state='ready') AND `+managerAccessSQL, connectionID, actor, expectedVersion)
	if err != nil {
		return models.BitrixConnectionHealth{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return models.BitrixConnectionHealth{}, ErrConflict
	}
	return s.TestConnection(ctx, connectionID, actor)
}

func (s *Service) ListExternalUsers(ctx context.Context, connectionID, actor uuid.UUID) ([]models.BitrixExternalUser, error) {
	info, token, err := s.connectionToken(ctx, connectionID, actor)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err = s.call(ctx, info.Domain, "user.get", token, map[string]any{}, &raw); err != nil {
		return nil, err
	}
	items := make([]models.BitrixExternalUser, 0, len(raw))
	for _, user := range raw {
		id := stringValue(user["ID"])
		if id == "" {
			continue
		}
		name := strings.TrimSpace(strings.Join([]string{stringValue(user["LAST_NAME"]), stringValue(user["NAME"])}, " "))
		if name == "" {
			name = "Bitrix24 user #" + id
		}
		item := models.BitrixExternalUser{ExternalUserID: id, DisplayName: name, Active: boolValue(user["ACTIVE"])}
		err = s.db.QueryRowContext(ctx, `INSERT INTO integration_external_user_mappings(mapping_uuid,connection_uuid,external_user_id,status,external_display_snapshot,external_active,lock_version)
			VALUES($1,$2,$3,'unmapped',$4,$5,1)
			ON CONFLICT(connection_uuid,external_user_id) DO UPDATE SET external_display_snapshot=EXCLUDED.external_display_snapshot,external_active=EXCLUDED.external_active,updated_at=now()
			RETURNING mapping_uuid,internal_user_uuid,department_uuid,status,lock_version`, uuid.New(), connectionID, id, name, item.Active).
			Scan(&item.MappingID, &item.InternalUserID, &item.DepartmentID, &item.MappingStatus, &item.LockVersion)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) UpdateExternalUserMapping(ctx context.Context, in models.UpdateBitrixUserMappingInput) (models.BitrixExternalUser, error) {
	if in.ExpectedLockVersion < 0 || in.ExternalUserID == "" || (in.Status != "mapped" && in.Status != "ignored" && in.Status != "unmapped") {
		return models.BitrixExternalUser{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.BitrixExternalUser{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND `+managerAccessSQL+`)`, in.ConnectionID, in.ActorID).Scan(&allowed); err != nil || !allowed {
		return models.BitrixExternalUser{}, ErrForbidden
	}
	if in.Status == "mapped" && !in.InternalUserID.Valid {
		return models.BitrixExternalUser{}, ErrInvalid
	}
	if err = validateMappingAssignment(ctx, tx, in.ConnectionID, models.BitrixMappingChange{ExternalUserID: in.ExternalUserID, InternalUserID: in.InternalUserID, DepartmentID: in.DepartmentID, Status: in.Status, ExpectedLockVersion: in.ExpectedLockVersion}); err != nil {
		return models.BitrixExternalUser{}, err
	}
	now := s.now().UTC()
	id := uuid.New()
	result, err := tx.ExecContext(ctx, `INSERT INTO integration_external_user_mappings(mapping_uuid,connection_uuid,external_user_id,internal_user_uuid,department_uuid,status,external_display_snapshot,external_active,mapped_by_user_uuid,mapped_at,lock_version,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,'',true,$7,$8,1,$8,$8)
		ON CONFLICT(connection_uuid,external_user_id) DO UPDATE SET internal_user_uuid=EXCLUDED.internal_user_uuid,department_uuid=EXCLUDED.department_uuid,status=EXCLUDED.status,mapped_by_user_uuid=EXCLUDED.mapped_by_user_uuid,mapped_at=EXCLUDED.mapped_at,lock_version=integration_external_user_mappings.lock_version+1,updated_at=EXCLUDED.updated_at
		WHERE integration_external_user_mappings.lock_version=$9`, id, in.ConnectionID, in.ExternalUserID, nullableUUID(in.InternalUserID), nullableUUID(in.DepartmentID), in.Status, in.ActorID, now, in.ExpectedLockVersion)
	if err != nil {
		return models.BitrixExternalUser{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return models.BitrixExternalUser{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return models.BitrixExternalUser{}, err
	}
	return models.BitrixExternalUser{ExternalUserID: in.ExternalUserID, Active: true, InternalUserID: in.InternalUserID, DepartmentID: in.DepartmentID, MappingStatus: in.Status, LockVersion: max(1, in.ExpectedLockVersion+1)}, nil
}

type connectionInfo struct {
	Domain string
	Scopes []string
}

func (s *Service) connectionToken(ctx context.Context, id, actor uuid.UUID) (connectionInfo, string, error) {
	var allowed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections c WHERE c.connection_uuid=$1 AND c.provider='bitrix24' AND c.status NOT IN ('revoked','paused','disabled') AND `+managerOrLeaderAccessSQL+`)`, id, actor).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return connectionInfo{}, "", ErrNotFound
	}
	if err != nil {
		return connectionInfo{}, "", err
	}
	if !allowed {
		return connectionInfo{}, "", ErrForbidden
	}
	return s.systemConnectionToken(ctx, id)
}

func (s *Service) refreshConnectionToken(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	var refreshCipher, refreshNonce []byte
	var state string
	var lease sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT refresh_token_ciphertext,refresh_token_nonce,refresh_state,refresh_lease_until FROM integration_oauth_credentials WHERE connection_uuid=$1 FOR UPDATE`, id).Scan(&refreshCipher, &refreshNonce, &state, &lease)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	now := s.now().UTC()
	if state == "refreshing" && lease.Valid && lease.Time.After(now) {
		_ = tx.Rollback()
		return ErrUnavailable
	}
	if state == "revoked" || len(refreshCipher) == 0 {
		_ = tx.Rollback()
		return ErrUnavailable
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_oauth_credentials SET refresh_state='refreshing',refresh_lease_until=$2,updated_at=$3 WHERE connection_uuid=$1`, id, now.Add(30*time.Second), now)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	plain, err := s.cipher.Decrypt(append(append([]byte{}, refreshNonce...), refreshCipher...), "integration_oauth_credentials/"+id.String()+"/refresh")
	if err != nil {
		return s.markRefreshFailed(ctx, id, "credential_decrypt_failed")
	}
	token, err := s.exchangeRefresh(ctx, string(plain))
	if err != nil {
		return s.markRefreshFailed(ctx, id, "bitrix_refresh_failed")
	}
	if token.AccessToken == "" {
		return s.markRefreshFailed(ctx, id, "bitrix_refresh_invalid")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = string(plain)
	}
	accessCipher, accessNonce, err := s.encryptToken(token.AccessToken, id, "access")
	if err != nil {
		return s.markRefreshFailed(ctx, id, "credential_encrypt_failed")
	}
	newRefreshCipher, newRefreshNonce, err := s.encryptToken(token.RefreshToken, id, "refresh")
	if err != nil {
		return s.markRefreshFailed(ctx, id, "credential_encrypt_failed")
	}
	expires := token.ExpiresIn
	if expires <= 0 {
		expires = 3600
	}
	_, err = s.db.ExecContext(ctx, `UPDATE integration_oauth_credentials SET access_token_ciphertext=$2,access_token_nonce=$3,refresh_token_ciphertext=$4,refresh_token_nonce=$5,expires_at=$6,refresh_state='ready',refresh_lease_until=NULL,last_refreshed_at=$7,last_error_code=NULL,updated_at=$7 WHERE connection_uuid=$1`, id, accessCipher, accessNonce, newRefreshCipher, newRefreshNonce, now.Add(time.Duration(expires)*time.Second), now)
	return err
}

func (s *Service) markRefreshFailed(ctx context.Context, id uuid.UUID, code string) error {
	_, _ = s.db.ExecContext(ctx, `UPDATE integration_oauth_credentials SET refresh_state='failed',refresh_lease_until=NULL,last_error_code=$2,updated_at=now() WHERE connection_uuid=$1`, id, code)
	_, _ = s.db.ExecContext(ctx, `UPDATE integration_connections SET status='reconnect_required',last_error_code=$2,updated_at=now() WHERE connection_uuid=$1`, id, code)
	return ErrUnavailable
}

func (s *Service) exchangeRefresh(ctx context.Context, refresh string) (tokenResponse, error) {
	values := url.Values{"grant_type": {"refresh_token"}, "client_id": {s.config.ClientID}, "client_secret": {s.config.ClientSecret}, "refresh_token": {refresh}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var token tokenResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		return token, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || token.Error != "" {
		return token, fmt.Errorf("bitrix refresh rejected")
	}
	return token, nil
}

type callPageMeta struct {
	Next  *int
	Total *int
}

func (s *Service) call(ctx context.Context, domain, method, token string, params any, target any) error {
	_, err := s.callPage(ctx, domain, method, token, params, target)
	return err
}

func (s *Service) methodAvailable(ctx context.Context, domain, token, method string) (bool, error) {
	var result map[string]any
	if err := s.call(ctx, domain, "method.get", token, map[string]any{"name": method}, &result); err != nil {
		return false, err
	}
	return boolValue(result["isAvailable"]), nil
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) callPage(ctx context.Context, domain, method, token string, params any, target any) (callPageMeta, error) {
	domain, err := s.validatePortalDomain(ctx, domain)
	if err != nil {
		return callPageMeta{}, ErrInvalid
	}
	payload := map[string]any{"auth": token}
	if fields, ok := params.(map[string]any); ok {
		for key, value := range fields {
			payload[key] = value
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+domain+"/rest/"+method+".json", bytes.NewReader(body))
	if err != nil {
		return callPageMeta{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	portalClient, err := s.portalHTTPClient(ctx, domain)
	if err != nil {
		return callPageMeta{}, err
	}
	resp, err := portalClient.Do(req)
	if err != nil {
		return callPageMeta{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	limited := io.LimitReader(resp.Body, 4<<20)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, limited)
		return callPageMeta{}, fmt.Errorf("bitrix api http status %d", resp.StatusCode)
	}
	var envelope struct {
		Result           json.RawMessage `json:"result"`
		Error            string          `json:"error"`
		ErrorDescription string          `json:"error_description"`
		Next             *int            `json:"next"`
		Total            *int            `json:"total"`
	}
	if err = json.NewDecoder(limited).Decode(&envelope); err != nil {
		return callPageMeta{}, err
	}
	if envelope.Error != "" {
		return callPageMeta{}, fmt.Errorf("bitrix api error: %s", envelope.Error)
	}
	if target != nil && len(envelope.Result) > 0 {
		if err = json.Unmarshal(envelope.Result, target); err != nil {
			return callPageMeta{}, err
		}
	}
	return callPageMeta{Next: envelope.Next, Total: envelope.Total}, nil
}

func (s *Service) exchangeCode(ctx context.Context, code, redirectURI string) (tokenResponse, error) {
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {s.config.ClientID}, "client_secret": {s.config.ClientSecret}, "code": {code}, "redirect_uri": {redirectURI}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var token tokenResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		return token, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || token.Error != "" {
		return token, fmt.Errorf("bitrix oauth rejected")
	}
	return token, nil
}

func (s *Service) encryptToken(token string, id uuid.UUID, kind string) ([]byte, []byte, error) {
	sealed, err := s.cipher.Encrypt([]byte(token), "integration_oauth_credentials/"+id.String()+"/"+kind)
	if err != nil {
		return nil, nil, err
	}
	if len(sealed) <= 12 {
		return nil, nil, ErrUnavailable
	}
	return append([]byte{}, sealed[12:]...), append([]byte{}, sealed[:12]...), nil
}

func (s *Service) validatePortalDomain(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	parsed, err := url.Parse("https://" + raw)
	if err != nil || parsed.Hostname() == "" || parsed.Host != parsed.Hostname() || parsed.Path != "" || parsed.Hostname() == "localhost" {
		return "", ErrInvalid
	}
	if net.ParseIP(parsed.Hostname()) != nil {
		return "", ErrInvalid
	}
	addresses, err := s.validPortalAddresses(ctx, parsed.Hostname())
	if err != nil || len(addresses) == 0 {
		return "", ErrInvalid
	}
	return parsed.Hostname(), nil
}

func (s *Service) validPortalAddresses(ctx context.Context, domain string) ([]net.IPAddr, error) {
	addresses, err := s.resolver.LookupIPAddr(ctx, domain)
	if err != nil || len(addresses) == 0 {
		return nil, ErrInvalid
	}
	for _, address := range addresses {
		if address.IP.IsLoopback() || address.IP.IsPrivate() || address.IP.IsLinkLocalUnicast() || address.IP.IsUnspecified() {
			return nil, ErrInvalid
		}
	}
	return addresses, nil
}

func (s *Service) portalHTTPClient(ctx context.Context, domain string) (*http.Client, error) {
	addresses, err := s.validPortalAddresses(ctx, domain)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:               nil,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, ServerName: domain},
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			host, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), domain) || port != "443" {
				return nil, ErrInvalid
			}
			var lastErr error
			for _, ip := range addresses {
				connection, dialErr := dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.IP.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !strings.EqualFold(req.URL.Hostname(), domain) || req.URL.Scheme != "https" || req.URL.Port() != "" {
				return ErrInvalid
			}
			if len(via) >= 3 {
				return errors.New("too many bitrix redirects")
			}
			return nil
		},
	}, nil
}

const managerAccessSQL = `(EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=c.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=c.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role='company_manager'))`
const managerOrLeaderAccessSQL = `(` + managerAccessSQL + ` OR EXISTS(SELECT 1 FROM department_members dm JOIN departments d ON d.department_uuid=dm.department_uuid WHERE dm.user_uuid=$2 AND dm.status='active' AND dm.role='department_leader' AND ((c.department_uuid IS NOT NULL AND dm.department_uuid=c.department_uuid) OR (c.department_uuid IS NULL AND d.company_uuid=c.company_uuid))))`

func nullableUUID(id uuid.NullUUID) any {
	if !id.Valid {
		return nil
	}
	return id.UUID
}
func containsScope(scopes []string, wanted string) bool {
	for _, scope := range scopes {
		if strings.EqualFold(scope, wanted) {
			return true
		}
	}
	return false
}
func hasTaskScope(scopes []string) bool {
	return containsScope(scopes, "task")
}
func stringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}
func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "Y" || typed == "1"
	default:
		return false
	}
}
