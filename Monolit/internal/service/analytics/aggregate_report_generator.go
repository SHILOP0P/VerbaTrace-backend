package analytics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/jung-kurt/gofpdf"
	"github.com/xuri/excelize/v2"
)

type AggregateReportData struct {
	Analysis    models.AggregateAnalysis
	GeneratedAt time.Time
}

type aggregateResult struct {
	Summary                string                                `json:"summary"`
	ExecutiveSummary       string                                `json:"executive_summary"`
	OverallAssessment      string                                `json:"overall_assessment"`
	KeyFindings            []aggregateFinding                    `json:"key_findings"`
	RecurringIssues        []aggregateRecurringIssue             `json:"recurring_issues"`
	SystemicIssues         []aggregateIssueDetail                `json:"systemic_issues"`
	SingleCallObservations []aggregateIssueDetail                `json:"single_call_observations"`
	WeakCriteria           []aggregateMetricDetail               `json:"weak_criteria"`
	ClientObjections       []aggregateMetricDetail               `json:"client_objections"`
	LossAndRiskPatterns    []aggregateIssueDetail                `json:"loss_and_risk_patterns"`
	Strengths              []string                              `json:"strengths"`
	Risks                  []string                              `json:"risks"`
	PriorityActions        []aggregatePriorityAction             `json:"priority_actions"`
	ManagerRecommendations []string                              `json:"manager_recommendations"`
	Confidence             any                                   `json:"confidence"`
	DetailedReport         aggregateDetailedReport               `json:"detailed_report"`
	SourceSummary          models.AggregateAnalysisSourceSummary `json:"source_summary"`
	AggregateStatistics    aggregateReportStatistics             `json:"aggregate_statistics"`
	CoverageNote           string                                `json:"coverage_note"`
}

type aggregateFinding struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Severity           string   `json:"severity"`
	AffectedCallsCount int      `json:"affected_calls_count"`
	AffectedShare      float64  `json:"affected_share"`
	EvidenceCallUUIDs  []string `json:"evidence_call_uuids"`
}

type aggregateRecurringIssue struct {
	Code           string  `json:"code"`
	Title          string  `json:"title"`
	Count          int     `json:"count"`
	Recommendation string  `json:"recommendation"`
	AffectedShare  float64 `json:"affected_share"`
}

