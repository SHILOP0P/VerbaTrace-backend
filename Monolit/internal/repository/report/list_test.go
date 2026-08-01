package report

import (
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBuildListReportFilters(t *testing.T) {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	input := models.ListReportsInput{
		UserUUID:       uuid.New(),
		Format:         models.ReportFormatPDF,
		Status:         models.ReportStatusReady,
		CompanyUUID:    uuid.NullUUID{UUID: uuid.New(), Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		CallUUID:       uuid.NullUUID{UUID: uuid.New(), Valid: true},
		From:           &from,
		To:             &to,
	}

	where, args := buildListReportFilters(input, now)

	require.Len(t, args, 9)
	require.Contains(t, where, "r.expires_at > $2")
	require.Contains(t, where, "r.format = $3")
	require.Contains(t, where, "r.status = $4")
	require.Contains(t, where, "c.company_uuid = $5")
	require.Contains(t, where, "c.department_uuid = $6")
	require.Contains(t, where, "r.call_uuid = $7")
	require.Contains(t, where, "r.created_at >= $8")
	require.Contains(t, where, "r.created_at <= $9")
	require.Contains(t, where, "company_members")
	require.Contains(t, where, "department_members")
}

func TestReportSortHelpers(t *testing.T) {
	require.Equal(t, "r.created_at", reportSortColumn(models.ReportSortCreatedAt))
	require.Equal(t, "r.updated_at", reportSortColumn(models.ReportSortUpdatedAt))
	require.Equal(t, "ASC", reportSortOrder(models.SortOrderAsc))
	require.Equal(t, "DESC", reportSortOrder(models.SortOrderDesc))
	require.True(t, strings.Contains(prefixedReportColumns("r"), "r.report_uuid"))
}
