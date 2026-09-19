// Package mockstaged answers every step of the staged analysis pipeline without
// a network. Paid providers are never used to verify changes, so this is what
// the pipeline, the scorecard and everything built on analysis results are
// exercised against locally and in CI.
//
// Answers are deterministic: the same call and the same unit always get the
// same status, while different calls get different ones, so progress between
// calls can be reproduced. Markers in the transcript force an outcome for
// precise tests:
//
//	[[missed: Выяснил бюджет]]   status of the requirement titled "Выяснил бюджет"
//
// Any scoring status may be used instead of "missed", plus not_applicable and
// unclear. Growth areas, when the call keeps them:
//
//	[[growth:repeated: Отвечает общими словами]]   the open area of that title repeated
//	[[growth:improved: Отвечает общими словами]]   the area improved
//	[[growth:new: Перебивает клиента]]             a new area
package mockstaged

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"verbatrace/monolit/internal/analyzer/analysisflow"
	"verbatrace/monolit/internal/analyzer/openrouter"
	"verbatrace/monolit/internal/analyzer/scorecardflow"
	"verbatrace/monolit/internal/models"
)

const ProviderName = "mock_staged"

// ErrNotStaged is returned for a whole-call request: this analyzer only exists
// to run the staged pipeline.
var ErrNotStaged = errors.New("mock_staged answers only staged analysis tasks")

type Analyzer struct {
	model string
	steps map[string]stepFunc
}

type stepFunc func(models.AnalysisRequest, *models.AnalysisTask) (any, error)

func New(model string) *Analyzer {
	if strings.TrimSpace(model) == "" {
		model = ProviderName
	}
	a := &Analyzer{model: model}
	a.steps = map[string]stepFunc{
		analysisflow.StepInventory:         inventory,
		analysisflow.StepInventoryAudit:    inventoryAudit,
		analysisflow.StepInventoryRecovery: inventoryRecovery,
		analysisflow.StepRequirements:      requirements,
		analysisflow.StepAssessment:        assessment,
		analysisflow.StepAssessmentAudit:   assessmentAudit,
		analysisflow.StepSummary:           summary,
		scorecardflow.StepCompile:          compileScorecard,
	}
	return a
}

func (a *Analyzer) Provider() string { return ProviderName }

func (a *Analyzer) MaximumCompletionTokens(request models.AnalysisRequest) int64 {
	if request.Task != nil && request.Task.MaxTokens > 0 {
		return int64(request.Task.MaxTokens)
	}
	return 512
}

func (a *Analyzer) AnalysisSchema() map[string]any { return openrouter.AnalysisResponseSchema() }

func (a *Analyzer) Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error) {
	if err := ctx.Err(); err != nil {
		return models.AnalysisResult{}, err
	}
	if request.Task == nil {
		return models.AnalysisResult{}, ErrNotStaged
	}
	step, ok := a.steps[request.Task.Name]
	if !ok {
		return models.AnalysisResult{}, fmt.Errorf("mock_staged: unknown step %q", request.Task.Name)
	}
	output, err := step(request, request.Task)
	if err != nil {
		return models.AnalysisResult{}, err
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return models.AnalysisResult{}, err
	}
	model := a.model
	prompt := int64((len(request.Task.System) + len(request.Task.Context) + len(request.Task.Input) + 2) / 3)
	completion := int64((len(raw) + 2) / 3)
	return models.AnalysisResult{
		ResultJSON: raw,
		Model:      &model,
		// A token of cost keeps credit reservation and settlement on their real
		// path without moving any balance in a noticeable way.
		Usage: &models.ProviderUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion, CostNanoUSD: 1_000},
	}, nil
}

// Segment is a compact transcript turn as the pipeline sends it: [id, speaker, text].
type Segment [3]string

func (s Segment) ID() string      { return s[0] }
func (s Segment) Speaker() string { return s[1] }
func (s Segment) Text() string    { return s[2] }