type aggregateIssueDetail struct {
	Code               string  `json:"code"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	AffectedCallsCount int     `json:"affected_calls_count"`
	AffectedShare      float64 `json:"affected_share"`
	Severity           string  `json:"severity"`
	Recommendation     string  `json:"recommendation"`
	BusinessImpact     string  `json:"business_impact"`
	Reason             string  `json:"reason"`
	Count              int     `json:"count"`
}

type aggregateMetricDetail struct {
	Code               string  `json:"code"`
	Title              string  `json:"title"`
	AffectedCallsCount int     `json:"affected_calls_count"`
	AffectedShare      float64 `json:"affected_share"`
	Explanation        string  `json:"explanation"`
	Recommendation     string  `json:"recommendation"`
}

type aggregatePriorityAction struct {
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	ExpectedEffect string `json:"expected_effect"`
}

type aggregateDetailedReport struct {
	Methodology          string `json:"methodology"`
	QualityOverview      string `json:"quality_overview"`
	IssueAnalysis        string `json:"issue_analysis"`
	CustomerLossAnalysis string `json:"customer_loss_analysis"`
	TrainingPlan         string `json:"training_plan"`
	DataLimitations      string `json:"data_limitations"`
}

// aggregateReportStatistics mirrors the deterministic dataset saved with a deep
// analysis. Keeping it in the export means that every report format exposes
// statistics calculated from the complete source set, not only the AI's
// representative-call narrative.
type aggregateReportStatistics struct {
	ScoreSummary       models.AggregateAnalysisScoreSummary      `json:"score_summary"`
	IssueCoverage      []models.AggregateAnalysisFrequency       `json:"issue_coverage"`
	WeakCriteria       []models.AggregateAnalysisCriterionMetric `json:"weak_criteria"`
	BusinessOutcomes   []models.AggregateAnalysisFrequency       `json:"business_outcomes"`
	LostReasons        []models.AggregateAnalysisFrequency       `json:"lost_reasons"`
	CustomerObjections []models.AggregateAnalysisFrequency       `json:"customer_objections"`
	Risks              []models.AggregateAnalysisFrequency       `json:"risks"`
	Topics             []models.AggregateAnalysisFrequency       `json:"topics"`
	NextStepSummary    models.AggregateAnalysisNextStepSummary   `json:"next_step_summary"`
	AttentionCalls     []models.AggregateAnalysisCallEvidence    `json:"attention_calls"`
	StrongCalls        []models.AggregateAnalysisCallEvidence    `json:"strong_calls"`
}

func generateAggregateReport(format models.ReportFormat, data AggregateReportData) ([]byte, error) {
	switch format {
	case models.ReportFormatMD:
		return generateAggregateMarkdownReport(data), nil
	case models.ReportFormatXLSX:
		return generateAggregateXLSXReport(data)
	case models.ReportFormatPDF:
		return generateAggregatePDFReport(data)
	case models.ReportFormatDOCX:
		return generateAggregateDOCXReport(data)
	default:
		return nil, models.ErrUnsupportedReportFormat
	}
}

func generateAggregateMarkdownReport(data AggregateReportData) []byte {
	result := parseAggregateResult(data.Analysis)
	var b bytes.Buffer
	writeAggregateMarkdown(&b, data, result, true)
	return b.Bytes()
}

func writeAggregateMarkdown(b *bytes.Buffer, data AggregateReportData, result aggregateResult, includeRaw bool) {
	a := data.Analysis
	fmt.Fprintln(b, "# Глубокий анализ звонков")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "Период: %s - %s  \n", reportDate(a.PeriodFrom), reportDate(a.PeriodTo))
	fmt.Fprintf(b, "Охват: %s  \n", callCountLabel(a.SourceCallsCount))
	fmt.Fprintf(b, "Сформировано: %s\n", reportDateTime(data.GeneratedAt))
	if a.Model != nil {
		fmt.Fprintf(b, "Модель анализа: %s\n", *a.Model)
	}
	fmt.Fprintln(b)
	writeMarkdownSection(b, "Покрытие источников", aggregateCoverageLines(a, result))
	writeMarkdownSection(b, "Резюме для руководителя", []string{executiveSummary(result, a)})
	writeDetailedReport(b, result.DetailedReport)
	writeIssueDetails(b, "Системные проблемы", result.SystemicIssues)
	writeFindings(b, "Ключевые выводы", result.KeyFindings)
	writeRecurringIssues(b, result.RecurringIssues)
	writeIssueDetails(b, "Единичные, но важные сигналы", result.SingleCallObservations)
	writeMetricDetails(b, "Слабые критерии", result.WeakCriteria)
	writeMetricDetails(b, "Возражения клиентов", result.ClientObjections)
	writeIssueDetails(b, "Паттерны потерь и рисков", result.LossAndRiskPatterns)
	writeMarkdownSection(b, "Сильные стороны", result.Strengths)
	writeMarkdownSection(b, "Риски", result.Risks)
	writePriorityActions(b, result.PriorityActions)
	writeMarkdownSection(b, "Рекомендации менеджерам", result.ManagerRecommendations)
	writeMarkdownSection(b, "Оценки качества", aggregateScoreLines(result.AggregateStatistics.ScoreSummary))
	writeMarkdownSection(b, "Покрытие проблем", aggregateFrequencyLines(result.AggregateStatistics.IssueCoverage))
	writeMarkdownSection(b, "Слабые критерии: фактические показатели", aggregateWeakCriteriaLines(result.AggregateStatistics.WeakCriteria))
	writeMarkdownSection(b, "Бизнес-результаты", aggregateFrequencyLines(result.AggregateStatistics.BusinessOutcomes))
	writeMarkdownSection(b, "Причины потерь", aggregateFrequencyLines(result.AggregateStatistics.LostReasons))
	writeMarkdownSection(b, "Возражения клиентов: статистика", aggregateFrequencyLines(result.AggregateStatistics.CustomerObjections))
	writeMarkdownSection(b, "Риски: статистика", aggregateFrequencyLines(result.AggregateStatistics.Risks))
	writeMarkdownSection(b, "Темы разговоров", aggregateFrequencyLines(result.AggregateStatistics.Topics))
	writeMarkdownSection(b, "Следующие шаги", aggregateNextStepLines(result.AggregateStatistics.NextStepSummary))
	writeMarkdownSection(b, "Звонки, требующие внимания", aggregateCallEvidenceLines(result.AggregateStatistics.AttentionCalls))
	writeMarkdownSection(b, "Сильные звонки", aggregateCallEvidenceLines(result.AggregateStatistics.StrongCalls))
	if result.Confidence != nil {
		writeMarkdownSection(b, "Уверенность в выводах", []string{localizedEnum(fmt.Sprint(result.Confidence))})
	}
	_ = includeRaw // Технический JSON намеренно не экспортируется: в нем есть внутренние идентификаторы звонков.
}

func writeMarkdownSection(b *bytes.Buffer, title string, items []string) {
	prepared := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			prepared = append(prepared, strings.TrimSpace(item))
		}
	}
	if len(prepared) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, item := range prepared {
		fmt.Fprintf(b, "- %s\n", item)
	}
	fmt.Fprintln(b)
}

func executiveSummary(result aggregateResult, analysis models.AggregateAnalysis) string {
	return fallback(result.ExecutiveSummary, fallback(result.OverallAssessment, fallback(result.Summary, resultText(analysis))))
}

func writeDetailedReport(b *bytes.Buffer, report aggregateDetailedReport) {
	sections := []struct{ title, text string }{
		{"Методика", report.Methodology},
		{"Обзор качества", report.QualityOverview},
		{"Анализ проблем", report.IssueAnalysis},
		{"Потери клиентов", report.CustomerLossAnalysis},
		{"План обучения", report.TrainingPlan},
		{"Ограничения данных", report.DataLimitations},
	}
	for _, section := range sections {
		if strings.TrimSpace(section.text) != "" {
			writeMarkdownSection(b, section.title, []string{section.text})
		}
	}
}

func writeFindings(b *bytes.Buffer, title string, items []aggregateFinding) {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := localizedLabel(item.Title, "")
		if item.Description != "" {
			line += ": " + cleanReportClause(item.Description)
		}
		if item.AffectedCallsCount > 0 {
			line += fmt.Sprintf(". Затронуто: %s", callCountLabel(item.AffectedCallsCount))
			if item.AffectedShare > 0 {
				line += " (" + formatShare(item.AffectedShare) + ")"
			}
		}
		if item.Severity != "" {
			line += ". Приоритет: " + localizedEnum(item.Severity)
		}
		lines = append(lines, line)
	}
	writeMarkdownSection(b, title, lines)
}

func writeRecurringIssues(b *bytes.Buffer, items []aggregateRecurringIssue) {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := localizedLabel(item.Title, item.Code)
		if item.Count > 0 {
			line += fmt.Sprintf(". Затронуто: %s", callCountLabel(item.Count))
			if item.AffectedShare > 0 {
				line += " (" + formatShare(item.AffectedShare) + ")"
			}
		}
		if item.Recommendation != "" {
			line += ". Рекомендация: " + cleanReportClause(item.Recommendation)
		}
		lines = append(lines, line)
	}
	writeMarkdownSection(b, "Повторяющиеся проблемы", lines)
}

func writeIssueDetails(b *bytes.Buffer, title string, items []aggregateIssueDetail) {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := localizedLabel(item.Title, item.Code)
		if text := fallback(item.Description, item.Reason); text != "" {
			line += ": " + cleanReportClause(text)
		}
		count := item.AffectedCallsCount
		if count == 0 {
			count = item.Count
		}
		if count > 0 {
			line += fmt.Sprintf(". Затронуто: %s", callCountLabel(count))
			if item.AffectedShare > 0 {
				line += " (" + formatShare(item.AffectedShare) + ")"
			}
		}
		if item.BusinessImpact != "" {
			line += ". Влияние: " + cleanReportClause(item.BusinessImpact)
		}
		if item.Recommendation != "" {
			line += ". Рекомендация: " + cleanReportClause(item.Recommendation)
		}
		if item.Severity != "" {
			line += ". Приоритет: " + localizedEnum(item.Severity)
		}
		lines = append(lines, line)
	}
	writeMarkdownSection(b, title, lines)
}

func writeMetricDetails(b *bytes.Buffer, title string, items []aggregateMetricDetail) {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := localizedLabel(item.Title, item.Code)
		if item.Explanation != "" {
			line += ": " + cleanReportClause(item.Explanation)
		}
		if item.AffectedCallsCount > 0 {
			line += fmt.Sprintf(". Затронуто: %s", callCountLabel(item.AffectedCallsCount))
			if item.AffectedShare > 0 {
				line += " (" + formatShare(item.AffectedShare) + ")"
			}
		}
		if item.Recommendation != "" {
			line += ". Рекомендация: " + cleanReportClause(item.Recommendation)
		}
		lines = append(lines, line)
	}
	writeMarkdownSection(b, title, lines)
}

func writePriorityActions(b *bytes.Buffer, items []aggregatePriorityAction) {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := item.Title
		if item.ExpectedEffect != "" {
			line += ". Ожидаемый эффект: " + cleanReportClause(item.ExpectedEffect)
		}
		if item.Priority != "" {
			line += ". Приоритет: " + localizedEnum(item.Priority)
		}
		lines = append(lines, line)
	}
	writeMarkdownSection(b, "Приоритетные действия", lines)
}

func cleanReportClause(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), ".; ")
}

func generateAggregateXLSXReport(data AggregateReportData) ([]byte, error) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	result := parseAggregateResult(data.Analysis)
	_ = file.SetSheetName("Sheet1", "Сводка")
	setSheetRows(file, "Сводка", [][]any{
		{"Глубокий анализ звонков", ""},
		{"Период", reportDate(data.Analysis.PeriodFrom) + " - " + reportDate(data.Analysis.PeriodTo)},
		{"Охват", data.Analysis.SourceCallsCount},
		{"Сформировано", reportDateTime(data.GeneratedAt)},
		{"Резюме для руководителя", executiveSummary(result, data.Analysis)},
		{"Уверенность", localizedEnum(fmt.Sprint(result.Confidence))},
		{"Учтено в статистике", result.SourceSummary.IncludedInStatistics},
		{"Примеров для ИИ", result.SourceSummary.RepresentativeCalls},
		{"Средняя оценка", optionalFloatValue(result.AggregateStatistics.ScoreSummary.Average)},
		{"Низкие оценки", result.AggregateStatistics.ScoreSummary.LowCount},
		{"Средние оценки", result.AggregateStatistics.ScoreSummary.MediumCount},
		{"Высокие оценки", result.AggregateStatistics.ScoreSummary.HighCount},
	})
	for _, sheet := range []struct {
		name  string
		items []string
	}{
		{"Сильные стороны", result.Strengths},
		{"Риски", result.Risks},
		{"Рекомендации", result.ManagerRecommendations},
	} {
		if _, err := file.NewSheet(sheet.name); err != nil {
			return nil, err
		}
		rows := [][]any{{"Item"}}
		for _, item := range sheet.items {
			rows = append(rows, []any{item})
		}
		setSheetRows(file, sheet.name, rows)
	}
	for _, sheet := range []struct {
		name string
		rows [][]any
	}{
		{"Подробный отчет", aggregateDetailedReportRows(result.DetailedReport)},
		{"Системные проблемы", aggregateIssueDetailRows(result.SystemicIssues)},
		{"Ключевые выводы", aggregateFindingRows(result.KeyFindings)},
		{"Повторяющиеся проблемы", aggregateRecurringIssueRows(result.RecurringIssues)},
		{"Единичные сигналы", aggregateIssueDetailRows(result.SingleCallObservations)},
		{"Слабые критерии ИИ", aggregateMetricDetailRows(result.WeakCriteria)},
		{"Возражения ИИ", aggregateMetricDetailRows(result.ClientObjections)},
		{"Потери и риски", aggregateIssueDetailRows(result.LossAndRiskPatterns)},
		{"Приоритетные действия", aggregatePriorityActionRows(result.PriorityActions)},
		{"Покрытие проблем", aggregateFrequencyRows(result.AggregateStatistics.IssueCoverage)},
		{"Слабые критерии", aggregateWeakCriteriaRows(result.AggregateStatistics.WeakCriteria)},
		{"Бизнес-результаты", aggregateFrequencyRows(result.AggregateStatistics.BusinessOutcomes)},
		{"Причины потерь", aggregateFrequencyRows(result.AggregateStatistics.LostReasons)},
		{"Возражения", aggregateFrequencyRows(result.AggregateStatistics.CustomerObjections)},
		{"Риски статистика", aggregateFrequencyRows(result.AggregateStatistics.Risks)},
		{"Темы разговоров", aggregateFrequencyRows(result.AggregateStatistics.Topics)},
		{"Следующие шаги", aggregateNextStepRows(result.AggregateStatistics.NextStepSummary)},
		{"Требуют внимания", aggregateCallEvidenceRows(result.AggregateStatistics.AttentionCalls)},
		{"Сильные звонки", aggregateCallEvidenceRows(result.AggregateStatistics.StrongCalls)},
	} {
		if err := addAggregateSheet(file, sheet.name, sheet.rows); err != nil {
			return nil, err
		}
	}
	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func generateAggregateDOCXReport(data AggregateReportData) ([]byte, error) {
	result := parseAggregateResult(data.Analysis)
	var text bytes.Buffer
	writeAggregateMarkdown(&text, data, result, false)
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	doc.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range strings.Split(text.String(), "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			docxStyledParagraph(&doc, strings.TrimPrefix(line, "# "), "title")
		case strings.HasPrefix(line, "## "):
			docxStyledParagraph(&doc, strings.TrimPrefix(line, "## "), "heading")
		case strings.HasPrefix(line, "- "):
			docxStyledParagraph(&doc, "• "+strings.TrimPrefix(line, "- "), "bullet")
		case strings.TrimSpace(line) != "":
			docxStyledParagraph(&doc, line, "body")
		default:
			docxStyledParagraph(&doc, "", "space")
		}
	}
	doc.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1080" w:right="1080" w:bottom="1080" w:left="1080"/></w:sectPr>`)
	doc.WriteString(`</w:body></w:document>`)
	return zipDocx(doc.String())
}

