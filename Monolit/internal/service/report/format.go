package report

import "verbatrace/monolit/internal/models"

func normalizeFormat(format models.ReportFormat) (models.ReportFormat, error) {
	switch format {
	case models.ReportFormatPDF, models.ReportFormatDOCX, models.ReportFormatMD, models.ReportFormatXLSX:
		return format, nil
	default:
		return "", models.ErrUnsupportedReportFormat
	}
}

func contentType(format models.ReportFormat) string {
	switch format {
	case models.ReportFormatPDF:
		return "application/pdf"
	case models.ReportFormatDOCX:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case models.ReportFormatMD:
		return "text/markdown; charset=utf-8"
	case models.ReportFormatXLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

func fileExtension(format models.ReportFormat) string {
	switch format {
	case models.ReportFormatPDF:
		return ".pdf"
	case models.ReportFormatDOCX:
		return ".docx"
	case models.ReportFormatMD:
		return ".md"
	case models.ReportFormatXLSX:
		return ".xlsx"
	default:
		return ""
	}
}
