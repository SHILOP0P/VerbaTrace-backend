//go:build integration

package bitrix24

import (
	"context"
	"errors"
	"testing"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
)

func TestCompanyManagerMappingDoesNotRequireDepartment(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()

	managerID, employeeID := uuid.New(), uuid.New()
	for _, fixture := range []struct {
		id       uuid.UUID
		email    string
		username string
	}{
		{managerID, "mapping-manager@example.test", "mapping-manager"},
		{employeeID, "mapping-employee@example.test", "mapping-employee"},
	} {
		if _, err := db.ExecContext(ctx, `WITH account AS (
			INSERT INTO users(user_uuid,email,password_hash,role,created_at)
			VALUES($1,$2,'hash','user',now()) RETURNING user_uuid
		) INSERT INTO user_profiles(user_uuid,full_name,full_surname,username)
		  SELECT user_uuid,'Mapping','User',$3 FROM account`, fixture.id, fixture.email, fixture.username); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}

	companyID, departmentID := uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO companies(company_uuid,name,tag,manager_user_uuid,member_limit)
		VALUES($1,'Mapping Company','@mapping-company',$2,10)`, companyID, managerID); err != nil {
		t.Fatalf("create company: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO company_members(company_uuid,user_uuid,role,status)
		VALUES($1,$2,'company_manager','active'),($1,$3,'employee','active')`, companyID, managerID, employeeID); err != nil {
		t.Fatalf("create company members: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO departments(department_uuid,company_uuid,name) VALUES($1,$2,'Sales')`, departmentID, companyID); err != nil {
		t.Fatalf("create department: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO department_members(department_uuid,user_uuid,role,status)
		VALUES($1,$2,'employee','active')`, departmentID, employeeID); err != nil {
		t.Fatalf("create department member: %v", err)
	}

	billingID, applicationID, connectionID := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid)
		VALUES($1,'company',$2)`, billingID, companyID); err != nil {
		t.Fatalf("create billing account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO developer_applications(application_uuid,owner_type,company_uuid,billing_account_uuid,name,environment,status)
		VALUES($1,'company',$2,$3,'Mapping App','sandbox','active')`, applicationID, companyID, billingID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO integration_connections(connection_uuid,application_uuid,company_uuid,created_by_user_uuid,name,provider,status)
		VALUES($1,$2,$3,$4,'Bitrix24','bitrix24','draft')`, connectionID, applicationID, companyID, managerID); err != nil {
		t.Fatalf("create connection: %v", err)
	}

	service := NewService(db, nil, Config{})
	managerChange := models.BitrixMappingChange{
		ExternalUserID: "1",
		InternalUserID: uuid.NullUUID{UUID: managerID, Valid: true},
		Status:         "mapped",
	}
	preview, err := service.PreviewExternalUserMappings(ctx, connectionID, managerID, []models.BitrixMappingChange{managerChange})
	if err != nil {
		t.Fatalf("preview company manager mapping without department: %v", err)
	}
	if preview.ChangesCount != 1 || preview.Items[0].AfterDepartmentID.Valid {
		t.Fatalf("unexpected manager preview: %#v", preview)
	}
	result, err := service.BulkUpdateExternalUserMappings(ctx, models.BulkUpdateBitrixMappingsInput{
		ConnectionID: connectionID,
		ActorID:      managerID,
		PreviewHash:  preview.PreviewHash,
		RequestKey:   "manager-map-without-department",
		Changes:      []models.BitrixMappingChange{managerChange},
	})
	if err != nil {
		t.Fatalf("apply company manager mapping without department: %v", err)
	}
	if !result.Created || len(result.Mappings) != 1 || result.Mappings[0].DepartmentID.Valid {
		t.Fatalf("unexpected manager mapping result: %#v", result)
	}

	employeeWithoutDepartment := models.BitrixMappingChange{
		ExternalUserID: "2",
		InternalUserID: uuid.NullUUID{UUID: employeeID, Valid: true},
		Status:         "mapped",
	}
	if _, err = service.PreviewExternalUserMappings(ctx, connectionID, managerID, []models.BitrixMappingChange{employeeWithoutDepartment}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("employee mapping without department must be rejected, got %v", err)
	}

	employeeWithDepartment := employeeWithoutDepartment
	employeeWithDepartment.DepartmentID = uuid.NullUUID{UUID: departmentID, Valid: true}
	if _, err = service.PreviewExternalUserMappings(ctx, connectionID, managerID, []models.BitrixMappingChange{employeeWithDepartment}); err != nil {
		t.Fatalf("employee mapping with active department must be allowed: %v", err)
	}
}
