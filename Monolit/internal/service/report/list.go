package report

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const (
	defaultReportsListLimit = 20
	maxReportsListLimit     = 100
)

func (s *Service) List(ctx context.Context, input models.ListReportsInput) (models.ListReportsResult, error) {
	if input.UserUUID == uuid.Nil {
		return models.ListReportsResult{}, models.ErrInvalidReportInput
	}

	if input.Format != "" {
		if _, err := normalizeFormat(input.Format); err != nil {
			return models.ListReportsResult{}, err
		}
	}
	if input.Status != "" && !validReportStatus(input.Status) {
		return models.ListReportsResult{}, models.ErrInvalidReportInput
	}
	if input.Sort == "" {
		input.Sort = models.ReportSortCreatedAt
	}
	if !validReportSort(input.Sort) {
		return models.ListReportsResult{}, models.ErrInvalidReportInput
	}
	if input.Order == "" {
		input.Order = models.SortOrderDesc
	}
	if input.Order != models.SortOrderAsc && input.Order != models.SortOrderDesc {
		return models.ListReportsResult{}, models.ErrInvalidReportInput
	}
	if input.Limit == 0 {
		input.Limit = defaultReportsListLimit
	}
	if input.Limit < 0 || input.Limit > maxReportsListLimit || input.Offset < 0 {
		return models.ListReportsResult{}, models.ErrInvalidReportInput
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return models.ListReportsResult{}, models.ErrInvalidReportInput
	}

	return s.reportRepository.List(ctx, input, s.now())
}

func (s *Service) CreateGlobal(ctx context.Context, input models.CreateGlobalReportInput) (models.ReportExport, error) {
	if input.UserUUID == uuid.Nil {
		return models.ReportExport{}, models.ErrInvalidReportInput
	}
	if _, err := normalizeFormat(input.Format); err != nil {
		return models.ReportExport{}, err
	}

	switch input.Scope {
	case models.ReportScopeCall:
		if !input.CallUUID.Valid {
			return models.ReportExport{}, models.ErrInvalidReportInput
		}
		return s.Create(ctx, models.CreateReportInput{
			CallUUID: input.CallUUID.UUID,
			UserUUID: input.UserUUID,
			Format:   input.Format,
		})
	case models.ReportScopeCompany,
		models.ReportScopeDepartment,
		models.ReportScopeManager,
		models.ReportScopePeriod:
		return models.ReportExport{}, models.ErrReportScopeNotImplemented
	default:
		return models.ReportExport{}, models.ErrUnsupportedReportScope
	}
}

func validReportStatus(status models.ReportStatus) bool {
	switch status {
	case models.ReportStatusPending, models.ReportStatusReady, models.ReportStatusFailed:
		return true
	default:
		return false
	}
}

func validReportSort(sort models.ReportSortField) bool {
	switch sort {
	case models.ReportSortCreatedAt, models.ReportSortUpdatedAt:
		return true
	default:
		return false
	}
}
