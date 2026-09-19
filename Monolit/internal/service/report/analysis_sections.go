package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/analysistext"
)

type analysisReport struct {
	SchemaVersion      int              `json:"schema_version"`
	PromptVersion      string           `json:"prompt_version"`
	Summary            string           `json:"summary"`
	Outcome            string           `json:"outcome"`
	Strengths          []string         `json:"strengths"`
	WorkOn             []string         `json:"work_on"`
	OverallScore       *float64         `json:"overall_score"`
	OverallScoreLabel  string           `json:"overall_score_label"`
	Items              []universalItem  `json:"items"`
	Recommendations    []recommendation `json:"recommendations"`
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

type universalItem struct {
	ID                 string              `json:"id"`
	Kind               string              `json:"kind"`
	Weight             *float64            `json:"weight"`
	Title              string              `json:"title"`
	Topic              string              `json:"topic"`
	AnswerSummary      *string             `json:"answer_summary"`
	Status             string              `json:"status"`
	Score              *float64            `json:"score"`
	Explanation        string              `json:"explanation"`
	Strengths          []string            `json:"strengths"`
	Gaps               []universalGap      `json:"gaps"`
	Improvement        *string             `json:"improvement"`
	InstructionSources []string            `json:"instruction_sources"`
	InstructionTitles  []string            `json:"instruction_titles"`
	Evidence           []universalEvidence `json:"evidence"`
}

type universalGap struct {
	Text        string `json:"text"`
	Explanation string `json:"explanation"`
}
type universalEvidence struct {
	Quote   string `json:"quote"`
	Speaker string `json:"speaker"`
}
type recommendation struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Action         string   `json:"action"`
	Reason         string   `json:"reason"`
	ExpectedResult string   `json:"expected_result"`
	Priority       string   `json:"priority"`
	PriorityScore  *float64 `json:"priority_score"`
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
	if analysis.SchemaVersion == 3 {
		return universalSections(analysis, d.TranscriptionText, d.analysisReader())
	}
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

func universalSections(analysis analysisReport, transcription string, reader analysisReader) []reportSection {
	score := analysis.OverallScoreLabel
	if score == "" && analysis.OverallScore != nil {
		score = scoreLabel(*analysis.OverallScore)
	}
	sections := []reportSection{
		{Title: "Общий вывод", Rows: []reportRow{{Value: withFallback(reader.text(analysis.Summary))}, {Label: "Результат разговора", Value: withFallback(reader.text(analysis.Outcome))}, {Label: "Оценка", Value: withFallback(score)}, {Label: "Сильные стороны", List: withFallbackList(reader.list(analysis.Strengths))}, {Label: "Над чем работать", List: withFallbackList(reader.list(analysis.WorkOn))}}},
	}
	for index, item := range analysis.Items {
		answer := ""
		if item.AnswerSummary != nil {
			answer = reader.text(*item.AnswerSummary)
		}
		improvement := ""
		if item.Improvement != nil {
			improvement = reader.text(*item.Improvement)
		}
		itemScore := "Не оценивается"
		if item.Score != nil {
			itemScore = scoreLabel(*item.Score)
		}
		gaps, evidence := make([]string, 0, len(item.Gaps)), make([]string, 0, len(item.Evidence))
		for _, gap := range item.Gaps {
			text := reader.text(gap.Text)
			if explanation := reader.text(gap.Explanation); explanation != "" {
				text += ": " + explanation
			}
			gaps = append(gaps, text)
		}
		for _, proof := range item.Evidence {
			evidence = append(evidence, reader.quote(proof))
		}
		sections = append(sections, reportSection{Title: reader.itemTitle(item, index), Rows: []reportRow{{Label: "Статус", Value: criterionStatusLabel(item.Status)}, {Label: "Оценка", Value: itemScore}, {Label: "Ответ или действие", Value: withFallback(answer)}, {Label: "Разбор", Value: withFallback(reader.text(item.Explanation))}, {Label: "Сильные стороны", List: withFallbackList(reader.list(item.Strengths))}, {Label: "Что не раскрыто", List: withFallbackList(gaps)}, {Label: "Эталонный ответ или совет", Value: withFallback(improvement)}, {Label: "Основания инструкции", List: instructionBasis(item)}, {Label: "Цитаты", List: withFallbackList(evidence)}}})
	}
	rows := make([]reportRow, 0, len(analysis.Recommendations))
	for index, rec := range analysis.Recommendations {
		priority := priorityLabel(rec.Priority)
		if rec.PriorityScore != nil {
			priority = fmt.Sprintf("%s · %.0f/100", priority, *rec.PriorityScore)
		}
		title := reader.text(rec.Title)
		if title == "" {
			title = fmt.Sprintf("Рекомендация %d", index+1)
		}
		parts := make([]string, 0, 4)
		for _, text := range []string{reader.text(rec.Action), reader.text(rec.Reason), reader.text(rec.ExpectedResult)} {
			if text != "" {
				parts = append(parts, text)
			}
		}
		parts = append(parts, "[приоритет: "+priority+"]")
		rows = append(rows, reportRow{Label: title, Value: strings.Join(parts, " ")})
	}
	sections = append(sections, reportSection{Title: "Приоритетные рекомендации", Rows: rows})
	if transcription != "" {
		sections = append(sections, reportSection{Title: "Транскрипция", Rows: []reportRow{{Value: transcription}}})
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

// criterionStatusLabel never prints a status code: one it does not know reads
// as not given.
func criterionStatusLabel(status string) string {
	switch status {
	case "met":
		return "Выполнено"
	case "mostly_met":
		return "В основном выполнено"
	case "partially_met":
		return "Частично выполнено"
	case "minimally_met":
		return "Минимально выполнено"
	case "missed":
		return "Не выполнено"
	case "not_applicable":
		return "Не применимо"
	case "unclear":
		return "Неясно"
	case "conflict":
		return "Конфликт требований"
	case "not_assessed":
		return "Не оценено"
	default:
		return "Не указано"
	}
}

func priorityLabel(priority string) string {
	switch priority {
	case "high":
		return "высокий"
	case "medium":
		return "средний"
	case "low":
		return "низкий"
	case "unresolved":
		return "не определён"
	default:
		return "не указан"
	}
}

// instructionBasis names the instructions a card rests on by their titles,
// never by their IDs.
func instructionBasis(item universalItem) []string {
	titles := make([]string, 0, len(item.InstructionTitles))
	for _, title := range item.InstructionTitles {
		if title = strings.TrimSpace(title); title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) == 0 && len(item.InstructionSources) > 0 {
		// Cards analysed before titles were stamped on them carry only the IDs.
		titles = append(titles, "Инструкция звонка")
	}
	return withFallbackList(titles)
}

// analysisReader makes the model's text of a v3 analysis readable: reference
// IDs go or become the titles they stand for, and speakers are named.
type analysisReader struct {
	titles map[string]string
	names  map[string]string
}

func (d ReportData) analysisReader() analysisReader {
	return analysisReader{titles: analysistext.ResultTitles(d.Analysis.ResultJSON), names: d.SpeakerNames}
}

func (r analysisReader) text(value string) string {
	return analysistext.ResolveSpeakerMarkersFunc(analysistext.StripReferenceIDs(value, r.titles), r.speaker)
}

// list cleans every entry; an entry that was only a list of IDs goes.
func (r analysisReader) list(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text := r.text(value); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// itemTitle is the title the card is cited by, which keeps a scorecard
// criterion's title as its author wrote it.
func (r analysisReader) itemTitle(item universalItem, index int) string {
	if title := r.titles[strings.TrimSpace(item.ID)]; title != "" {
		return analysistext.ResolveSpeakerMarkersFunc(title, r.speaker)
	}
	title := r.text(item.Title)
	if title == "" {
		title = r.text(item.Topic)
	}
	if title == "" {
		title = fmt.Sprintf("Пункт %d", index+1)
	}
	return title
}

// speaker names a speaker key as the report's transcript does, except that a
// bare key reads «Спикер A» rather than a lone letter.
func (r analysisReader) speaker(key string) string {
	key = strings.TrimSpace(key)
	if name := strings.TrimSpace(r.names[key]); name != "" {
		return name
	}
	if label := reportSpeakerLabel(key, nil); label != key {
		return label
	}
	return "Спикер " + key
}

// quote is a quote of the transcript, left as said, after its speaker's name.
func (r analysisReader) quote(proof universalEvidence) string {
	if strings.TrimSpace(proof.Speaker) == "" {
		return proof.Quote
	}
	return r.speaker(proof.Speaker) + ": " + proof.Quote
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
