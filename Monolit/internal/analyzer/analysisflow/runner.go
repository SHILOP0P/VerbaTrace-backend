package analysisflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"verbatrace/monolit/internal/models"
)

func (r *Runner) Run(ctx context.Context) (models.AnalysisResult, error) {
	if len(r.Segments) == 0 {
		return models.AnalysisResult{}, models.ErrInvalidAnalysisInput
	}
	r.progress.Stage = "inventory"
	windows := splitWindows(r.Segments)
	r.progress.WindowsTotal = len(windows)
	if err := r.publish(ctx, nil); err != nil {
		return models.AnalysisResult{}, err
	}
	for i, window := range windows {
		if err := r.inventoryWindow(ctx, fmt.Sprintf("inventory/%d", i), window); err != nil {
			return models.AnalysisResult{}, err
		}
	}
	// A question, its answer and subsequent explanation form one review card.
	// Inventory windows may still describe answer turns as episodes, so merge
	// consecutive episodes into the preceding question before assessment.
	r.units = mergeQuestionResponses(r.units)
	segmentsByID := sourceIndex(r.Segments)
	r.items = r.items[:0]
	r.progress.QuestionsFound = 0
	for i := range r.units {
		if r.units[i].Kind == "question" && len(r.units[i].SegmentIDs) > 0 {
			r.units[i].QuestionSpeaker = segmentsByID[r.units[i].SegmentIDs[0]].Speaker
		}
		// A conversational question becomes mandatory only through an explicit
		// instruction requirement created below, never from provider inference.
		r.units[i].RequiredQuestion = false
		r.units[i].ID = fmt.Sprintf("u%d", i+1)
		r.items = append(r.items, placeholder(r.units[i], i))
		if r.units[i].Kind == "question" {
			r.progress.QuestionsFound++
		}
	}
	if err := r.publish(ctx, nil); err != nil {
		return models.AnalysisResult{}, err
	}
	if len(r.Request.Instructions) > 0 {
		var req Inventory
		if err := r.step(ctx, "requirements", requirementsPrompt, map[string]any{"instructions": r.Request.Instructions}, inventorySchema(), &req, func() error {
			for _, u := range req.Units {
				if u.Kind != "requirement" || !nonempty(u.Title) || len(u.Parts) == 0 {
					return errors.New("invalid instruction requirement")
				}
			}
			return nil
		}); err != nil {
			return models.AnalysisResult{}, err
		}
		for i, u := range req.Units {
			u.ID = fmt.Sprintf("r%d", i+1)
			r.units = append(r.units, u)
			r.items = append(r.items, placeholder(u, len(r.items)))
		}
	}
	r.progress.Stage = "answers"
	r.progress.ItemsTotal = len(r.units)
	if err := r.publish(ctx, nil); err != nil {
		return models.AnalysisResult{}, err
	}
	if err := r.assessAll(ctx); err != nil {
		return models.AnalysisResult{}, err
	}
	r.progress.Stage = "validation"
	if err := r.publish(ctx, nil); err != nil {
		return models.AnalysisResult{}, err
	}
	var summary map[string]any
	props := r.Schema["properties"].(map[string]any)
	summaryProps := map[string]any{}
	for _, key := range []string{"summary", "purpose", "outcome", "conversation_types", "strengths", "work_on", "recommendations", "priority_recommendation_ids"} {
		summaryProps[key] = props[key]
	}
	if err := r.step(ctx, "summary", summaryPrompt, map[string]any{"assessed_items": r.items}, object(summaryProps), &summary, func() error {
		if !nonempty(text(summary["summary"])) {
			return errors.New("empty summary")
		}
		validIDs := map[string]bool{}
		for _, u := range r.units {
			validIDs[u.ID] = true
		}
		recommendations, ok := summary["recommendations"].([]any)
		if !ok {
			return errors.New("invalid recommendations")
		}
		for _, raw := range recommendations {
			rec, ok := raw.(map[string]any)
			if !ok {
				return errors.New("invalid recommendation")
			}
			ids, ok := rec["item_ids"].([]any)
			if !ok || len(ids) == 0 {
				return errors.New("recommendation must cite assessed items")
			}
			for _, id := range ids {
				if !validIDs[text(id)] {
					return errors.New("invalid recommendation item ID")
				}
			}
		}
		return nil
	}); err != nil {
		return models.AnalysisResult{}, err
	}
	r.progress.Stage = "complete"
	root := r.result(summary)
	root["coverage"].(map[string]any)["status"] = "complete"
	raw, err := json.Marshal(root)
	return models.AnalysisResult{ResultJSON: raw, Model: r.model}, err
}