func decode(raw string, out any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), out)
}

func inventory(_ models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input struct {
		Owned []string `json:"owned_segment_ids"`
	}
	var shared struct {
		Segments []Segment `json:"context_segments"`
	}
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	if err := decode(task.Context, &shared); err != nil {
		return nil, err
	}
	byID := make(map[string]Segment, len(shared.Segments))
	for _, s := range shared.Segments {
		byID[s.ID()] = s
	}
	units := []analysisflow.Unit{}
	var current *analysisflow.Unit
	for _, id := range input.Owned {
		text := byID[id].Text()
		if strings.Contains(stripMarkers(text), "?") {
			title := unitTitle(text, id)
			units = append(units, analysisflow.Unit{ID: fmt.Sprintf("q%d", len(units)+1), Kind: "question", Title: title, Topic: "Разговор", SegmentIDs: []string{id}, Parts: []string{title}})
			current = &units[len(units)-1]
			continue
		}
		if current == nil {
			units = append(units, analysisflow.Unit{ID: fmt.Sprintf("e%d", len(units)+1), Kind: "episode", Title: "Продолжение разговора", Topic: "Разговор", SegmentIDs: []string{id}, Parts: []string{unitTitle(text, id)}})
			current = &units[len(units)-1]
			continue
		}
		current.SegmentIDs = append(current.SegmentIDs, id)
	}
	return map[string]any{"units": units, "excluded": []any{}}, nil
}

func inventoryAudit(_ models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input struct {
		Candidate json.RawMessage `json:"candidate_inventory"`
	}
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	if len(input.Candidate) == 0 {
		return map[string]any{"units": []any{}, "excluded": []any{}}, nil
	}
	return input.Candidate, nil
}

func inventoryRecovery(_ models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input struct {
		Owned []string `json:"owned_segment_ids"`
	}
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	excluded := make([]map[string]string, 0, len(input.Owned))
	for _, id := range input.Owned {
		excluded = append(excluded, map[string]string{"segment_id": id, "reason": "Служебная реплика"})
	}
	return map[string]any{"units": []any{}, "excluded": excluded}, nil
}

// instructionInput mirrors models.AnalysisInstructionContent as it is
// serialized into step input: that struct has no JSON tags.
type instructionInput struct {
	ID      string `json:"ID"`
	Title   string `json:"Title"`
	Content string `json:"Content"`
}

func requirements(_ models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input struct {
		Instructions []instructionInput `json:"instructions"`
	}
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	units := []analysisflow.Unit{}
	for _, instruction := range input.Instructions {
		lines := ListItems(instruction.Content)
		if len(lines) == 0 {
			lines = []string{instruction.Title}
		}
		for _, line := range lines {
			title := CleanTitle(line)
			if title == "" {
				continue
			}
			units = append(units, analysisflow.Unit{
				ID: fmt.Sprintf("r%d", len(units)+1), Kind: "requirement", Title: title, Topic: instruction.Title,
				SegmentIDs: []string{}, Parts: []string{"Требование: " + line, "Инструкция-источник: " + instruction.ID},
			})
		}
	}
	return map[string]any{"units": units, "excluded": []any{}}, nil
}