func generateAggregatePDFReport(data AggregateReportData) ([]byte, error) {
	result := parseAggregateResult(data.Analysis)
	pdf := gofpdf.New("P", "mm", "A4", "")
	fontPath, err := reportFontPath()
	if err != nil {
		return nil, err
	}
	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, err
	}
	pdf.AddUTF8FontFromBytes("report", "", fontBytes)
	pdf.SetMargins(16, 16, 16)
	pdf.SetAutoPageBreak(true, 16)
	pdf.AliasNbPages("")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("report", "", 8)
		pdf.SetTextColor(111, 111, 111)
		pdf.CellFormat(0, 5, fmt.Sprintf("Глубокий анализ звонков · %d/{nb}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()
	var text bytes.Buffer
	writeAggregateMarkdown(&text, data, result, false)
	for _, line := range strings.Split(text.String(), "\n") {
		writePDFReportLine(pdf, line)
	}
	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func parseAggregateResult(analysis models.AggregateAnalysis) aggregateResult {
	var result aggregateResult
	if len(analysis.ResultJSON) > 0 && json.Unmarshal(analysis.ResultJSON, &result) == nil {
		return result
	}
	result.Summary = resultText(analysis)
	return result
}

func resultText(analysis models.AggregateAnalysis) string {
	if analysis.ResultText != nil && strings.TrimSpace(*analysis.ResultText) != "" {
		return strings.TrimSpace(*analysis.ResultText)
	}
	return "Структурированный результат анализа не сформирован."
}

func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func aggregateCoverageLines(analysis models.AggregateAnalysis, result aggregateResult) []string {
	summary := result.SourceSummary
	if summary.AnalyzedCalls == 0 && summary.IncludedInStatistics == 0 && summary.SourceSetHash == "" && result.CoverageNote == "" && analysis.SourceCallsCount == 0 {
		return nil
	}
	analyzedCalls := summary.AnalyzedCalls
	if analyzedCalls == 0 {
		analyzedCalls = analysis.SourceCallsCount
	}
	includedCalls := summary.IncludedInStatistics
	if includedCalls == 0 {
		includedCalls = analysis.SourceCallsCount
	}
	lines := []string{
		"Учтено в статистике: " + callCountLabel(includedCalls),
		"Готовых анализов в наборе: " + callCountLabel(analyzedCalls),
		"Примеров для ИИ: " + callCountLabel(summary.RepresentativeCalls),
	}
	if summary.AllAnalyzedCallsUsed {
		lines = append(lines, "Статистика построена по всем готовым анализам за выбранный период.")
	}
	if result.CoverageNote != "" {
		lines = append(lines, result.CoverageNote)
	}
	return lines
}

func aggregateScoreLines(summary models.AggregateAnalysisScoreSummary) []string {
	if summary.CallsWithScore == 0 {
		return nil
	}
	return []string{
		"С оценкой: " + callCountLabel(summary.CallsWithScore),
		"Средняя оценка: " + formatOptionalFloat(summary.Average),
		"Минимальная оценка: " + formatOptionalFloat(summary.Min),
		"Максимальная оценка: " + formatOptionalFloat(summary.Max),
		fmt.Sprintf("Распределение: низкие — %s, средние — %s, высокие — %s", callCountLabel(summary.LowCount), callCountLabel(summary.MediumCount), callCountLabel(summary.HighCount)),
	}
}

func aggregateFrequencyLines(items []models.AggregateAnalysisFrequency) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%s — %s (%s)", localizedLabel(item.Title, item.Code), callCountLabel(item.Count), formatShare(item.Share)))
	}
	return lines
}