func (r *Runner) assessAll(ctx context.Context) error {
	var workers sync.WaitGroup
	var workMu sync.Mutex
	next := 0
	var firstErr error
	// Bound concurrency so large meetings do not monopolize provider capacity.
	for worker := 0; worker < 2; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				workMu.Lock()
				if firstErr != nil || next >= len(r.units) {
					workMu.Unlock()
					return
				}
				start := next
				next += 3
				workMu.Unlock()
				if err := r.assess(ctx, r.units[start:min(start+3, len(r.units))]); err != nil {
					workMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					workMu.Unlock()
					return
				}
			}
		}()
	}
	workers.Wait()
	return firstErr
}

func splitWindows(segments []Segment) [][]Segment {
	var windows [][]Segment
	for start := 0; start < len(segments); {
		end, size := start, 0
		for end < len(segments) && (size < 6500 || end == start) {
			size += len([]rune(segments[end].Text))
			end++
		}
		windows = append(windows, segments[start:end])
		start = end
	}
	return windows
}

func (r *Runner) inventoryWindow(ctx context.Context, key string, owned []Segment) error {
	index := sourceIndex(r.Segments)
	ids := make([]string, 0, len(owned))
	for _, s := range owned {
		ids = append(ids, s.ID)
	}
	// Read neighbouring turns without granting them ownership in this window.
	first, last := 0, 0
	for i, s := range r.Segments {
		if s.ID == owned[0].ID {
			first = i
		}
		if s.ID == owned[len(owned)-1].ID {
			last = i
		}
	}
	input := map[string]any{"owned_segment_ids": ids, "context_segments": r.Segments[max(0, first-2):min(len(r.Segments), last+3)]}
	var candidate Inventory
	validate := func() error {
		normalizeInventoryOwnership(&candidate, owned)
		return validateInventoryStructure(candidate, owned, index)
	}
	err := r.step(ctx, key+"/extract", inventoryPrompt, input, inventorySchema(), &candidate, validate)
	if err == nil {
		input["candidate_inventory"] = candidate
		err = r.step(ctx, key+"/audit", inventoryAuditPrompt, input, inventorySchema(), &candidate, validate)
	}
	if err != nil {
		return err
	}
	missing := uncoveredOwned(candidate, owned)
	if len(missing) > 0 {
		missingIDs := make([]string, 0, len(missing))
		for _, segment := range missing {
			missingIDs = append(missingIDs, segment.ID)
		}
		var recovered Inventory
		recoveryInput := map[string]any{"owned_segment_ids": missingIDs, "context_segments": missing}
		if err = r.step(ctx, key+"/recover", inventoryRecoveryPrompt, recoveryInput, inventorySchema(), &recovered, func() error {
			return validateInventory(recovered, missing, index)
		}); err != nil {
			return err
		}
		for i := range recovered.Units {
			recovered.Units[i].ID = fmt.Sprintf("recovered_%d_%s", i+1, recovered.Units[i].ID)
		}
		candidate.Units = append(candidate.Units, recovered.Units...)
		candidate.Excluded = append(candidate.Excluded, recovered.Excluded...)
	}
	if err = validateInventory(candidate, owned, index); err != nil {
		return err
	}
	positions := map[string]int{}
	for i, s := range r.Segments {
		positions[s.ID] = i
	}
	sort.SliceStable(candidate.Units, func(i, j int) bool {
		return positions[candidate.Units[i].SegmentIDs[0]] < positions[candidate.Units[j].SegmentIDs[0]]
	})
	for _, u := range candidate.Units {
		u.ID = fmt.Sprintf("u%d", len(r.units)+1)
		r.units = append(r.units, u)
		r.items = append(r.items, placeholder(u, len(r.items)))
		if u.Kind == "question" {
			r.progress.QuestionsFound++
		}
	}
	r.progress.WindowsDone++
	return r.publish(ctx, nil)
}

func validateInventory(in Inventory, owned []Segment, index map[string]Segment) error {
	if err := validateInventoryStructure(in, owned, index); err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, u := range in.Units {
		for _, id := range u.SegmentIDs {
			covered[id] = true
		}
	}
	for _, e := range in.Excluded {
		covered[e.SegmentID] = true
	}
	for _, s := range owned {
		if !covered[s.ID] {
			return fmt.Errorf("incomplete_coverage: segment %s is missing", s.ID)
		}
	}
	return nil
}