var listItem = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+(.+?)\s*$`)

// ListItems returns the bulleted or numbered lines of an instruction. It is the
// deterministic stand-in for the model's decomposition of an instruction.
func ListItems(content string) []string {
	var items []string
	for _, line := range strings.Split(content, "\n") {
		if match := listItem.FindStringSubmatch(line); match != nil {
			items = append(items, strings.TrimSpace(match[1]))
		}
	}
	return items
}

var marker = regexp.MustCompile(`\[\[\s*([a-z_]+)\s*:\s*([^\]]+?)\s*\]\]`)

// CleanTitle removes test markers and caps the length the way a model title
// would be capped.
func CleanTitle(text string) string {
	text = strings.Join(strings.Fields(stripMarkers(text)), " ")
	if utf8.RuneCountInString(text) > 80 {
		text = string([]rune(text)[:80])
	}
	return strings.TrimSpace(text)
}

func stripMarkers(text string) string { return marker.ReplaceAllString(text, "") }

func unitTitle(text, id string) string {
	if title := CleanTitle(text); title != "" {
		return title
	}
	return "Реплика " + id
}

// NormalizeTitle is the comparison form used to match markers to titles.
func NormalizeTitle(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	return strings.ReplaceAll(value, "ё", "е")
}

var scoringStatuses = []string{"met", "met", "mostly_met", "partially_met", "minimally_met", "missed"}

var markerStatuses = map[string]bool{"met": true, "mostly_met": true, "partially_met": true, "minimally_met": true, "missed": true, "not_applicable": true, "unclear": true}

// forcedStatuses reads [[status: title]] markers from the whole conversation.
func forcedStatuses(segments []Segment) map[string]string {
	forced := map[string]string{}
	for _, s := range segments {
		for _, match := range marker.FindAllStringSubmatch(s.Text(), -1) {
			if markerStatuses[match[1]] {
				forced[NormalizeTitle(match[2])] = match[1]
			}
		}
	}
	return forced
}

// Pick maps a stable key to one of n choices.
func Pick(key string, n int) int {
	sum := sha256.Sum256([]byte(key))
	return int(binary.BigEndian.Uint32(sum[:4]) % uint32(n))
}

func assessment(request models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input struct {
		Units []analysisflow.Unit `json:"assigned_units"`
	}
	var shared struct {
		Segments     []Segment          `json:"source_segments"`
		Instructions []instructionInput `json:"instructions"`
	}
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	if err := decode(task.Context, &shared); err != nil {
		return nil, err
	}
	byID := make(map[string]Segment, len(shared.Segments))
	for _, s := range shared.Segments {
		byID[s.ID()] = s
	}
	forced := forcedStatuses(shared.Segments)
	growth := growthMarkers(shared.Segments)
	items := make([]map[string]any, 0, len(input.Units))
	for _, u := range input.Units {
		status, ok := forced[NormalizeTitle(u.Title)]
		if !ok {
			status = scoringStatuses[Pick(request.CallUUID.String()+"/"+NormalizeTitle(u.Title), len(scoringStatuses))]
		}
		item := assessedItem(u, status, byID, shared.Segments, shared.Instructions)
		if len(growth) > 0 {
			// The summary step sees cards, not the transcript: growth markers
			// travel to it inside the explanation.
			item["explanation"] = stringOf(item["explanation"]) + " " + strings.Join(growth, " ")
		}
		items = append(items, item)
	}
	return map[string]any{"items": items}, nil
}

var growthMarker = regexp.MustCompile(`\[\[\s*growth\s*:\s*(repeated|improved|new)\s*:\s*([^\]]+?)\s*\]\]`)

// growthMarkers are the [[growth:verdict:title]] markers of the conversation.
func growthMarkers(segments []Segment) []string {
	var found []string
	for _, s := range segments {
		found = append(found, growthMarker.FindAllString(s.Text(), -1)...)
	}
	return found
}

func stringOf(value any) string { s, _ := value.(string); return s }

func assessedItem(u analysisflow.Unit, status string, byID map[string]Segment, all []Segment, instructions []instructionInput) map[string]any {
	item := map[string]any{
		"id": u.ID, "kind": u.Kind, "asked": u.Kind == "question", "fulfilled_earlier": false,
		"status": status, "weight": 1, "strengths": []any{}, "gaps": []any{},
		"improvement_kind": "not_needed", "improvement": nil, "instruction_sources": []any{},
		"answer_summary":     "Тестовый ответ mock_staged.",
		"explanation":        "Тестовая оценка mock_staged: статус выбран детерминированно по звонку и названию карточки.",
		"information_status": informationStatus(status),
	}
	if status == "met" || status == "mostly_met" {
		item["strengths"] = []any{"Требование раскрыто по существу."}
	}
	if status != "met" && status != "not_applicable" && status != "unclear" {
		basis := "explicit_question"
		if u.Kind == "requirement" {
			basis = "instruction"
		}
		item["gaps"] = []any{map[string]any{"text": "Тестовый пробел", "basis": basis, "explanation": "Пробел задан детерминированно для проверки.", "affects_score": true}}
		item["improvement_kind"] = "advice"
		item["improvement"] = "Тестовая рекомендация: раскрыть требование полнее."
	}
	var source Segment
	if len(u.SegmentIDs) > 0 {
		source = byID[u.SegmentIDs[0]]
	} else if len(all) > 0 {
		source = all[Pick(u.Title, len(all))]
	}
	if source.ID() != "" {
		item["evidence"] = []any{map[string]any{"segment_id": source.ID(), "quote": quote(source.Text())}}
	} else {
		item["evidence"] = []any{}
	}
	if u.Kind == "requirement" {
		item["instruction_sources"] = []any{instructionSource(u, instructions)}
	}
	return item
}

func informationStatus(status string) any {
	switch status {
	case "met":
		return "complete"
	case "missed":
		return "absent"
	case "not_applicable":
		return nil
	case "unclear":
		return "unclear"
	default:
		return "partial"
	}
}

// quote returns a prefix of the segment, which is always an exact substring.
func quote(text string) string {
	runes := []rune(text)
	if len(runes) > 80 {
		runes = runes[:80]
	}
	if q := strings.TrimSpace(string(runes)); q != "" {
		return q
	}
	return text
}

func instructionSource(u analysisflow.Unit, instructions []instructionInput) string {
	for _, instruction := range instructions {
		for _, part := range u.Parts {
			if instruction.ID != "" && strings.Contains(part, instruction.ID) {
				return instruction.ID
			}
		}
	}
	if len(instructions) > 0 {
		return instructions[0].ID
	}
	return ""
}

func assessmentAudit(models.AnalysisRequest, *models.AnalysisTask) (any, error) {
	return map[string]any{"issues": []any{}}, nil
}

func summary(_ models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input struct {
		Items  []map[string]any      `json:"assessed_items"`
		Growth *models.GrowthContext `json:"growth_context"`
	}
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	answer, err := summaryAnswer(input.Items)
	if err != nil || input.Growth == nil {
		return answer, err
	}
	growthAnswer(answer, input.Items, input.Growth)
	return answer, nil
}

// growthAnswer judges every open area not_applicable unless a marker says
// otherwise, citing the first question or episode card; [[growth:new:title]]
// opens an area.
func growthAnswer(answer map[string]any, items []map[string]any, growth *models.GrowthContext) {
	verdicts := map[string]string{}
	var created []string
	for _, item := range items {
		for _, match := range growthMarker.FindAllStringSubmatch(stringOf(item["explanation"]), -1) {
			if match[1] == "new" {
				if !slices.Contains(created, match[2]) {
					created = append(created, match[2])
				}
				continue
			}
			verdicts[NormalizeTitle(match[2])] = match[1]
		}
	}
	conversational := ""
	for _, item := range items {
		if stringOf(item["kind"]) != "requirement" {
			conversational = stringOf(item["id"])
			break
		}
	}
	cited := conversational
	if cited == "" && len(items) > 0 {
		cited = stringOf(items[0]["id"])
	}
	if len(growth.OpenAreas) > 0 {
		observations := []any{}
		for _, area := range growth.OpenAreas {
			verdict, ok := verdicts[NormalizeTitle(area.Title)]
			ids := []any{cited}
			if !ok || cited == "" {
				verdict, ids = "not_applicable", []any{}
			}
			observations = append(observations, map[string]any{"area_id": area.ID, "verdict": verdict, "item_ids": ids, "note": "Тестовое наблюдение mock_staged."})
		}
		answer["growth_observations"] = observations
	}
	areas := []any{}
	for _, title := range created {
		if conversational == "" || len(areas) == 3 {
			break
		}
		areas = append(areas, map[string]any{"title": title, "description": "Тестовая зона роста mock_staged.", "item_ids": []any{conversational}})
	}
	answer["new_growth_areas"] = areas
}

func summaryAnswer(items []map[string]any) (map[string]any, error) {
	workOn := []any{}
	recommendations := []any{}
	for _, item := range items {
		status, _ := item["status"].(string)
		if status == "met" || status == "not_applicable" || status == "unclear" || status == "not_assessed" {
			continue
		}
		title, _ := item["title"].(string)
		id, _ := item["id"].(string)
		if len(workOn) < 3 {
			workOn = append(workOn, title)
		}
		if len(recommendations) < 2 && id != "" {
			recommendations = append(recommendations, map[string]any{
				"id": fmt.Sprintf("rec%d", len(recommendations)+1), "title": "Поработать: " + title,
				"action": "Раскрыть требование полнее.", "reason": "Карточка оценена ниже полного выполнения.",
				"expected_result": "Требование выполняется полностью.", "item_ids": []any{id},
				"affects_score": true, "importance": 2, "impact": 2, "repetition": 1,
				"priority_score": nil, "priority": "medium",
			})
		}
	}
	return map[string]any{
		"summary":                     "Тестовый итог mock_staged: разговор разобран по всем карточкам.",
		"purpose":                     "Проверка поэтапного анализа без платного провайдера.",
		"outcome":                     "Тестовый результат.",
		"conversation_types":          []any{"test"},
		"strengths":                   []any{"Разговор разобран полностью."},
		"work_on":                     workOn,
		"recommendations":             recommendations,
		"priority_recommendation_ids": []any{},
	}, nil
}

var (
	criticalMarker = regexp.MustCompile(`\[\[\s*critical\s*\]\]`)
	weightMarker   = regexp.MustCompile(`\[\[\s*weight\s*:\s*([123])\s*\]\]`)
)

// compileScorecard stands in for the model when an instruction is compiled into
// a scorecard: every bulleted or numbered line becomes a criterion. [[critical]]
// and [[weight:3]] in a line set those fields, and a line whose title matches a
// criterion of the previous scorecard keeps its key.
func compileScorecard(_ models.AnalysisRequest, task *models.AnalysisTask) (any, error) {
	var input scorecardflow.Input
	if err := decode(task.Input, &input); err != nil {
		return nil, err
	}
	previous := map[string]string{}
	for _, criterion := range input.PreviousCriteria {
		previous[NormalizeTitle(criterion.Title)] = criterion.Key
	}
	criteria := []scorecardflow.Criterion{}
	for _, line := range ListItems(input.InstructionText) {
		weight := 1
		if match := weightMarker.FindStringSubmatch(line); match != nil {
			weight = int(match[1][0] - '0')
		}
		title := CleanTitle(weightMarker.ReplaceAllString(criticalMarker.ReplaceAllString(line, ""), ""))
		if title == "" {
			continue
		}
		criterion := scorecardflow.Criterion{
			Title: title, Requirement: line, SourceExcerpt: line, Weight: weight,
			IsCritical: criticalMarker.MatchString(line), Warnings: []string{},
		}
		if key, ok := previous[NormalizeTitle(title)]; ok {
			criterion.SameAs = &key
			delete(previous, NormalizeTitle(title))
		}
		criteria = append(criteria, criterion)
	}
	if len(criteria) == 0 {
		return scorecardflow.Output{Criteria: criteria, NoCriteriaReason: "В тексте нет списка требований."}, nil
	}
	return scorecardflow.Output{Criteria: criteria}, nil
}
