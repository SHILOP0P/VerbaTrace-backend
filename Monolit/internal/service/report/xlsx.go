package report

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

func generateXLSXReport(data ReportData) ([]byte, error) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	analysis := data.StructuredAnalysis()

	metaSheet := "Метаданные"
	if err := file.SetSheetName("Sheet1", metaSheet); err != nil {
		return nil, fmt.Errorf("rename metadata sheet: %w", err)
	}
	meta := [][]any{
		{"Поле", "Значение"},
		{"Название", data.Call.Title},
		{"Статус звонка", string(data.Call.Status)},
		{"Длительность, сек.", data.Call.DurationSeconds},
		{"Создан", data.Call.CreatedAt.Format(timeLayout)},
		{"Отчет создан", data.GeneratedAt.Format(timeLayout)},
	}
	if data.TranscriptionOnly {
		meta = append(meta, []any{"Версия транскрипции", data.TranscriptionRevision})
	} else {
		meta = append(meta, []any{"Статус анализа", string(data.Analysis.Status)})
	}
	setRows(file, metaSheet, meta)

	if !data.TranscriptionOnly {
		analysisSheet := "Анализ"
		if _, err := file.NewSheet(analysisSheet); err != nil {
			return nil, fmt.Errorf("create analysis sheet: %w", err)
		}
		setRows(file, analysisSheet, sectionRows(data.Sections()))

		if len(analysis.ClientQuestions) > 0 {
			questionsSheet := "Вопросы"
			if _, err := file.NewSheet(questionsSheet); err != nil {
				return nil, fmt.Errorf("create questions sheet: %w", err)
			}
			rows := [][]any{{"Вопрос", "Ответ менеджера", "Статус", "Цитаты"}}
			for _, question := range analysis.ClientQuestions {
				rows = append(rows, []any{
					question.Question,
					question.ManagerAnswer,
					answerStatusLabel(question.AnswerStatus),
					strings.Join(question.EvidenceQuotes, "\n"),
				})
			}
			setRows(file, questionsSheet, rows)
		}

		if len(analysis.CriteriaResults) > 0 {
			criteriaSheet := "Критерии"
			if _, err := file.NewSheet(criteriaSheet); err != nil {
				return nil, fmt.Errorf("create criteria sheet: %w", err)
			}
			rows := [][]any{{"Критерий", "Результат", "Цитаты"}}
			for _, criterion := range analysis.CriteriaResults {
				rows = append(rows, []any{
					criterion.InstructionTitle,
					criterion.Result,
					strings.Join(criterion.EvidenceQuotes, "\n"),
				})
			}
			setRows(file, criteriaSheet, rows)
		}

		// Schema v3 keeps every question and requirement as a card in items[];
		// criteria_results above is empty for it.
		if analysis.SchemaVersion == 3 && len(analysis.Items) > 0 {
			itemsSheet := "Карточки"
			if _, err := file.NewSheet(itemsSheet); err != nil {
				return nil, fmt.Errorf("create items sheet: %w", err)
			}
			setRows(file, itemsSheet, universalItemRows(analysis.Items, data.analysisReader()))
		}
	}
	if data.TranscriptionText != "" {
		transcriptionSheet := "Транскрипция"
		if _, err := file.NewSheet(transcriptionSheet); err != nil {
			return nil, fmt.Errorf("create transcription sheet: %w", err)
		}
		rows := [][]any{{"Строка", "Текст"}}
		for index, paragraph := range splitParagraphs(data.TranscriptionText) {
			rows = append(rows, []any{index + 1, paragraph})
		}
		setRows(file, transcriptionSheet, rows)
	}

	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		return nil, fmt.Errorf("generate xlsx report: %w", err)
	}

	return buffer.Bytes(), nil
}

func setRows(file *excelize.File, sheet string, rows [][]any) {
	for rowIndex, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, rowIndex+1)
		_ = file.SetSheetRow(sheet, cell, &row)
	}
	headerStyle, _ := file.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"263449"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	bodyStyle, _ := file.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "263449"}, Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	stripeStyle, _ := file.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "263449"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"FFF0E7"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		end, _ := excelize.CoordinatesToCellName(len(row), i+1)
		start, _ := excelize.CoordinatesToCellName(1, i+1)
		style := bodyStyle
		if i == 0 {
			style = headerStyle
		} else if i%2 == 0 {
			style = stripeStyle
		}
		_ = file.SetCellStyle(sheet, start, end, style)
	}
	_ = file.SetRowHeight(sheet, 1, 28)
	_ = file.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	_ = file.SetColWidth(sheet, "A", "A", 24)
	_ = file.SetColWidth(sheet, "B", "B", 100)
	_ = file.SetColWidth(sheet, "C", "D", 60)
}

func sectionRows(sections []reportSection) [][]any {
	rows := [][]any{{"Раздел", "Поле", "Значение"}}
	for _, section := range sections {
		for _, row := range section.Rows {
			if row.Value != "" {
				rows = append(rows, []any{section.Title, row.Label, row.Value})
			}
			if len(row.List) > 0 {
				rows = append(rows, []any{section.Title, row.Label, strings.Join(row.List, "\n")})
			}
		}
	}
	return rows
}

func universalItemRows(items []universalItem, reader analysisReader) [][]any {
	rows := [][]any{{"Вид", "Пункт", "Статус", "Оценка", "Вес", "Разбор", "Цитаты"}}
	for index, item := range items {
		score, weight := any(""), any("")
		if item.Score != nil {
			score = *item.Score
		}
		if item.Weight != nil {
			weight = *item.Weight
		}
		quotes := make([]string, 0, len(item.Evidence))
		for _, proof := range item.Evidence {
			quotes = append(quotes, reader.quote(proof))
		}
		rows = append(rows, []any{itemKindLabel(item.Kind), reader.itemTitle(item, index), criterionStatusLabel(item.Status), score, weight, reader.text(item.Explanation), strings.Join(quotes, "\n")})
	}
	return rows
}

func itemKindLabel(kind string) string {
	switch kind {
	case "question":
		return "Вопрос"
	case "episode":
		return "Эпизод"
	case "requirement":
		return "Требование"
	default:
		return kind
	}
}