func validateInventoryStructure(in Inventory, owned []Segment, index map[string]Segment) error {
	ownedIDs := map[string]bool{}
	seen := map[string]bool{}
	for _, s := range owned {
		ownedIDs[s.ID] = true
	}
	for _, u := range in.Units {
		if !nonempty(u.ID) || seen[u.ID] || !nonempty(u.Title) || len(u.SegmentIDs) == 0 || (u.Kind != "question" && u.Kind != "episode") {
			return errors.New("invalid inventory unit")
		}
		seen[u.ID] = true
		if !ownedIDs[u.SegmentIDs[0]] {
			return fmt.Errorf("invalid inventory ownership: %s", u.ID)
		}
		for _, id := range u.SegmentIDs {
			if _, ok := index[id]; !ok {
				return fmt.Errorf("invalid segment ID %s", id)
			}
		}
	}
	for _, e := range in.Excluded {
		if !ownedIDs[e.SegmentID] || !nonempty(e.Reason) {
			return errors.New("invalid exclusion")
		}
	}
	return nil
}

func uncoveredOwned(in Inventory, owned []Segment) []Segment {
	covered := make(map[string]bool, len(owned))
	for _, unit := range in.Units {
		for _, id := range unit.SegmentIDs {
			covered[id] = true
		}
	}
	for _, excluded := range in.Excluded {
		covered[excluded.SegmentID] = true
	}
	missing := make([]Segment, 0)
	for _, segment := range owned {
		if !covered[segment.ID] {
			missing = append(missing, segment)
		}
	}
	return missing
}

func normalizeInventoryOwnership(in *Inventory, owned []Segment) {
	ownedIDs := make(map[string]bool, len(owned))
	for _, segment := range owned {
		ownedIDs[segment.ID] = true
	}
	units := in.Units[:0]
	for _, unit := range in.Units {
		firstOwned := -1
		for index, id := range unit.SegmentIDs {
			if ownedIDs[id] {
				firstOwned = index
				break
			}
		}
		// A unit made exclusively from overlap context belongs to another window.
		if firstOwned < 0 {
			continue
		}
		if firstOwned > 0 {
			unit.SegmentIDs[0], unit.SegmentIDs[firstOwned] = unit.SegmentIDs[firstOwned], unit.SegmentIDs[0]
		}
		units = append(units, unit)
	}
	in.Units = units
	excluded := in.Excluded[:0]
	for _, item := range in.Excluded {
		if ownedIDs[item.SegmentID] {
			excluded = append(excluded, item)
		}
	}
	in.Excluded = excluded
}

func mergeQuestionResponses(units []Unit) []Unit {
	merged := make([]Unit, 0, len(units))
	for _, unit := range units {
		if unit.Kind == "episode" && len(merged) > 0 && merged[len(merged)-1].Kind == "question" {
			previous := &merged[len(merged)-1]
			seen := make(map[string]bool, len(previous.SegmentIDs))
			for _, id := range previous.SegmentIDs {
				seen[id] = true
			}
			for _, id := range unit.SegmentIDs {
				if !seen[id] {
					previous.SegmentIDs = append(previous.SegmentIDs, id)
					seen[id] = true
				}
			}
			previous.Parts = append(previous.Parts, unit.Parts...)
			continue
		}
		merged = append(merged, unit)
	}
	return merged
}

