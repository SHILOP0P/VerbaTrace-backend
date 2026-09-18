//go:build integration

package analysis_instruction

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
	instructionRepo "verbatrace/monolit/internal/repository/analysis_instruction"
	companyRepo "verbatrace/monolit/internal/repository/company"
	departmentRepo "verbatrace/monolit/internal/repository/department"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Creating and reordering carry the company in the body, where the route guard
// cannot see it, so the service itself refuses them in a frozen company.
func TestInstructionsCannotBeCreatedOrReorderedInAFrozenCompany(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()

	ownerID := repositorytest.CreateUser(t, db)
	companyID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, created_at, lifecycle_state, freeze_reason) VALUES ($1,'Frozen','@frozen-instr',$2,now(),'frozen','downgrade')`, companyID, ownerID)
	require.NoError(t, err)
	repositorytest.InsertCompanyMember(t, db, companyID, ownerID, "company_manager", "active")

	service := NewService(instructionRepo.NewRepository(db), companyRepo.NewRepository(db), departmentRepo.NewRepository(db), nil, nil)
	service.SetCompanyStateReader(db)
	company := uuid.NullUUID{UUID: companyID, Valid: true}

	_, err = service.Create(ctx, models.CreateAnalysisInstructionInput{
		Scope: models.AnalysisInstructionScopeCompany, CompanyUUID: company, CreatedByUserUUID: ownerID,
		Title: "Стандарт", OriginalFilename: "standard.md", MimeType: "text/markdown",
		Content: strings.NewReader("- Выяснил бюджет"),
	})
	require.ErrorIs(t, err, models.ErrCompanyFrozen)

	err = service.Reorder(ctx, models.ReorderAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopeCompany, CompanyUUID: company, UserUUID: ownerID,
		Items: []models.ReorderAnalysisInstructionItem{{ID: uuid.New(), SortOrder: 1}},
	})
	require.ErrorIs(t, err, models.ErrCompanyFrozen)
}
