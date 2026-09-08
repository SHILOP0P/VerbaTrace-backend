package report

import (
	"encoding/json"
	"fmt"
	"strings"
)

type analysisReport struct {
	Summary            string           `json:"summary"`
	Topics             []string         `json:"topics"`
	DialogueTone       dialogueTone     `json:"dialogue_tone"`
	ClientQuestions    []clientQuestion `json:"client_questions"`
	QuestionCoverage   questionCoverage `json:"question_coverage"`
	ManagerQuality     managerQuality   `json:"manager_quality"`
	CallOutcome        string           `json:"call_outcome"`
	Score              float64          `json:"score"`
	CriteriaResults    []criteriaResult `json:"criteria_results"`
	CustomerObjections []string         `json:"customer_objections"`
	Risks              []string         `json:"risks"`
	NextSteps          []string         `json:"next_steps"`
	NextStep           string           `json:"next_step"`
	EvidenceQuotes     []string         `json:"evidence_quotes"`
	Confidence         string           `json:"confidence"`
	RawFallback        string           `json:"-"`
}

type dialogueTone struct {
	Overall        string   `json:"overall"`
	Manager        string   `json:"manager"`
	Client         string   `json:"client"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

type clientQuestion struct {
	Question       string   `json:"question"`
	ManagerAnswer  string   `json:"manager_answer"`
	AnswerStatus   string   `json:"answer_status"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

type questionCoverage struct {
	Status              string   `json:"status"`
	Summary             string   `json:"summary"`
	UnansweredQuestions []string `json:"unanswered_questions"`
}

type managerQuality struct {
	Strengths       []string `json:"strengths"`
	Issues          []string `json:"issues"`
	Recommendations []string `json:"recommendations"`
}

type criteriaResult struct {
	InstructionTitle string   `json:"instruction_title"`
	Result           string   `json:"result"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`
	EvidenceQuotes   []string `json:"evidence_quotes"`
}

type reportSection struct {
	Title string
	Rows  []reportRow
}

type reportRow struct {
	Label string
	Value string
	List  []string
}

func (d ReportData) StructuredAnalysis() analysisReport {
	var analysis analysisReport
	if len(d.Analysis.ResultJSON) > 0 {
		if err := json.Unmarshal(d.Analysis.ResultJSON, &analysis); err == nil {
			analysis.normalize(d.AnalysisText())
			return analysis
		}
	}

	analysis.RawFallback = d.AnalysisText()
	analysis.normalize(d.AnalysisText())
	return analysis
}

func (a *analysisReport) normalize(fallback string) {
	a.Summary = strings.TrimSpace(a.Summary)
	if a.Summary == "" {
		a.Summary = strings.TrimSpace(fallback)
	}
	if a.Summary == "" {
		a.Summary = "Не указано"
	}
	if a.NextStep == "" && len(a.NextSteps) > 0 {
		a.NextStep = a.NextSteps[0]
	}
}

func (d ReportData) Sections() []reportSection {
	if d.TranscriptionOnly {
		return []reportSection{{Title: fmt.Sprintf("Транскрипция · версия %d", d.TranscriptionRevision), Rows: []reportRow{{Value: d.TranscriptionText}}}}
	}
	analysis := d.StructuredAnalysis()
	sections := []reportSection{
		{
			Title: "Резюме",
			Rows:  []reportRow{{Value: analysis.Summary}},
		},
		{
			Title: "Ключевые темы",
			Rows:  []reportRow{{List: withFallbackList(analysis.Topics)}},
		},
		{
			Title: "Тон диалога",
			Rows: []reportRow{
				{Label: "Общий тон", Value: withFallback(analysis.DialogueTone.Overall)},
				{Label: "Менеджер", Value: withFallback(analysis.DialogueTone.Manager)},
				{Label: "Клиент", Value: withFallback(analysis.DialogueTone.Client)},
				{Label: "Цитаты", List: withFallbackList(analysis.DialogueTone.EvidenceQuotes)},
			},
		},
	}

	sections = append(sections, clientQuestionsSection(analysis.ClientQuestions))
	sections = append(sections, reportSection{
		Title: "Полнота ответов менеджера",
		Rows: []reportRow{
			{Label: "Статус", Value: answerStatusLabel(analysis.QuestionCoverage.Status)},
			{Label: "Итог", Value: withFallback(analysis.QuestionCoverage.Summary)},
			{Label: "Незакрытые вопросы", List: withFallbackList(analysis.QuestionCoverage.UnansweredQuestions)},
		},
	})
	sections = append(sections, reportSection{
		Title: "Качество менеджера",
		Rows: []reportRow{
			{Label: "Сильные стороны", List: withFallbackList(analysis.ManagerQuality.Strengths)},
			{Label: "Проблемы", List: withFallbackList(analysis.ManagerQuality.Issues)},
			{Label: "Рекомендации", List: withFallbackList(analysis.ManagerQuality.Recommendations)},
		},
	})
	sections = append(sections, reportSection{
		Title: "Итог, риски и следующие шаги",
		Rows: []reportRow{
			{Label: "Итог звонка", Value: withFallback(analysis.CallOutcome)},
			{Label: "Оценка", Value: scoreLabel(analysis.Score)},
			{Label: "Уверенность", Value: confidenceLabel(analysis.Confidence)},
			{Label: "Возражения клиента", List: withFallbackList(analysis.CustomerObjections)},
			{Label: "Риски", List: withFallbackList(analysis.Risks)},
			{Label: "Следующие шаги", List: withFallbackList(analysis.NextSteps)},
			{Label: "Главный следующий шаг", Value: withFallback(analysis.NextStep)},
		},
	})
	sections = append(sections, criteriaSection(analysis.CriteriaResults))
	sections = append(sections, reportSection{
		Title: "Общие цитаты-доказательства",
		Rows:  []reportRow{{List: withFallbackList(analysis.EvidenceQuotes)}},
	})

	if d.TranscriptionText != "" {
		sections = append(sections, reportSection{
			Title: "Транскрипция",
			Rows:  []reportRow{{Value: d.TranscriptionText}},
		})
	}

	return sections
}

func clientQuestionsSection(questions []clientQuestion) reportSection {
	if len(questions) == 0 {
		return reportSection{
			Title: "Вопросы клиента и ответы менеджера",
			Rows:  []reportRow{{Value: "Не указаны"}},
		}
	}

	rows := make([]reportRow, 0, len(questions)*4)
	for index, question := range questions {
		rows = append(rows,
			reportRow{Label: fmt.Sprintf("Вопрос %d", index+1), Value: withFallback(question.Question)},
			reportRow{Label: "Ответ менеджера", Value: withFallback(question.ManagerAnswer)},
			reportRow{Label: "Статус ответа", Value: answerStatusLabel(question.AnswerStatus)},
			reportRow{Label: "Цитаты", List: withFallbackList(question.EvidenceQuotes)},
		)
	}

	return reportSection{Title: "Вопросы клиента и ответы менеджера", Rows: rows}
}

func criteriaSection(criteria []criteriaResult) reportSection {
	if len(criteria) == 0 {
		return reportSection{
			Title: "Критерии инструкции",
			Rows:  []reportRow{{Value: "Не указаны"}},
		}
	}

	rows := make([]reportRow, 0, len(criteria)*3)
	for index, criterion := range criteria {
		title := criterion.Title
		if title == "" {
			title = criterion.InstructionTitle
		}
		if title == "" {
			title = fmt.Sprintf("Критерий %d", index+1)
		}
		result := criterion.Result
		if result == "" {
			result = criterionStatusLabel(criterion.Status)
		}
		rows = append(rows,
			reportRow{Label: title, Value: withFallback(result)},
			reportRow{Label: "Цитаты", List: withFallbackList(criterion.EvidenceQuotes)},
		)
	}

	return reportSection{Title: "Критерии инструкции", Rows: rows}
}

func withFallback(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Не указано"
	}
	return value
}

func withFallbackList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return []string{"Не указано"}
	}
	return out
}

func criterionStatusLabel(status string) string {
	switch status {
	case "met":
		return "Выполнено"
	case "partially_met":
		return "Частично выполнено"
	case "missed":
		return "Не выполнено"
	case "not_applicable":
		return "Не применимо"
	case "unclear":
		return "Неясно"
	default:
		return status
	}
}

func answerStatusLabel(status string) string {
	switch status {
	case "answered":
		return "Ответ дан"
	case "partially_answered":
		return "Ответ частичный"
	case "not_answered":
		return "Ответ не дан"
	case "no_questions":
		return "Вопросов не было"
	case "unclear":
		return "Неясно"
	default:
		return withFallback(status)
	}
}

func confidenceLabel(confidence string) string {
	switch confidence {
	case "high":
		return "Высокая"
	case "medium":
		return "Средняя"
	case "low":
		return "Низкая"
	default:
		return withFallback(confidence)
	}
}

func scoreLabel(score float64) string {
	if score == 0 {
		return "Не указана"
	}
	return fmt.Sprintf("%.0f/100", score)
}