func aggregateWeakCriteriaLines(items []models.AggregateAnalysisCriterionMetric) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := fmt.Sprintf("%s — слабый в %s из %s применимых звонков (%s); пропущено: %d, частично: %d, неясно: %d", localizedLabel(item.Title, item.Code), callCountLabel(item.WeakCalls), callCountLabel(item.ApplicableCalls), formatShare(item.WeakShare), item.MissedCalls, item.PartiallyMetCalls, item.UnclearCalls)
		if item.AveragePointsShare != nil {
			line += "; средний результат: " + formatShare(*item.AveragePointsShare)
		}
		lines = append(lines, line)
	}
	return lines
}

func aggregateNextStepLines(summary models.AggregateAnalysisNextStepSummary) []string {
	if summary.CallsWithNextStep == 0 && summary.CallsMissingNextStep == 0 && summary.CallsWithSpecificNextStep == 0 && summary.CallsMissingSpecificStep == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("Есть следующий шаг: %s; нет следующего шага: %s (%s)", callCountLabel(summary.CallsWithNextStep), callCountLabel(summary.CallsMissingNextStep), formatShare(summary.MissingNextStepShare)),
		fmt.Sprintf("Есть конкретный шаг: %s; нет конкретики: %s (%s)", callCountLabel(summary.CallsWithSpecificNextStep), callCountLabel(summary.CallsMissingSpecificStep), formatShare(summary.MissingSpecificStepShare)),
	}
}

