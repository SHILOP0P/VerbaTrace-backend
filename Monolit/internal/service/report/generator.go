package report

import (
	"bytes"
	"fmt"

	"verbatrace/monolit/internal/models"
)

func generateReport(format models.ReportFormat, data ReportData) ([]byte, error) {
	switch format {
	case models.ReportFormatMD:
		return generateMarkdownReport(data), nil
	case models.ReportFormatDOCX:
		return generateDOCXReport(data)
	case models.ReportFormatPDF:
		return generatePDFReport(data)
	case models.ReportFormatXLSX:
		return generateXLSXReport(data)
	default:
		return nil, models.ErrUnsupportedReportFormat
	}
}

func generateMarkdownReport(data ReportData) []byte {
	var b bytes.Buffer

	fmt.Fprintf(&b, "# Отчет по звонку: %s\n\n", data.Call.Title)
	fmt.Fprintf(&b, "- ID звонка: `%s`\n", data.Call.ID.String())
	fmt.Fprintf(&b, "- Статус звонка: `%s`\n", data.Call.Status)
	fmt.Fprintf(&b, "- Длительность: %d сек.\n", data.Call.DurationSeconds)
	fmt.Fprintf(&b, "- Создан: %s\n", data.Call.CreatedAt.Format(timeLayout))
	fmt.Fprintf(&b, "- Отчет создан: %s\n\n", data.GeneratedAt.Format(timeLayout))

	fmt.Fprintf(&b, "## Анализ\n\n")
	fmt.Fprintf(&b, "- ID анализа: `%s`\n", data.Analysis.ID.String())
	fmt.Fprintf(&b, "- Статус анализа: `%s`\n", data.Analysis.Status)
	fmt.Fprintf(&b, "- Провайдер: `%s`\n", data.Analysis.Provider)
	if data.Analysis.Model != nil {
		fmt.Fprintf(&b, "- Модель: `%s`\n", *data.Analysis.Model)
	}
	fmt.Fprintln(&b)

	for _, section := range data.Sections() {
		fmt.Fprintf(&b, "## %s\n\n", section.Title)
		for _, row := range section.Rows {
			if row.Label != "" {
				fmt.Fprintf(&b, "**%s:** ", row.Label)
			}
			if row.Value != "" {
				fmt.Fprintln(&b, row.Value)
			}
			for _, item := range row.List {
				fmt.Fprintf(&b, "- %s\n", item)
			}
			fmt.Fprintln(&b)
		}
		fmt.Fprintln(&b)
	}

	return b.Bytes()
}

const timeLayout = "2006-01-02 15:04:05 UTC"