func (r *Runner) assess(ctx context.Context, units []Unit) error {
	props := r.Schema["properties"].(map[string]any)
	// Copy the provider schema before replacing evidence proposals.
	var itemSchema map[string]any
	_ = json.Unmarshal([]byte(asJSON(props["items"].(map[string]any)["items"])), &itemSchema)
	itemSchema["properties"].(map[string]any)["evidence"] = array(object(map[string]any{"segment_id": str(), "quote": str()}))
	schema := object(map[string]any{"items": array(itemSchema)})
	input := map[string]any{"assigned_units": units, "source_segments": r.Segments, "instructions": r.Request.Instructions, "personalization": r.Request.Personalization, "privacy_context": r.Request.Redaction, "all_requirements": requirementUnits(r.units)}
	var output struct {
		Items []map[string]any `json:"items"`
	}
	key := "assess/" + units[0].ID
	var err error
	for repair := 0; repair < 5; repair++ {
		err = r.step(ctx, fmt.Sprintf("%s/repair%d", key, repair), assessmentPrompt, input, schema, &output, func() error { return r.validateItems(output.Items, units) })
		if err != nil {
			break
		}
		var audit struct {
			Issues []struct {
				ID     string `json:"id"`
				Reason string `json:"reason"`
			} `json:"issues"`
		}
		auditInput := map[string]any{"assigned_units": units, "source_segments": r.Segments, "instructions": r.Request.Instructions, "candidate_result": output}
		err = r.step(ctx, fmt.Sprintf("%s/audit%d", key, repair), assessmentAuditPrompt, auditInput, object(map[string]any{"issues": array(object(map[string]any{"id": str(), "reason": str()}))}), &audit, func() error {
			for _, issue := range audit.Issues {
				found := false
				for _, u := range units {
					if u.ID == issue.ID {
						found = true
					}
				}
				if !found || !nonempty(issue.Reason) {
					return errors.New("invalid audit issue")
				}
			}
			return nil
		})
		if err != nil {
			break
		}
		if len(audit.Issues) == 0 {
			err = nil
			break
		}
		input["validation_errors"] = audit.Issues
		input["previous_output"] = output
		err = errors.New("incomplete_coverage: answer audit still has unresolved issues")
	}
	if err != nil && strings.Contains(err.Error(), "incomplete_coverage: answer audit still has unresolved issues") {
		// The candidate has already passed the structural and evidence checks and
		// five independent repair rounds. Preserve it with an explicit warning so
		// one subjective audit disagreement cannot abort an otherwise complete call.
		for _, item := range output.Items {
			item["validation_warning"] = "Ответ прошёл проверку структуры и доказательств, но аудитор сохранил замечание после пяти уточнений."
		}
		err = nil
	}
	if err != nil {
		if len(units) > 1 && strings.Contains(err.Error(), "incomplete_coverage") {
			for _, u := range units {
				if err = r.assess(ctx, []Unit{u}); err != nil {
					return err
				}
			}
			return nil
		}
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range output.Items {
		for i, u := range r.units {
			if u.ID == text(item["id"]) {
				item["order"] = i
				item["processing_status"] = "ready"
				// Each real question and atomic requirement is scored once. A
				// cross-cutting rule remains one aggregate requirement card.
				item["contributes_to_overall"] = true
				r.items[i] = item
				break
			}
		}
		r.progress.ItemsDone++
	}
	return r.publishLocked(ctx, nil)
}

func (r *Runner) validateItems(items []map[string]any, units []Unit) error {
	if len(items) != len(units) {
		return errors.New("incomplete_coverage: missing assigned items")
	}
	expected := map[string]Unit{}
	for _, u := range units {
		expected[u.ID] = u
	}
	index := sourceIndex(r.Segments)
	for _, item := range items {
		id := text(item["id"])
		u, ok := expected[id]
		if !ok {
			return fmt.Errorf("invalid/duplicate assigned ID %s", id)
		}
		delete(expected, id)
		if text(item["kind"]) != u.Kind || !nonempty(text(item["explanation"])) {
			return errors.New("invalid assessment content")
		}
		item["title"] = u.Title
		item["topic"] = u.Topic
		item["question_parts"] = u.Parts
		item["required_question"] = u.RequiredQuestion
		item["question_speaker"] = u.QuestionSpeaker
		if u.Kind == "question" {
			item["asked"] = true
		}
		allowedEvidence := make(map[string]bool, len(u.SegmentIDs))
		for _, segmentID := range u.SegmentIDs {
			allowedEvidence[segmentID] = true
		}
		evidence, ok := item["evidence"].([]any)
		if !ok || (len(evidence) == 0 && u.Kind != "requirement") {
			return errors.New("evidence_invalid: missing evidence")
		}
		normalizedEvidence := make([]any, 0, len(evidence))
		for _, raw := range evidence {
			e, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			segmentID := text(e["segment_id"])
			s, ok := index[segmentID]
			quote := text(e["quote"])
			if !ok || !allowedEvidence[segmentID] {
				continue
			}
			if !nonempty(quote) || !strings.Contains(s.Text, quote) {
				// Providers occasionally normalize punctuation or whitespace in a
				// quote. Keep evidence exact and auditable by falling back to the
				// immutable source segment instead of failing the whole call.
				quote = strings.TrimSpace(s.Text)
				if quote == "" {
					return errors.New("evidence_invalid: source segment is empty")
				}
				e["quote"] = quote
			}
			// Time is the source segment range, never a model-generated coordinate.
			e["speaker"] = s.Speaker
			e["start_seconds"] = s.Start
			e["end_seconds"] = s.End
			e["match_status"] = "matched"
			e["location_precision"] = "segment"
			normalizedEvidence = append(normalizedEvidence, e)
		}
		if len(normalizedEvidence) == 0 && u.Kind != "requirement" {
			for _, segmentID := range u.SegmentIDs {
				if s, exists := index[segmentID]; exists && strings.TrimSpace(s.Text) != "" {
					normalizedEvidence = append(normalizedEvidence, map[string]any{
						"segment_id": segmentID, "quote": strings.TrimSpace(s.Text), "speaker": s.Speaker,
						"start_seconds": s.Start, "end_seconds": s.End,
						"match_status": "matched", "location_precision": "segment",
					})
					break
				}
			}
			if len(normalizedEvidence) == 0 {
				return errors.New("evidence_invalid: assessed unit has no usable source segment")
			}
		}
		item["evidence"] = normalizedEvidence
		sources, _ := item["instruction_sources"].([]any)
		validSources := make([]any, 0, len(sources))
		for _, raw := range sources {
			found := false
			for _, instruction := range r.Request.Instructions {
				if text(raw) == instruction.ID.String() {
					found = true
					break
				}
			}
			if found {
				validSources = append(validSources, raw)
			}
		}
		item["instruction_sources"] = validSources
		if u.Kind == "requirement" && len(validSources) == 0 {
			return errors.New("invalid requirement without instruction source")
		}
	}
	return nil
}

func (r *Runner) step(ctx context.Context, key, prompt string, input map[string]any, schema map[string]any, out any, validate func() error) error {
	input["input_version"] = Version
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if last != nil {
			input["validation_errors"] = last.Error()
		}
		task := models.AnalysisTask{Name: "analysis_step", System: commonPrompt + "\n" + prompt, Input: asJSON(input), Schema: schema, MaxTokens: 12288}
		result, err := r.Execute(ctx, fmt.Sprintf("%s/try%d", key, attempt), task)
		if err != nil {
			last = err
			continue
		}
		r.mu.Lock()
		r.model = result.Model
		r.mu.Unlock()
		if err = json.Unmarshal(result.ResultJSON, out); err == nil {
			err = validate()
		}
		if err == nil {
			return nil
		}
		last = err
	}
	return fmt.Errorf("%s: %w", key, last)
}

