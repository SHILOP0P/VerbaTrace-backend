package report

import (
	"database/sql"

	"verbatrace/monolit/internal/models"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReport(row rowScanner) (models.ReportExport, error) {
	var report models.ReportExport
	var format string
	var status string
	var storagePath sql.NullString
	var errorMessage sql.NullString

	if err := row.Scan(
		&report.ID,
		&report.CallUUID,
		&report.AnalysisUUID,
		&report.RequestedByUserUUID,
		&format,
		&status,
		&storagePath,
		&report.FileName,
		&report.ContentType,
		&report.SizeBytes,
		&errorMessage,
		&report.CreatedAt,
		&report.UpdatedAt,
		&report.ExpiresAt,
		&report.Content,
		&report.TranscriptionRevision,
	); err != nil {
		return models.ReportExport{}, err
	}

	report.Format = models.ReportFormat(format)
	report.Status = models.ReportStatus(status)
	if storagePath.Valid {
		report.StoragePath = &storagePath.String
	}
	if errorMessage.Valid {
		report.ErrorMessage = &errorMessage.String
	}

	return report, nil
}

func scanReportWithCall(row rowScanner) (models.ReportWithCall, int, error) {
	var item models.ReportWithCall
	var callStatus string
	var total int
	report, err := scanReportWithSuffix(row,
		&item.Call.ID,
		&item.Call.Title,
		&callStatus,
		&item.Call.CreatedAt,
		&item.Call.CompanyUUID,
		&item.Call.DepartmentUUID,
		&total,
	)
	if err != nil {
		return models.ReportWithCall{}, 0, err
	}

	item.Report = report
	item.Call.Status = models.CallStatus(callStatus)
	return item, total, nil
}

func scanReportWithSuffix(row rowScanner, suffix ...any) (models.ReportExport, error) {
	var report models.ReportExport
	var format string
	var status string
	var storagePath sql.NullString
	var errorMessage sql.NullString

	dest := []any{
		&report.ID,
		&report.CallUUID,
		&report.AnalysisUUID,
		&report.RequestedByUserUUID,
		&format,
		&status,
		&storagePath,
		&report.FileName,
		&report.ContentType,
		&report.SizeBytes,
		&errorMessage,
		&report.CreatedAt,
		&report.UpdatedAt,
		&report.ExpiresAt,
		&report.Content,
		&report.TranscriptionRevision,
	}
	dest = append(dest, suffix...)

	if err := row.Scan(dest...); err != nil {
		return models.ReportExport{}, err
	}

	report.Format = models.ReportFormat(format)
	report.Status = models.ReportStatus(status)
	if storagePath.Valid {
		report.StoragePath = &storagePath.String
	}
	if errorMessage.Valid {
		report.ErrorMessage = &errorMessage.String
	}

	return report, nil
}