func aggregateCallEvidenceLines(items []models.AggregateAnalysisCallEvidence) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := callEvidenceLabel(item)
		if item.Score != nil {
			line += fmt.Sprintf(". Оценка: %.1f", *item.Score)
		}
		if item.Summary != "" {
			line += "; " + item.Summary
		}
		if len(item.IssueCodes) > 0 {
			line += "; сигналы: " + strings.Join(localizedCodes(item.IssueCodes), ", ")
		}
		lines = append(lines, line)
	}
	return lines
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return "Не указано"
	}
	return fmt.Sprintf("%.2f", *value)
}

func optionalFloatValue(value *float64) any {
	if value == nil {
		return ""
	}
	return *value
}

func reportDate(value time.Time) string {
	if value.IsZero() {
		return "Дата не указана"
	}
	return value.UTC().Format("02.01.2006")
}

func reportDateTime(value time.Time) string {
	if value.IsZero() {
		return "Не указано"
	}
	return value.UTC().Format("02.01.2006 15:04 UTC")
}

func callCountLabel(count int) string {
	if count%100 >= 11 && count%100 <= 14 {
		return fmt.Sprintf("%d звонков", count)
	}
	switch count % 10 {
	case 1:
		return fmt.Sprintf("%d звонок", count)
	case 2, 3, 4:
		return fmt.Sprintf("%d звонка", count)
	default:
		return fmt.Sprintf("%d звонков", count)
	}
}