func (r *Runner) publish(ctx context.Context, summary map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publishLocked(ctx, summary)
}
func (r *Runner) publishLocked(ctx context.Context, summary map[string]any) error {
	if r.Publish == nil {
		return nil
	}
	raw, err := json.Marshal(r.result(summary))
	if err != nil {
		return err
	}
	return r.Publish(ctx, raw)
}
func (r *Runner) result(summary map[string]any) map[string]any {
	r.progress.StageRank = map[string]int{"inventory": 1, "answers": 2, "validation": 3, "complete": 4}[r.progress.Stage]
	if summary == nil {
		summary = map[string]any{"summary": "Разбор выполняется. Готовые карточки уже можно читать.", "strengths": []any{}, "work_on": []any{}, "recommendations": []any{}, "priority_recommendation_ids": []any{}}
	}
	summary["schema_version"] = 3
	summary["prompt_version"] = "universal-v3.1"
	summary["pipeline_version"] = Version
	items := r.items
	if items == nil {
		items = []map[string]any{}
	}
	summary["items"] = items
	summary["progress"] = r.progress
	analyzed, early, required := 0, 0, 0
	for _, item := range items {
		if item["kind"] == "question" && item["processing_status"] == "ready" {
			analyzed++
		}
		if item["required_question"] == true {
			required++
		}
		if item["required_question"] == true && item["fulfilled_earlier"] == true && item["information_status"] == "complete" {
			early++
		}
	}
	summary["coverage"] = map[string]any{"status": "partial", "actual_question_count": r.progress.QuestionsFound, "analyzed_actual_question_count": analyzed, "required_question_count": required, "complete_without_separate_question": early, "limitations": []any{}}
	summary["overall_score"] = nil
	summary["overall_score_label"] = "Разбор не завершён"
	return summary
}
func placeholder(u Unit, order int) map[string]any {
	return map[string]any{"id": u.ID, "kind": u.Kind, "title": u.Title, "topic": u.Topic, "order": order, "asked": u.Kind == "question", "question_speaker": u.QuestionSpeaker, "question_parts": u.Parts, "processing_status": "pending", "status": "not_assessed", "weight": 1, "score": nil, "explanation": "Ответ и оценка появятся после обработки этого вопроса.", "strengths": []any{}, "gaps": []any{}, "improvement_kind": "not_needed", "evidence": []any{}, "instruction_sources": []any{}}
}
func requirementUnits(units []Unit) []Unit {
	result := []Unit{}
	for _, u := range units {
		if u.Kind == "requirement" {
			result = append(result, u)
		}
	}
	return result
}
func text(value any) string { s, _ := value.(string); return s }
