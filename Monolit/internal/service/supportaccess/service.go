package supportaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var (
	ErrForbidden = errors.New("support access forbidden")
	ErrInvalid   = errors.New("invalid support access request")
	ErrNotFound  = errors.New("support access request not found")
	ErrConflict  = errors.New("support access conflict")
)

var allowedResources = []string{"calls", "actions", "integrations", "billing_summary"}
var allowedCommands = []string{"diagnose", "retry_ingest", "reconnect_integration"}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }

func (s *Service) RunExpiryWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.expire(ctx)
			}
		}
	}()
	return done
}

func (s *Service) expire(ctx context.Context) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `UPDATE support_access_requests SET status='expired',decided_at=now(),lock_version=lock_version+1 WHERE status='pending' AND expires_at<=now() RETURNING request_uuid,requested_by_user_uuid`)
	if err != nil {
		return
	}
	var expiredRequests []struct{ requestID, requesterID uuid.UUID }
	for rows.Next() {
		var item struct{ requestID, requesterID uuid.UUID }
		if rows.Scan(&item.requestID, &item.requesterID) == nil {
			expiredRequests = append(expiredRequests, item)
		}
	}
	if rows.Err() != nil {
		_ = rows.Close()
		return
	}
	_ = rows.Close()
	for _, item := range expiredRequests {
		_, _ = tx.ExecContext(ctx, `INSERT INTO support_access_events(event_uuid,request_uuid,actor_type,event_type) VALUES($1,$2,'system','expired')`, uuid.New(), item.requestID)
		_ = insertNotification(ctx, tx, item.requesterID, "support_access_decided", "Запрос доступа истёк", "Разрешение не было выдано в течение срока действия заявки.", "support_access_request", item.requestID)
	}
	_, _ = tx.ExecContext(ctx, `INSERT INTO support_access_events(event_uuid,request_uuid,grant_uuid,actor_type,event_type)
		SELECT gen_random_uuid(),g.request_uuid,g.grant_uuid,'system','expired' FROM support_access_grants g
		WHERE g.expires_at<=now() AND NOT EXISTS(SELECT 1 FROM support_access_events e WHERE e.grant_uuid=g.grant_uuid AND e.event_type='expired')`)
	_ = tx.Commit()
}

