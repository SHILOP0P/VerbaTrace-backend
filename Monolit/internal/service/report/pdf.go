package report

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

func generatePDFReport(data ReportData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	fontPath, err := reportFontPath()
	if err != nil {
		return nil, err
	}
	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("read pdf report font: %w", err)
	}
	pdf.AddUTF8FontFromBytes("report", "", fontBytes)
	pdf.SetFont("report", "", 12)
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AliasNbPages("")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-14)
		pdf.SetFont("report", "", 9)
		pdf.SetTextColor(110, 120, 135)
		pdf.CellFormat(0, 8, fmt.Sprintf("VerbaTrace · %d / {nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()
	pdf.SetTextColor(38, 52, 73)

	pdf.SetFillColor(38, 52, 73)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFontSize(20)
	pdf.MultiCell(0, 10, data.Title(), "", "L", true)
	pdf.Ln(5)
	pdf.SetTextColor(70, 80, 95)
	writePDFLine(pdf, 11, "ID звонка: "+data.Call.ID.String())
	writePDFLine(pdf, 11, "Статус звонка: "+string(data.Call.Status))
	writePDFLine(pdf, 11, fmt.Sprintf("Длительность: %d сек.", data.Call.DurationSeconds))
	writePDFLine(pdf, 11, "Создан: "+data.Call.CreatedAt.Format(timeLayout))
	writePDFLine(pdf, 11, "Отчет создан: "+data.GeneratedAt.Format(timeLayout))
	pdf.Ln(4)

	if !data.TranscriptionOnly {
		writePDFLine(pdf, 14, "Анализ")
		writePDFLine(pdf, 11, "ID анализа: "+data.Analysis.ID.String())
		writePDFLine(pdf, 11, "Статус анализа: "+string(data.Analysis.Status))
		writePDFLine(pdf, 11, "Провайдер: "+data.Analysis.Provider)
		if data.Analysis.Model != nil {
			writePDFLine(pdf, 11, "Модель: "+*data.Analysis.Model)
		}
		pdf.Ln(2)

	}

	for _, section := range data.Sections() {
		pdf.Ln(4)
		if pdf.GetY() > 245 {
			pdf.AddPage()
		}
		pdf.SetFillColor(255, 237, 228)
		pdf.SetTextColor(157, 58, 25)
		pdf.SetFontSize(14)
		pdf.MultiCell(0, 9, section.Title, "", "L", true)
		pdf.Ln(3)
		pdf.SetTextColor(38, 52, 73)
		for _, row := range section.Rows {
			if row.Label != "" && row.Value != "" {
				writePDFBlock(pdf, row.Label+": "+row.Value)
			} else if row.Value != "" {
				if strings.HasPrefix(section.Title, "Транскрипция") {
					writePDFTranscript(pdf, row.Value)
				} else {
					writePDFBlock(pdf, row.Value)
				}
			} else if row.Label != "" {
				writePDFBlock(pdf, row.Label+":")
			}
			for _, item := range row.List {
				writePDFBlock(pdf, "• "+item)
			}
			pdf.Ln(1)
		}
	}

	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("generate pdf report: %w", err)
	}

	return buffer.Bytes(), nil
}

func reportFontPath() (string, error) {
	candidates := []string{
		`C:\Windows\Fonts\arial.ttf`,
		`C:\Windows\Fonts\segoeui.ttf`,
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("report pdf font not found")
}

func writePDFLine(pdf *gofpdf.Fpdf, size float64, text string) {
	pdf.SetFontSize(size)
	pdf.MultiCell(0, 7, text, "", "L", false)
}

func writePDFBlock(pdf *gofpdf.Fpdf, text string) {
	pdf.SetFontSize(10)
	for _, paragraph := range splitParagraphs(text) {
		pdf.MultiCell(0, 5, paragraph, "", "L", false)
	}
}

// Render each turn line independently so long utterances can continue across pages.
func writePDFTranscript(pdf *gofpdf.Fpdf, text string) {
	for _, paragraph := range splitParagraphs(text) {
		if strings.TrimSpace(paragraph) == "" {
			pdf.Ln(3)
			continue
		}
		if transcriptHeading.MatchString(paragraph) {
			if pdf.GetY() > 252 {
				pdf.AddPage()
			}
			pdf.SetFontSize(10)
			pdf.SetTextColor(157, 58, 25)
			pdf.SetFillColor(255, 237, 228)
			pdf.MultiCell(0, 7, paragraph, "L", "L", true)
		} else {
			pdf.SetFontSize(11)
			pdf.SetTextColor(38, 52, 73)
			pdf.SetFillColor(246, 248, 251)
			pdf.MultiCell(0, 6, paragraph, "L", "L", true)
		}
	}
	pdf.SetTextColor(38, 52, 73)
}
