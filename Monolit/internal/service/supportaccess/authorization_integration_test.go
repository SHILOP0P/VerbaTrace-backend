//go:build integration

package supportaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
)

func TestAuthorizationRequiresActiveExactScopeGrant(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()
	service := NewService(db)
	adminID, ownerID, memberID := uuid.New(), uuid.New(), uuid.New()
	for index, id := range []uuid.UUID{adminID, ownerID, memberID} {
		email := []string{"support@example.test", "owner@example.test", "member@example.test"}[index]
		role := []string{"admin", "user", "user"}[index]
		_, err := db.ExecContext(ctx, `WITH account AS (INSERT INTO users(user_uuid,email,password_hash,role,created_at) VALUES($1,$2,'hash',$3,now()) RETURNING user_uuid) INSERT INTO user_profiles(user_uuid,full_name,full_surname,username) SELECT user_uuid,'Test','User',$4 FROM account`, id, email, role, "u"+id.String()[:8])
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	companyID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO companies(company_uuid,name,tag,manager_user_uuid,member_limit) VALUES($1,'Company','@company',$2,10)`, companyID, ownerID); err != nil {
		t.Fatalf("create company: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO company_members(company_uuid,user_uuid,role,status) VALUES($1,$2,'company_manager','active'),($1,$3,'employee','active')`, companyID, ownerID, memberID); err != nil {
		t.Fatalf("create company members: %v", err)
	}
	if err := service.AuthorizeCompany(ctx, adminID, companyID, "calls", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("access without grant must be forbidden, got %v", err)
	}

	requestID, grantID := uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO support_access_requests(request_uuid,requested_by_user_uuid,approver_user_uuid,subject_type,subject_company_uuid,reason,requested_resources,requested_commands,requested_duration_minutes,status,expires_at,decided_at) VALUES($1,$2,$3,'company',$4,'Need diagnostic access',ARRAY['calls'],ARRAY['diagnose'],30,'approved',now()+interval '1 hour',now())`, requestID, adminID, ownerID, companyID); err != nil {
		t.Fatalf("create grant request: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO support_access_grants(grant_uuid,request_uuid,grantee_user_uuid,granted_by_user_uuid,subject_type,subject_company_uuid,resource_allowlist,command_allowlist,valid_from,expires_at) VALUES($1,$2,$3,$4,'company',$5,ARRAY['calls'],ARRAY['diagnose'],now()-interval '1 minute',now()+interval '30 minutes')`, grantID, requestID, adminID, ownerID, companyID); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if err := service.AuthorizeCompany(ctx, adminID, companyID, "calls", ""); err != nil {
		t.Fatalf("active company grant rejected: %v", err)
	}
	if err := service.AuthorizeUser(ctx, adminID, memberID, "calls", "diagnose"); err != nil {
		t.Fatalf("active company grant must cover active member: %v", err)
	}
	if err := service.AuthorizeCompany(ctx, adminID, companyID, "actions", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong resource must be forbidden, got %v", err)
	}
	if err := service.AuthorizeCompany(ctx, adminID, companyID, "calls", "retry_ingest"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong command must be forbidden, got %v", err)
	}
	var events int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM support_access_events WHERE grant_uuid=$1 AND event_type='resource_accessed'`, grantID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("expected two audited accesses, count=%d err=%v", events, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE support_access_grants SET revoked_at=$2,revoked_by_user_uuid=$3,revoke_reason='done' WHERE grant_uuid=$1`, grantID, time.Now().UTC(), ownerID); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if err := service.AuthorizeCompany(ctx, adminID, companyID, "calls", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked grant must be forbidden, got %v", err)
	}
	expiredRequestID, expiredGrantID := uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO support_access_requests(request_uuid,requested_by_user_uuid,approver_user_uuid,subject_type,subject_company_uuid,reason,requested_resources,requested_commands,requested_duration_minutes,status,created_at,expires_at,decided_at) VALUES($1,$2,$3,'company',$4,'Expired diagnostic access',ARRAY['calls'],ARRAY[]::text[],30,'approved',now()-interval '2 hours',now()-interval '1 hour',now()-interval '90 minutes')`, expiredRequestID, adminID, ownerID, companyID); err != nil {
		t.Fatalf("create expired grant request: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO support_access_grants(grant_uuid,request_uuid,grantee_user_uuid,granted_by_user_uuid,subject_type,subject_company_uuid,resource_allowlist,command_allowlist,valid_from,expires_at) VALUES($1,$2,$3,$4,'company',$5,ARRAY['calls'],ARRAY[]::text[],now()-interval '2 hours',now()-interval '1 hour')`, expiredGrantID, expiredRequestID, adminID, ownerID, companyID); err != nil {
		t.Fatalf("create expired grant: %v", err)
	}
	if err := service.AuthorizeCompany(ctx, adminID, companyID, "calls", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expired grant must be forbidden, got %v", err)
	}
}