func (s *Service) Create(ctx context.Context, in models.CreateSupportAccessRequestInput) (models.SupportAccessRequest, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !onlyAllowed(in.Resources, allowedResources) || !onlyAllowed(in.Commands, allowedCommands) {
		return models.SupportAccessRequest{}, ErrInvalid
	}
	in.Resources = uniqueAllowed(in.Resources, allowedResources)
	in.Commands = uniqueAllowed(in.Commands, allowedCommands)
	if in.RequesterUserID == uuid.Nil || len(in.Reason) < 10 || len(in.Resources) == 0 || in.RequestedDurationMinutes < 5 || in.RequestedDurationMinutes > 1440 {
		return models.SupportAccessRequest{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.SupportAccessRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE user_uuid=$1`, in.RequesterUserID).Scan(&role); err != nil || (role != "helper" && role != "admin" && role != "superadmin") {
		return models.SupportAccessRequest{}, ErrForbidden
	}
	var approver uuid.UUID
	switch {
	case in.SubjectType == "user" && in.SubjectUserID.Valid && !in.SubjectCompanyID.Valid:
		approver = in.SubjectUserID.UUID
	case in.SubjectType == "company" && in.SubjectCompanyID.Valid && !in.SubjectUserID.Valid:
		if err = tx.QueryRowContext(ctx, `SELECT manager_user_uuid FROM companies WHERE company_uuid=$1`, in.SubjectCompanyID.UUID).Scan(&approver); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return models.SupportAccessRequest{}, ErrNotFound
			}
			return models.SupportAccessRequest{}, err
		}
	default:
		return models.SupportAccessRequest{}, ErrInvalid
	}
	if approver == in.RequesterUserID {
		return models.SupportAccessRequest{}, ErrInvalid
	}
	now := s.now().UTC()
	item := models.SupportAccessRequest{
		ID: uuid.New(), RequestedByUserID: in.RequesterUserID, ApproverUserID: approver,
		SubjectType: in.SubjectType, SubjectUserID: in.SubjectUserID, SubjectCompanyID: in.SubjectCompanyID,
		Reason: in.Reason, RequestedResources: in.Resources, RequestedCommands: in.Commands,
		RequestedDurationMinutes: in.RequestedDurationMinutes, Status: "pending", LockVersion: 1,
		CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	resourcesJSON, _ := json.Marshal(item.RequestedResources)
	commandsJSON, _ := json.Marshal(item.RequestedCommands)
	_, err = tx.ExecContext(ctx, `INSERT INTO support_access_requests(
		request_uuid,requested_by_user_uuid,approver_user_uuid,subject_type,subject_user_uuid,subject_company_uuid,
		reason,requested_resources,requested_commands,requested_duration_minutes,status,lock_version,created_at,expires_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,ARRAY(SELECT jsonb_array_elements_text($8::jsonb)),ARRAY(SELECT jsonb_array_elements_text($9::jsonb)),$10,'pending',1,$11,$12)`,
		item.ID, item.RequestedByUserID, item.ApproverUserID, item.SubjectType, nullableUUID(item.SubjectUserID), nullableUUID(item.SubjectCompanyID),
		item.Reason, string(resourcesJSON), string(commandsJSON), item.RequestedDurationMinutes, item.CreatedAt, item.ExpiresAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "uq_support_access_pending_subject") {
			return models.SupportAccessRequest{}, ErrConflict
		}
		return models.SupportAccessRequest{}, err
	}
	if err = insertEvent(ctx, tx, item.ID, uuid.NullUUID{}, in.RequesterUserID, "requested", "", ""); err != nil {
		return models.SupportAccessRequest{}, err
	}
	if err = insertNotification(ctx, tx, approver, "support_access_requested", "Запрос доступа поддержки", in.Reason, "support_access_request", item.ID); err != nil {
		return models.SupportAccessRequest{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.SupportAccessRequest{}, err
	}
	return item, nil
}

func onlyAllowed(values, allowlist []string) bool {
	for _, value := range values {
		if !slices.Contains(allowlist, strings.TrimSpace(value)) {
			return false
		}
	}
	return true
}

func (s *Service) Get(ctx context.Context, id, actor uuid.UUID) (models.SupportAccessRequest, error) {
	row := s.db.QueryRowContext(ctx, requestSelect+` WHERE request_uuid=$1 AND (requested_by_user_uuid=$2 OR approver_user_uuid=$2)`, id, actor)
	return scanRequest(row)
}

func (s *Service) Approve(ctx context.Context, id, actor uuid.UUID, expectedVersion int64, comment string) (models.SupportAccessGrant, error) {
	return s.decide(ctx, id, actor, expectedVersion, true, comment)
}

func (s *Service) Deny(ctx context.Context, id, actor uuid.UUID, expectedVersion int64, comment string) error {
	_, err := s.decide(ctx, id, actor, expectedVersion, false, comment)
	return err
}

func (s *Service) decide(ctx context.Context, id, actor uuid.UUID, expectedVersion int64, approve bool, comment string) (models.SupportAccessGrant, error) {
	if expectedVersion < 1 {
		return models.SupportAccessGrant{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return models.SupportAccessGrant{}, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanRequest(tx.QueryRowContext(ctx, requestSelect+` WHERE request_uuid=$1 FOR UPDATE`, id))
	if err != nil {
		return models.SupportAccessGrant{}, err
	}
	now := s.now().UTC()
	if item.ApproverUserID != actor {
		return models.SupportAccessGrant{}, ErrForbidden
	}
	if item.Status != "pending" || item.LockVersion != expectedVersion || !now.Before(item.ExpiresAt) {
		return models.SupportAccessGrant{}, ErrConflict
	}
	comment = strings.TrimSpace(comment)
	if !approve && len(comment) < 3 {
		return models.SupportAccessGrant{}, ErrInvalid
	}
	state := "denied"
	if approve {
		state = "approved"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE support_access_requests SET status=$2,decision_comment=NULLIF($3,''),decided_at=$4,lock_version=lock_version+1 WHERE request_uuid=$1`, id, state, comment, now); err != nil {
		return models.SupportAccessGrant{}, err
	}
	grant := models.SupportAccessGrant{}
	if approve {
		grant = models.SupportAccessGrant{ID: uuid.New(), RequestID: id, GranteeUserID: item.RequestedByUserID, GrantedByUserID: actor,
			SubjectType: item.SubjectType, SubjectUserID: item.SubjectUserID, SubjectCompanyID: item.SubjectCompanyID,
			ResourceAllowlist: item.RequestedResources, CommandAllowlist: item.RequestedCommands, ValidFrom: now,
			ExpiresAt: now.Add(time.Duration(item.RequestedDurationMinutes) * time.Minute)}
		resourcesJSON, _ := json.Marshal(grant.ResourceAllowlist)
		commandsJSON, _ := json.Marshal(grant.CommandAllowlist)
		_, err = tx.ExecContext(ctx, `INSERT INTO support_access_grants(grant_uuid,request_uuid,grantee_user_uuid,granted_by_user_uuid,subject_type,subject_user_uuid,subject_company_uuid,resource_allowlist,command_allowlist,valid_from,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,ARRAY(SELECT jsonb_array_elements_text($8::jsonb)),ARRAY(SELECT jsonb_array_elements_text($9::jsonb)),$10,$11)`,
			grant.ID, grant.RequestID, grant.GranteeUserID, grant.GrantedByUserID, grant.SubjectType, nullableUUID(grant.SubjectUserID), nullableUUID(grant.SubjectCompanyID), string(resourcesJSON), string(commandsJSON), grant.ValidFrom, grant.ExpiresAt)
		if err != nil {
			return models.SupportAccessGrant{}, err
		}
	}
	event := "denied"
	if approve {
		event = "approved"
	}
	if err = insertEvent(ctx, tx, id, nullUUID(grant.ID), actor, event, "", ""); err != nil {
		return models.SupportAccessGrant{}, err
	}
	body := "Запрос временного доступа отклонён."
	if approve {
		body = "Временный доступ выдан до " + grant.ExpiresAt.Format(time.RFC3339) + "."
	}
	if err = insertNotification(ctx, tx, item.RequestedByUserID, "support_access_decided", "Решение по запросу доступа", body, "support_access_request", id); err != nil {
		return models.SupportAccessGrant{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.SupportAccessGrant{}, err
	}
	return grant, nil
}

func (s *Service) Revoke(ctx context.Context, id, actor uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var requestID, grantedBy, subjectUser uuid.NullUUID
	var subjectCompany uuid.NullUUID
	var grantee uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT request_uuid,grantee_user_uuid,granted_by_user_uuid,subject_user_uuid,subject_company_uuid FROM support_access_grants WHERE grant_uuid=$1 AND revoked_at IS NULL FOR UPDATE`, id).
		Scan(&requestID, &grantee, &grantedBy, &subjectUser, &subjectCompany)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	allowed := grantedBy.Valid && grantedBy.UUID == actor || subjectUser.Valid && subjectUser.UUID == actor
	if !allowed && subjectCompany.Valid {
		_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM companies WHERE company_uuid=$1 AND manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active' AND role IN ('company_manager','company_deputy'))`, subjectCompany.UUID, actor).Scan(&allowed)
	}
	if !allowed {
		return ErrForbidden
	}
	now := s.now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE support_access_grants SET revoked_at=$2,revoked_by_user_uuid=$3,revoke_reason=$4 WHERE grant_uuid=$1`, id, now, actor, reason); err != nil {
		return err
	}
	if err = insertEvent(ctx, tx, requestID.UUID, nullUUID(id), actor, "revoked", "", ""); err != nil {
		return err
	}
	if err = insertNotification(ctx, tx, grantee, "support_access_decided", "Доступ поддержки отозван", reason, "support_access_grant", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Authorize(ctx context.Context, actor uuid.UUID, subjectType string, subjectID uuid.UUID, resource, command string) (uuid.UUID, error) {
	var grantID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT grant_uuid FROM support_access_grants WHERE grantee_user_uuid=$1 AND subject_type=$2
		AND (($2='user' AND subject_user_uuid=$3) OR ($2='company' AND subject_company_uuid=$3))
		AND $4=ANY(resource_allowlist) AND ($5='' OR $5=ANY(command_allowlist)) AND valid_from<=now() AND expires_at>now() AND revoked_at IS NULL
		ORDER BY expires_at LIMIT 1`, actor, subjectType, subjectID, resource, command).Scan(&grantID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrForbidden
	}
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO support_access_events(event_uuid,grant_uuid,actor_user_uuid,event_type,resource,command) VALUES($1,$2,$3,'resource_accessed',$4,NULLIF($5,''))`, uuid.New(), grantID, actor, resource, command)
	return grantID, err
}

const requestSelect = `SELECT request_uuid,requested_by_user_uuid,approver_user_uuid,subject_type,subject_user_uuid,subject_company_uuid,reason,to_json(requested_resources)::text,to_json(requested_commands)::text,requested_duration_minutes,status,decision_comment,lock_version,created_at,expires_at,decided_at FROM support_access_requests`

type rowScanner interface{ Scan(...any) error }

func scanRequest(row rowScanner) (models.SupportAccessRequest, error) {
	var item models.SupportAccessRequest
	var resourcesJSON, commandsJSON string
	err := row.Scan(&item.ID, &item.RequestedByUserID, &item.ApproverUserID, &item.SubjectType, &item.SubjectUserID, &item.SubjectCompanyID, &item.Reason, &resourcesJSON, &commandsJSON, &item.RequestedDurationMinutes, &item.Status, &item.DecisionComment, &item.LockVersion, &item.CreatedAt, &item.ExpiresAt, &item.DecidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if json.Unmarshal([]byte(resourcesJSON), &item.RequestedResources) != nil || json.Unmarshal([]byte(commandsJSON), &item.RequestedCommands) != nil {
		return item, fmt.Errorf("decode support access allowlist")
	}
	return item, nil
}

func uniqueAllowed(values, allowlist []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !slices.Contains(allowlist, value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func nullableUUID(id uuid.NullUUID) any {
	if !id.Valid {
		return nil
	}
	return id.UUID
}
func nullUUID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil} }

func insertEvent(ctx context.Context, tx *sql.Tx, requestID uuid.UUID, grantID uuid.NullUUID, actor uuid.UUID, eventType, resource, command string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO support_access_events(event_uuid,request_uuid,grant_uuid,actor_type,actor_user_uuid,event_type,resource,command) VALUES($1,$2,$3,'user',$4,$5,NULLIF($6,''),NULLIF($7,''))`, uuid.New(), requestID, nullableUUID(grantID), actor, eventType, resource, command)
	return err
}

func insertNotification(ctx context.Context, tx *sql.Tx, user uuid.UUID, kind, title, body, entityType string, entityID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), user, kind, title, body, entityType, entityID)
	return err
}
