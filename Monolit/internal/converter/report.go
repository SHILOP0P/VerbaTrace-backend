package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func ReportModelToAPI(report models.ReportExport) (dto.ReportResponse, error) {
	var downloadURL *string
	if report.Status == models.ReportStatusReady {
		value := "/api/v1/reports/" + report.ID.String() + "/download"
		downloadURL = &value
	}

	return dto.ReportResponse{
		ID:                  report.ID.String(),
		CallUUID:            report.CallUUID.String(),
		AnalysisUUID:        report.AnalysisUUID.String(),
		RequestedByUserUUID: report.RequestedByUserUUID.String(),
		Format:              string(report.Format),
		Status:              string(report.Status),
		FileName:            report.FileName,
		ContentType:         report.ContentType,
		SizeBytes:           report.SizeBytes,
		ErrorMessage:        report.ErrorMessage,
		DownloadURL:         downloadURL,
		CreatedAt:           report.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           report.UpdatedAt.Format(time.RFC3339),
		ExpiresAt:           report.ExpiresAt.Format(time.RFC3339),
	}, nil
}

func ReportsModelToAPI(reports []models.ReportExport) (dto.ReportsResponse, error) {
	resp := dto.ReportsResponse{
		Reports: make([]dto.ReportResponse, 0, len(reports)),
	}

	for _, report := range reports {
		item, err := ReportModelToAPI(report)
		if err != nil {
			return dto.ReportsResponse{}, err
		}
		resp.Reports = append(resp.Reports, item)
	}

	return resp, nil
}

func GlobalReportsModelToAPI(result models.ListReportsResult) (dto.GlobalReportsResponse, error) {
	resp := dto.GlobalReportsResponse{
		Reports: make([]dto.ReportWithCallResponse, 0, len(result.Reports)),
		Total:   result.Total,
		Limit:   result.Limit,
		Offset:  result.Offset,
	}

	for _, item := range result.Reports {
		report, err := ReportModelToAPI(item.Report)
		if err != nil {
			return dto.GlobalReportsResponse{}, err
		}
		resp.Reports = append(resp.Reports, dto.ReportWithCallResponse{
			ReportResponse: report,
			Call:           reportCallSummaryToAPI(item.Call),
		})
	}

	return resp, nil
}

func reportCallSummaryToAPI(call models.ReportCallSummary) dto.ReportCallSummaryResponse {
	var companyUUID *string
	if call.CompanyUUID.Valid {
		value := call.CompanyUUID.UUID.String()
		companyUUID = &value
	}

	var departmentUUID *string
	if call.DepartmentUUID.Valid {
		value := call.DepartmentUUID.UUID.String()
		departmentUUID = &value
	}

	return dto.ReportCallSummaryResponse{
		ID:             call.ID.String(),
		Title:          call.Title,
		Status:         string(call.Status),
		CreatedAt:      call.CreatedAt.Format(time.RFC3339),
		CompanyUUID:    companyUUID,
		DepartmentUUID: departmentUUID,
	}
}