func formatShare(value float64) string {
	return fmt.Sprintf("%.1f %%", value*100)
}

func callEvidenceLabel(item models.AggregateAnalysisCallEvidence) string {
	date := reportDate(item.CreatedAt)
	title := strings.TrimSpace(item.Title)
	if title == "" || strings.EqualFold(title, "Звонок") {
		return "Звонок от " + date
	}
	return fmt.Sprintf("%s от %s", title, date)
}

func localizedCodes(codes []string) []string {
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		result = append(result, localizedLabel("", code))
	}
	return result
}

func localizedEnum(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return "Низкий"
	case "medium":
		return "Средний"
	case "high":
		return "Высокий"
	default:
		return localizedLabel(value, value)
	}
}

func localizedLabel(title, code string) string {
	key := strings.ToLower(strings.TrimSpace(title))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(code))
	}
	labels := map[string]string{
		"high price": "Высокая цена", "price": "Цена", "pricing clarity": "Ясность цены",
		"price not clear": "Неясная цена", "no next step": "Нет следующего шага",
		"weak next step": "Слабый следующий шаг", "weak question": "Слабые вопросы",
		"weak answer": "Слабый ответ менеджера", "solution not offered": "Решение не предложено",
		"tone inappropriate": "Непрофессиональный тон", "not a call": "Не целевой звонок",
		"outcome not clear": "Неясный результат", "low confidence": "Низкая уверенность",
		"needs discovery": "Выявление потребностей", "question quality": "Качество вопросов",
		"answer quality": "Качество ответов", "solution relevance": "Актуальность решения",
		"pricing_clarity": "Ясность цены", "tone_professionalism": "Профессионализм тона",
		"next_step_quality": "Качество следующего шага", "outcome_clarity": "Ясность результата",
		"needs_discovery": "Выявление потребностей", "question_quality": "Качество вопросов",
		"answer_quality": "Качество ответов", "solution_relevance": "Актуальность решения",
		"no_next_step": "Нет следующего шага", "weak_next_step": "Слабый следующий шаг",
		"weak_question": "Слабые вопросы", "weak_answer": "Слабый ответ менеджера",
		"solution_not_offered": "Решение не предложено", "tone_inappropriate": "Непрофессиональный тон",
		"outcome_not_clear": "Неясный результат", "low_confidence": "Низкая уверенность",
		"not_a_call": "Не целевой звонок", "high_price": "Высокая цена",
	}
	if localized, ok := labels[key]; ok {
		return localized
	}
	if strings.TrimSpace(title) != "" {
		return title
	}
	if strings.TrimSpace(code) != "" {
		return strings.ReplaceAll(code, "_", " ")
	}
	return "Не указано"
}

func aggregateFrequencyRows(items []models.AggregateAnalysisFrequency) [][]any {
	rows := [][]any{{"Показатель", "Затронуто звонков", "Доля"}}
	for _, item := range items {
		rows = append(rows, []any{localizedLabel(item.Title, item.Code), item.Count, item.Share})
	}
	return rows
}

func aggregateDetailedReportRows(report aggregateDetailedReport) [][]any {
	return [][]any{
		{"Раздел", "Содержание"},
		{"Методика", report.Methodology},
		{"Обзор качества", report.QualityOverview},
		{"Анализ проблем", report.IssueAnalysis},
		{"Потери клиентов", report.CustomerLossAnalysis},
		{"План обучения", report.TrainingPlan},
		{"Ограничения данных", report.DataLimitations},
	}
}

func aggregateFindingRows(items []aggregateFinding) [][]any {
	rows := [][]any{{"Вывод", "Описание", "Затронуто", "Доля", "Приоритет"}}
	for _, item := range items {
		rows = append(rows, []any{localizedLabel(item.Title, ""), item.Description, item.AffectedCallsCount, item.AffectedShare, localizedEnum(item.Severity)})
	}
	return rows
}

func aggregateRecurringIssueRows(items []aggregateRecurringIssue) [][]any {
	rows := [][]any{{"Проблема", "Затронуто", "Доля", "Рекомендация"}}
	for _, item := range items {
		rows = append(rows, []any{localizedLabel(item.Title, item.Code), item.Count, item.AffectedShare, item.Recommendation})
	}
	return rows
}

