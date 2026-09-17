//go:build integration

package company

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) createCall(companyID uuid.UUID, departmentID uuid.NullUUID, uploader uuid.UUID) uuid.UUID {
	callID := uuid.New()
	// The scope and the department are tied together by a check constraint, so
	// the fixture has to say the same thing twice.
	scope := "company"
	if departmentID.Valid {
		scope = "department"
	}
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, department_uuid, visibility_scope, created_at)
		VALUES ($1, 'call', 'analyzed', '/tmp/a.mp3', 'a.mp3', 'audio/mpeg', 1, $2, $3, $4, $5, now())
	`, callID, uploader, companyID, departmentID, scope)
	s.Require().NoError(err)

	return callID
}

func (s *RepositorySuite) createDepartmentRow(companyID uuid.UUID) uuid.UUID {
	departmentID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1, $2, $3)`,
		departmentID, companyID, "dept-"+departmentID.String()[:8])
	s.Require().NoError(err)

	return departmentID
}

// Moving data between two of the owner's companies is the only alternative to
// losing it when a company is deleted, and the lifecycle deletes companies
// whether or not this exists. It has to move the call and everything the call
// owns, and it must not leave a department behind that the receiving company
// does not have.
func (s *RepositorySuite) TestTransferCompanyDataMovesCallsWithWhatTheyOwn() {
	source, owner := s.createCompanyWithManager()
	target := s.createCompanyFor(owner)
	department := s.createDepartmentRow(source.ID)

	first := s.createCall(source.ID, uuid.NullUUID{UUID: department, Valid: true}, owner.ID)
	second := s.createCall(source.ID, uuid.NullUUID{}, owner.ID)

	folderID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO call_folders (folder_uuid, company_uuid, department_uuid, created_by_user_uuid, name, scope)
		VALUES ($1, $2, $3, $4, 'folder', 'department')`, folderID, source.ID, department, owner.ID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `INSERT INTO call_folder_assignments (folder_uuid, call_uuid, assigned_by_user_uuid) VALUES ($1, $2, $3)`, folderID, first, owner.ID)
	s.Require().NoError(err)

	result, err := s.repository.TransferCompanyData(s.ctx, models.TransferCompanyDataInput{
		OwnerUserUUID:     owner.ID,
		SourceCompanyUUID: source.ID,
		TargetCompanyUUID: target.ID,
		IncludeCalls:      true,
		IncludeFolders:    true,
		Reason:            "closing the old company",
	})
	s.Require().NoError(err)
	s.Require().Equal(int64(2), result.Calls)
	s.Require().Equal(int64(1), result.Folders)

	for _, callID := range []uuid.UUID{first, second} {
		var company uuid.UUID
		var departmentUUID uuid.NullUUID
		var scope string
		s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT company_uuid, department_uuid, visibility_scope FROM calls WHERE call_uuid=$1`, callID).Scan(&company, &departmentUUID, &scope))
		s.Require().Equal(target.ID, company)
		s.Require().False(departmentUUID.Valid, "a department of the old company would point nowhere")
		s.Require().Equal("company", scope, "the scope has to widen with the department, or the row would not be valid")
	}

	var folderCompany uuid.UUID
	var folderScope string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT company_uuid, scope FROM call_folders WHERE folder_uuid=$1`, folderID).Scan(&folderCompany, &folderScope))
	s.Require().Equal(target.ID, folderCompany)
	s.Require().Equal("company", folderScope)

	// The record of the move is what the owner reads afterwards.
	transfers, err := s.repository.ListCompanyDataTransfers(s.ctx, owner.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(transfers, 1)
	s.Require().Equal(int64(2), transfers[0].Calls)
	s.Require().Equal("closing the old company", transfers[0].Reason)
}

// Only the selected calls move when a selection is given, and nothing else in
// the source company is touched.
func (s *RepositorySuite) TestTransferCompanyDataMovesOnlyTheSelectedCalls() {
	source, owner := s.createCompanyWithManager()
	target := s.createCompanyFor(owner)

	chosen := s.createCall(source.ID, uuid.NullUUID{}, owner.ID)
	untouched := s.createCall(source.ID, uuid.NullUUID{}, owner.ID)

	result, err := s.repository.TransferCompanyData(s.ctx, models.TransferCompanyDataInput{
		OwnerUserUUID:     owner.ID,
		SourceCompanyUUID: source.ID,
		TargetCompanyUUID: target.ID,
		CallUUIDs:         []uuid.UUID{chosen},
		IncludeCalls:      true,
	})
	s.Require().NoError(err)
	s.Require().Equal(int64(1), result.Calls)

	var company uuid.UUID
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT company_uuid FROM calls WHERE call_uuid=$1`, untouched).Scan(&company))
	s.Require().Equal(source.ID, company)
}

// The two companies have to belong to the same person, and the receiving one has
// to work: moving data into a frozen company would only hide it.
func (s *RepositorySuite) TestTransferCompanyDataRefusesForeignAndFrozenCompanies() {
	source, owner := s.createCompanyWithManager()
	foreign, _ := s.createCompanyWithManager()
	target := s.createCompanyFor(owner)

	_, err := s.repository.TransferCompanyData(s.ctx, models.TransferCompanyDataInput{
		OwnerUserUUID:     owner.ID,
		SourceCompanyUUID: source.ID,
		TargetCompanyUUID: foreign.ID,
		IncludeCalls:      true,
	})
	s.Require().ErrorIs(err, models.ErrForbidden)

	s.Require().NoError(s.repository.FreezeCompany(s.ctx, target.ID, models.CompanyFreezeReasonDowngrade, time.Now().UTC()))
	_, err = s.repository.TransferCompanyData(s.ctx, models.TransferCompanyDataInput{
		OwnerUserUUID:     owner.ID,
		SourceCompanyUUID: source.ID,
		TargetCompanyUUID: target.ID,
		IncludeCalls:      true,
	})
	s.Require().ErrorIs(err, models.ErrCompanyFrozen)
}
