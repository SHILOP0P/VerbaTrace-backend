package repositorytest

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// InsertCompanyMember seeds a membership directly. The product has no direct
// "add member" operation any more, so integration fixtures write the row
// themselves instead of going through a repository that no longer exists.
func InsertCompanyMember(t *testing.T, db *sql.DB, companyID uuid.UUID, userID uuid.UUID, role string, status string) {
	t.Helper()

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO company_members (company_uuid, user_uuid, role, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (company_uuid, user_uuid)
		DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status
	`, companyID, userID, role, status)
	require.NoError(t, err)
}

// InsertDepartmentMember seeds a department membership directly.
func InsertDepartmentMember(t *testing.T, db *sql.DB, departmentID uuid.UUID, userID uuid.UUID, role string, status string) {
	t.Helper()

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO department_members (department_uuid, user_uuid, role, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (department_uuid, user_uuid)
		DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status
	`, departmentID, userID, role, status)
	require.NoError(t, err)
}