func aggregateIssueDetailRows(items []aggregateIssueDetail) [][]any {
	rows := [][]any{{"Проблема", "Описание", "Затронуто", "Доля", "Влияние", "Рекомендация", "Приоритет"}}
	for _, item := range items {
		count := item.AffectedCallsCount
		if count == 0 {
			count = item.Count
		}
		rows = append(rows, []any{localizedLabel(item.Title, item.Code), fallback(item.Description, item.Reason), count, item.AffectedShare, item.BusinessImpact, item.Recommendation, localizedEnum(item.Severity)})
	}
	return rows
}

func aggregateMetricDetailRows(items []aggregateMetricDetail) [][]any {
	rows := [][]any{{"Показатель", "Пояснение", "Затронуто", "Доля", "Рекомендация"}}
	for _, item := range items {
		rows = append(rows, []any{localizedLabel(item.Title, item.Code), item.Explanation, item.AffectedCallsCount, item.AffectedShare, item.Recommendation})
	}
	return rows
}

func aggregatePriorityActionRows(items []aggregatePriorityAction) [][]any {
	rows := [][]any{{"Действие", "Ожидаемый эффект", "Приоритет"}}
	for _, item := range items {
		rows = append(rows, []any{item.Title, item.ExpectedEffect, localizedEnum(item.Priority)})
	}
	return rows
}

func aggregateWeakCriteriaRows(items []models.AggregateAnalysisCriterionMetric) [][]any {
	rows := [][]any{{"Критерий", "Применимо", "Слабых", "Доля слабых", "Средний результат", "Пропущено", "Частично", "Неясно"}}
	for _, item := range items {
		rows = append(rows, []any{localizedLabel(item.Title, item.Code), item.ApplicableCalls, item.WeakCalls, item.WeakShare, optionalFloatValue(item.AveragePointsShare), item.MissedCalls, item.PartiallyMetCalls, item.UnclearCalls})
	}
	return rows
}

func aggregateNextStepRows(summary models.AggregateAnalysisNextStepSummary) [][]any {
	return [][]any{
		{"Показатель", "Значение"},
		{"Есть следующий шаг", summary.CallsWithNextStep},
		{"Есть конкретный шаг", summary.CallsWithSpecificNextStep},
		{"Нет следующего шага", summary.CallsMissingNextStep},
		{"Нет конкретики", summary.CallsMissingSpecificStep},
		{"Доля без следующего шага", summary.MissingNextStepShare},
		{"Доля без конкретики", summary.MissingSpecificStepShare},
	}
}

func aggregateCallEvidenceRows(items []models.AggregateAnalysisCallEvidence) [][]any {
	rows := [][]any{{"Дата звонка", "Название", "Оценка", "Краткое описание", "Сигналы"}}
	for _, item := range items {
		rows = append(rows, []any{reportDate(item.CreatedAt), fallback(item.Title, "Звонок"), optionalFloatValue(item.Score), item.Summary, strings.Join(localizedCodes(item.IssueCodes), ", ")})
	}
	return rows
}

func addAggregateSheet(file *excelize.File, name string, rows [][]any) error {
	if _, err := file.NewSheet(name); err != nil {
		return err
	}
	setSheetRows(file, name, rows)
	return file.SetColWidth(name, "C", "J", 18)
}

func setSheetRows(file *excelize.File, sheet string, rows [][]any) {
	for rowIndex, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, rowIndex+1)
		_ = file.SetSheetRow(sheet, cell, &row)
	}
	lastColumn, _ := excelize.ColumnNumberToName(maxAggregateRowWidth(rows))
	lastRow := len(rows)
	titleStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 15},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"2B463C"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	headerStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "24352F"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"E7EFEA"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    []excelize.Border{{Type: "Bottom", Color: "C9D8D0", Style: 1}},
	})
	bodyStyle, _ := file.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    []excelize.Border{{Type: "Bottom", Color: "E5E9E6", Style: 1}},
	})
	percentStyle, _ := file.NewStyle(&excelize.Style{NumFmt: 10, Alignment: &excelize.Alignment{Vertical: "top"}})
	_ = file.SetColWidth(sheet, "A", "A", 30)
	_ = file.SetColWidth(sheet, "B", "B", 72)
	_ = file.SetRowHeight(sheet, 1, 28)
	_ = file.SetCellStyle(sheet, "A1", fmt.Sprintf("%s%d", lastColumn, lastRow), bodyStyle)
	if sheet == "Сводка" {
		_ = file.MergeCell(sheet, "A1", "B1")
		_ = file.SetCellStyle(sheet, "A1", "B1", titleStyle)
		_ = file.SetRowHeight(sheet, 5, 72)
		return
	}
	_ = file.SetCellStyle(sheet, "A1", lastColumn+"1", headerStyle)
	for rowIndex, row := range rows[1:] {
		for columnIndex, value := range row {
			header := ""
			if columnIndex < len(rows[0]) {
				header, _ = rows[0][columnIndex].(string)
			}
			if _, ok := value.(float64); !ok || !strings.Contains(strings.ToLower(header), "доля") {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			_ = file.SetCellStyle(sheet, cell, cell, percentStyle)
		}
	}
}

