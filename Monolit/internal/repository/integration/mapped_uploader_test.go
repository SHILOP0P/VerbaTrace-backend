//go:build integration

package integration

import (
	"crypto/sha256"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// A call from a portal belongs to the person who made it. Without a mapping
// there is nobody to give it to, so it is refused instead of landing on the
// administrator who connected the portal.
func (s *RepositorySuite) TestCompanyIngestRequiresMappedPortalUser() {
	owner := s.createUser("mapped-ingest-owner@example.com")
	companyID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit)
		VALUES ($1,'Mapped company',$2,$3,10)`, companyID, "@company_"+companyID.String()[:8], owner)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status)
		VALUES ($1,$2,'company_manager','active')`, companyID, owner)
	s.Require().NoError(err)

	departmentID := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1,$2,'Sales')`, departmentID, companyID)
	s.Require().NoError(err)

	employee := s.createUser("mapped-ingest-employee@example.com")
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status)
		VALUES ($1,$2,'employee','active')`, companyID, employee)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO department_members (department_uuid, user_uuid, role, status)
		VALUES ($1,$2,'employee','active')`, departmentID, employee)
	s.Require().NoError(err)

	app, err := s.billing.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{
		OwnerType: "company", OwnerUUID: companyID, CreatedByUserUUID: owner,
		Name: "Portal", Environment: "production", Capabilities: []string{"calls:write", "calls:read"},
	})
	s.Require().NoError(err)
	connections, err := s.repo.ListConnections(s.ctx, app.ID, owner)
	s.Require().NoError(err)
	s.Require().NotEmpty(connections)
	connection := connections[0]

	principal := models.IntegrationPrincipal{
		ApplicationUUID: app.ID, ConnectionUUID: connection.ID, BillingAccountUUID: app.BillingAccountUUID,
		ServiceAccountUUID: uuid.New(), Environment: "production", Scopes: []string{"calls:write"},
	}
	input := models.IngestCallInput{
		SchemaVersion: 2, ExternalEventID: "evt-mapped", ExternalCallID: "call-mapped",
		Title: "Portal call", RecordingURL: "https://media.example.test/call.mp3",
		OriginalFilename: "call.mp3",
		Participants:     []map[string]string{{"external_user_id": "17", "role": "employee"}},
	}
	hash := sha256.Sum256([]byte("mapped"))
	locator, err := s.cipher.Encrypt([]byte(input.RecordingURL), "ingest_items/application/"+app.ID.String()+"/recording_locator")
	s.Require().NoError(err)

	_, _, err = s.repo.AcceptURLIngest(s.ctx, principal, input, "idem-mapped-1", hash, locator)
	s.Require().ErrorIs(err, models.ErrExternalUserNotMapped)

	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO integration_external_user_mappings
			(mapping_uuid, connection_uuid, external_user_id, internal_user_uuid, department_uuid, status, mapped_by_user_uuid, mapped_at)
		VALUES ($1,$2,'17',$3,$4,'mapped',$5,now())`, uuid.New(), connection.ID, employee, departmentID, owner)
	s.Require().NoError(err)

	item, _, err := s.repo.AcceptURLIngest(s.ctx, principal, input, "idem-mapped-2", hash, locator)
	s.Require().NoError(err)

	// The call is attributed to the mapped employee and to their department.
	var uploader uuid.NullUUID
	var department uuid.NullUUID
	s.Require().NoError(s.db.QueryRowContext(s.ctx,
		`SELECT uploader_user_uuid, destination_department_uuid FROM ingest_items WHERE ingest_item_uuid=$1`, item.ID).
		Scan(&uploader, &department))
	s.Require().True(uploader.Valid)
	s.Require().Equal(employee, uploader.UUID)
	s.Require().True(department.Valid)
	s.Require().Equal(departmentID, department.UUID)
}