func maxAggregateRowWidth(rows [][]any) int {
	width := 1
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	return width
}

func aggregateReportFileName(analysis models.AggregateAnalysis, id uuid.UUID, format models.ReportFormat) string {
	return fmt.Sprintf("deep-analysis-%s-%s-%s-%s%s",
		analysis.Scope,
		analysis.PeriodFrom.Format("2006-01-02"),
		analysis.PeriodTo.Format("2006-01-02"),
		id.String(),
		reportFileExtension(format),
	)
}

func normalizeReportFormat(format models.ReportFormat) (models.ReportFormat, error) {
	switch format {
	case models.ReportFormatPDF, models.ReportFormatDOCX, models.ReportFormatMD, models.ReportFormatXLSX:
		return format, nil
	default:
		return "", models.ErrUnsupportedReportFormat
	}
}

func reportContentType(format models.ReportFormat) string {
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

func reportFileExtension(format models.ReportFormat) string {
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

func reportFontPath() (string, error) {
	for _, candidate := range []string{`C:\Windows\Fonts\arial.ttf`, `C:\Windows\Fonts\segoeui.ttf`, "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("report pdf font not found")
}

func zipDocx(document string) ([]byte, error) {
	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   document,
	}
	for name, content := range files {
		writer, err := zipWriter.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func docxStyledParagraph(b *strings.Builder, text, kind string) {
	properties := `<w:spacing w:after="120" w:line="276" w:lineRule="auto"/>`
	runProperties := `<w:rFonts w:ascii="Aptos" w:hAnsi="Aptos"/><w:sz w:val="22"/><w:color w:val="403D39"/>`
	switch kind {
	case "title":
		properties = `<w:spacing w:before="0" w:after="220"/><w:keepNext/>`
		runProperties = `<w:rFonts w:ascii="Aptos Display" w:hAnsi="Aptos Display"/><w:b/><w:sz w:val="38"/><w:color w:val="2B463C"/>`
	case "heading":
		properties = `<w:spacing w:before="260" w:after="100"/><w:keepNext/><w:outlineLvl w:val="1"/>`
		runProperties = `<w:rFonts w:ascii="Aptos Display" w:hAnsi="Aptos Display"/><w:b/><w:sz w:val="27"/><w:color w:val="2B463C"/>`
	case "bullet":
		properties = `<w:ind w:left="360" w:hanging="180"/><w:spacing w:after="90" w:line="276" w:lineRule="auto"/>`
	case "space":
		properties = `<w:spacing w:after="80"/>`
		text = ""
	}
	b.WriteString(`<w:p><w:pPr>` + properties + `</w:pPr><w:r><w:rPr>` + runProperties + `</w:rPr><w:t xml:space="preserve">`)
	_ = xml.EscapeText((*stringWriter)(b), []byte(text))
	b.WriteString(`</w:t></w:r></w:p>`)
}

func writePDFReportLine(pdf *gofpdf.Fpdf, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		pdf.Ln(2)
		return
	}
	if strings.HasPrefix(line, "# ") {
		pdf.SetFillColor(43, 70, 60)
		pdf.Rect(16, pdf.GetY(), 178, 24, "F")
		pdf.SetXY(22, pdf.GetY()+6)
		pdf.SetFont("report", "", 20)
		pdf.SetTextColor(255, 255, 255)
		pdf.CellFormat(166, 10, strings.TrimPrefix(line, "# "), "", 1, "L", false, 0, "")
		pdf.Ln(8)
		return
	}
	if strings.HasPrefix(line, "## ") {
		if pdf.GetY() > 235 {
			pdf.AddPage()
		}
		pdf.SetFont("report", "", 14)
		pdf.SetTextColor(43, 70, 60)
		pdf.MultiCell(0, 7, strings.TrimPrefix(line, "## "), "B", "L", false)
		pdf.Ln(1)
		return
	}
	pdf.SetFont("report", "", 10)
	pdf.SetTextColor(71, 67, 63)
	if strings.HasPrefix(line, "- ") {
		pdf.SetTextColor(215, 90, 61)
		pdf.CellFormat(5, 5, "•", "", 0, "L", false, 0, "")
		pdf.SetTextColor(71, 67, 63)
		pdf.MultiCell(0, 5, strings.TrimPrefix(line, "- "), "", "L", false)
		return
	}
	pdf.MultiCell(0, 5, line, "", "L", false)
}

type stringWriter strings.Builder

func (w *stringWriter) Write(p []byte) (int, error) {
	(*strings.Builder)(w).Write(p)
	return len(p), nil
}
