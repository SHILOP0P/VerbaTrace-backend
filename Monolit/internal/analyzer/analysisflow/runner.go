package analysisflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"verbatrace/monolit/internal/models"
)

const (
	// Provider requests of one call run in two lanes so a flood of assessments
	// never delays the slowest inventory window, which gates the whole run.
	// Latency is output-bound, so wall time scales with these bounds; they still
	// keep a few concurrently analysed calls within provider rate limits.
	inventoryConcurrency = 8
	assessConcurrency    = 10
	assessBatchSize      = 3
	// Production runs showed that audit disagreements left after two targeted
	// repairs only re-litigated wording while every round cost a full assessment.
	maxAssessmentRepairs = 2
	repairWarning        = "Ответ прошёл проверку структуры и доказательств, но аудитор сохранил замечание после повторных уточнений."
)

var (
	auditCategories    = []string{"attribution", "missed_answer", "unsupported_claim", "unfair_penalty", "status_mismatch"}
	speakerMarker      = regexp.MustCompile(`\{\{speaker:([^{}]*)\}\}`)
	errPipelineStopped = errors.New("analysis pipeline stopped after an earlier step failed")
)

type auditIssue struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
	MustFix  bool   `json:"must_fix"`
}

// Run inventories windows, decomposes instructions and assesses units as one
// pipeline: a unit is assessed as soon as its final shape is known, instead of
// waiting for the slowest inventory window.
func (r *Runner) Run(ctx context.Context) (models.AnalysisResult, error) {
	if len(r.Segments) == 0 {
		return models.AnalysisResult{}, models.ErrInvalidAnalysisInput
	}
	r.index = sourceIndex(r.Segments)
	r.windows = splitWindows(r.Segments)
	r.windowUnits = make([][]Unit, len(r.windows))
	r.windowDone = make([]bool, len(r.windows))
	r.scheduled = map[unitRef]bool{}
	r.assessed = map[string]map[string]any{}
	r.tasks = &taskGroup{}
	r.inventorySlots = make(chan struct{}, inventoryConcurrency)
	r.assessSlots = make(chan struct{}, assessConcurrency)
	r.progress.Stage = "inventory"
	r.progress.WindowsTotal = len(r.windows)
	adhoc := r.takeScorecardRequirements()
	if err := r.publish(ctx, nil); err != nil {
		return models.AnalysisResult{}, err
	}
	if len(adhoc) > 0 {
		// Requirements depend only on instructions, so they overlap the inventory.
		r.tasks.Go(func() error { return r.decomposeInstructions(ctx, adhoc) })
	} else {
		r.mu.Lock()
		r.prepareAssessmentLocked()
		// Scorecard requirements are known before any window is read, so their
		// assessment need not wait for the inventory.
		r.scheduleLocked(ctx)
		r.mu.Unlock()
	}
	for w := range r.windows {
		r.tasks.Go(func() error { return r.inventoryWindow(ctx, w) })
	}
	if err := r.tasks.Wait(); err != nil {
		return models.AnalysisResult{}, err
	}
	r.mu.Lock()
	r.refreshLocked()
	unassessed := len(r.units) - len(r.assessed)
	r.mu.Unlock()
	if unassessed != 0 {
		return models.AnalysisResult{}, fmt.Errorf("incomplete_coverage: %d units were not assessed", unassessed)
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
	prompt, summaryInput := summaryPrompt, map[string]any{"assessed_items": summaryItems(r.items)}
	growth := r.Request.Growth
	if growth != nil {
		prompt += "\n" + growthPrompt
		summaryInput["growth_context"] = growthInput(growth)
		for key, value := range growthProperties(len(growth.OpenAreas) > 0) {
			summaryProps[key] = value
		}
	}
	var growthOutcome *models.GrowthOutcome
	if err := r.stepAttempts(ctx, nil, StepSummary, "summary", prompt, "", summaryInput, object(summaryProps), &summary, func(attempt int) error {
		growthOutcome = nil
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
		if growth == nil {
			return nil
		}
		outcome, err := readGrowth(summary)
		if err == nil {
			err = checkGrowth(outcome, growth, r.items)
		}
		if err == nil {
			growthOutcome = &outcome
			return nil
		}
		// A mistake in the growth fields never costs the analysis: after two
		// retries the summary is taken without them.
		if attempt < 2 {
			return err
		}
		if r.Warn != nil {
			r.Warn(ctx, "growth fields dropped from the summary: "+err.Error())
		}
		return nil
	}); err != nil {
		return models.AnalysisResult{}, err
	}
	normalizeSpeakerMarkers(summary, r.Segments)
	if growthOutcome != nil {
		// Markers in notes are normalized with the rest of the summary before the
		// fields leave it.
		if outcome, err := readGrowth(summary); err == nil {
			growthOutcome = &outcome
		}
	}
	delete(summary, "growth_observations")
	delete(summary, "new_growth_areas")
	if growthOutcome != nil && r.OnGrowth != nil {
		r.OnGrowth(ctx, *growthOutcome)
	}
	r.progress.Stage = "complete"
	root := r.result(summary)
	root["coverage"].(map[string]any)["status"] = "complete"
	raw, err := json.Marshal(root)
	return models.AnalysisResult{ResultJSON: raw, Model: r.model}, err
}

// takeScorecardRequirements turns the scorecard criteria of the request into
// requirement units and returns the instructions still to be broken down by
// the model. Without scorecards that is every instruction, as it always was.
func (r *Runner) takeScorecardRequirements() []models.AnalysisInstructionContent {
	scorecards := r.Request.Scorecards
	if scorecards == nil {
		return r.Request.Instructions
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requirementMeta = make(map[string]models.AnalysisRequirement, len(scorecards.Requirements))
	for i, requirement := range scorecards.Requirements {
		id := fmt.Sprintf("r%d", i+1)
		r.requirements = append(r.requirements, scorecardUnit(id, requirement))
		r.requirementMeta[id] = requirement
	}
	adhocIDs := make(map[string]bool, len(scorecards.AdhocInstructions))
	for _, id := range scorecards.AdhocInstructions {
		adhocIDs[id.String()] = true
	}
	adhoc := []models.AnalysisInstructionContent{}
	for _, instruction := range r.Request.Instructions {
		if adhocIDs[instruction.ID.String()] {
			adhoc = append(adhoc, instruction)
		}
	}
	r.refreshLocked()
	return adhoc
}

// scorecardUnit is built by the server, never by the model: the wording of a
// scorecard criterion is fixed, so every call is scored on the same text.
func scorecardUnit(id string, requirement models.AnalysisRequirement) Unit {
	parts := []string{
		"Требование: " + requirement.Requirement,
		"Инструкция-источник: " + requirement.InstructionID.String(),
		"Применимость: " + valueOr(requirement.Applicability, "всегда"),
		"Глубина: " + valueOr(requirement.Depth, "не задана"),
		"Важность: " + strconv.Itoa(requirement.Weight),
	}
	if requirement.CrossCutting {
		parts = append(parts, "Сквозное требование: проверяется по всему разговору")
	}
	if requirement.IsCritical {
		parts = append(parts, "Критичное требование")
	}
	return Unit{ID: id, Kind: "requirement", Title: requirement.Title, Topic: requirement.InstructionTitle, SegmentIDs: []string{}, Parts: parts, RequiredQuestion: requirement.RequiredQuestion}
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (r *Runner) decomposeInstructions(ctx context.Context, instructions []models.AnalysisInstructionContent) error {
	var req Inventory
	if err := r.step(ctx, nil, StepRequirements, "requirements", requirementsPrompt, "", map[string]any{"instructions": instructions}, inventorySchema(), &req, func() error {
		for _, u := range req.Units {
			if u.Kind != "requirement" || !nonempty(u.Title) || len(u.Parts) == 0 {
				return errors.New("invalid instruction requirement")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Numbering continues after the scorecard requirements already in place.
	for _, u := range req.Units {
		u.ID = fmt.Sprintf("r%d", len(r.requirements)+1)
		r.requirements = append(r.requirements, u)
	}
	r.prepareAssessmentLocked()
	r.refreshLocked()
	r.scheduleLocked(ctx)
	return r.publishLocked(ctx, nil)
}

// prepareAssessmentLocked freezes the shared assessment context. Every
// assessment reads all requirements, so none may start before they are known.
func (r *Runner) prepareAssessmentLocked() {
	r.assessmentContext = asJSON(map[string]any{
		"source_segments": compactSegments(r.Segments), "instructions": r.Request.Instructions,
		"personalization": r.Request.Personalization, "privacy_context": r.Request.Redaction,
		"all_requirements": requirementUnits(r.requirements),
	})
	r.contextReady = true
}

func (r *Runner) inventoryWindow(ctx context.Context, w int) error {
	units, err := r.extractWindow(ctx, fmt.Sprintf("inventory/%d", w), r.windows[w])
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.windowUnits[w], r.windowDone[w] = units, true
	r.progress.WindowsDone++
	r.refreshLocked()
	r.scheduleLocked(ctx)
	if err = r.publishLocked(ctx, nil); err != nil || r.progress.WindowsDone < len(r.windows) {
		return err
	}
	r.progress.Stage = "answers"
	return r.publishLocked(ctx, nil)
}

// refreshLocked rebuilds the published unit list in transcript order from the
// finished windows, keeping assessed cards in place of their placeholders.
func (r *Runner) refreshLocked() {
	entries := layoutUnits(r.windowUnits, r.windowDone, r.index)
	units := make([]Unit, 0, len(entries)+len(r.requirements))
	questions := 0
	for _, entry := range entries {
		units = append(units, entry.unit)
		if entry.unit.Kind == "question" {
			questions++
		}
	}
	units = append(units, r.requirements...)
	items := make([]map[string]any, 0, len(units))
	for i, u := range units {
		if item, ok := r.assessed[u.ID]; ok {
			item["order"] = i
			items = append(items, item)
			continue
		}
		items = append(items, placeholder(u, i))
	}
	r.units, r.items = units, items
	r.progress.QuestionsFound = questions
	r.progress.ItemsTotal = len(units)
	r.progress.ItemsDone = len(r.assessed)
}

// scheduleLocked starts assessment of every batch whose cards reached their
// final shape. A window's card list is fixed once none of its leading episodes
// may still merge backwards; batches are then fixed ranges of that list, so
// their composition, and therefore the durable step keys, never depend on the
// order in which windows finish.
func (r *Runner) scheduleLocked(ctx context.Context) {
	if !r.contextReady {
		return
	}
	byWindow := make([][]layoutEntry, len(r.windowUnits))
	for _, entry := range layoutUnits(r.windowUnits, r.windowDone, r.index) {
		byWindow[entry.head.window] = append(byWindow[entry.head.window], entry)
	}
	for w, entries := range byWindow {
		if slices.ContainsFunc(entries, unresolvedEpisode) {
			continue
		}
		for start := 0; start < len(entries); start += assessBatchSize {
			batch := entries[start:min(start+assessBatchSize, len(entries))]
			ref := unitRef{window: w, index: start}
			if r.scheduled[ref] || slices.ContainsFunc(batch, notFinal) {
				continue
			}
			r.scheduled[ref] = true
			units := make([]Unit, 0, len(batch))
			for _, entry := range batch {
				units = append(units, entry.unit)
			}
			r.tasks.Go(func() error { return r.assess(ctx, units) })
		}
	}
	if !r.requirementsOn {
		r.requirementsOn = true
		for start := 0; start < len(r.requirements); start += assessBatchSize {
			batch := r.requirements[start:min(start+assessBatchSize, len(r.requirements))]
			r.tasks.Go(func() error { return r.assess(ctx, batch) })
		}
	}
}

type layoutEntry struct {
	unit  Unit
	head  unitRef
	final bool
}

func notFinal(entry layoutEntry) bool { return !entry.final }

// unresolvedEpisode reports an episode that may still merge into a question of
// an unfinished earlier window. Questions never merge backwards.
func unresolvedEpisode(entry layoutEntry) bool { return entry.unit.Kind == "episode" && !entry.final }

// layoutUnits lists units in transcript order and merges consecutive answer
// episodes into the preceding question, because a question, its answer and the
// subsequent explanation form one review card even across window boundaries.
// An unfinished window is a barrier: the question before it may still absorb
// episodes and episodes after it may still merge backwards, so those units are
// not final yet.
func layoutUnits(windows [][]Unit, done []bool, index map[string]Segment) []layoutEntry {
	var entries []layoutEntry
	afterBarrier := false
	for w, units := range windows {
		if !done[w] {
			if last := len(entries) - 1; last >= 0 && entries[last].unit.Kind == "question" {
				entries[last].final = false
			}
			afterBarrier = true
			continue
		}
		for k, u := range units {
			last := len(entries) - 1
			if u.Kind == "episode" && !afterBarrier && last >= 0 && entries[last].unit.Kind == "question" {
				mergeEpisode(&entries[last].unit, u)
				continue
			}
			// An episode after a barrier, or after such an episode, may still merge
			// into a question of the unfinished window.
			unresolved := u.Kind == "episode" && (afterBarrier || (last >= 0 && entries[last].unit.Kind == "episode" && !entries[last].final))
			unit := u
			unit.ID = fmt.Sprintf("u%d.%d", w+1, k+1)
			unit.SegmentIDs = append([]string(nil), u.SegmentIDs...)
			unit.Parts = append([]string(nil), u.Parts...)
			// A conversational question becomes mandatory only through an explicit
			// instruction requirement, never from provider inference.
			unit.RequiredQuestion = false
			if unit.Kind == "question" && len(unit.SegmentIDs) > 0 {
				unit.QuestionSpeaker = index[unit.SegmentIDs[0]].Speaker
			}
			entries = append(entries, layoutEntry{unit: unit, head: unitRef{window: w, index: k}, final: !unresolved})
			afterBarrier = false
		}
	}
	return entries
}

func mergeEpisode(question *Unit, episode Unit) {
	seen := make(map[string]bool, len(question.SegmentIDs))
	for _, id := range question.SegmentIDs {
		seen[id] = true
	}
	for _, id := range episode.SegmentIDs {
		if !seen[id] {
			question.SegmentIDs = append(question.SegmentIDs, id)
			seen[id] = true
		}
	}
	question.Parts = append(question.Parts, episode.Parts...)
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

func (r *Runner) extractWindow(ctx context.Context, key string, owned []Segment) ([]Unit, error) {
	positions := make(map[string]int, len(r.Segments))
	for i, s := range r.Segments {
		positions[s.ID] = i
	}
	ids := make([]string, 0, len(owned))
	for _, s := range owned {
		ids = append(ids, s.ID)
	}
	// Read neighbouring turns without granting them ownership in this window.
	first, last := positions[owned[0].ID], positions[owned[len(owned)-1].ID]
	windowContext := asJSON(map[string]any{"context_segments": compactSegments(r.Segments[max(0, first-2):min(len(r.Segments), last+3)])})
	var candidate Inventory
	validate := func() error {
		normalizeInventoryOwnership(&candidate, owned)
		return validateInventoryStructure(candidate, owned, r.index)
	}
	err := r.step(ctx, r.inventorySlots, StepInventory, key+"/extract", inventoryPrompt, windowContext, map[string]any{"owned_segment_ids": ids}, inventorySchema(), &candidate, validate)
	if err == nil {
		// Freeze the candidate: the audit output is decoded into the same value.
		auditInput := map[string]any{"owned_segment_ids": ids, "candidate_inventory": json.RawMessage(asJSON(candidate))}
		candidate = Inventory{}
		err = r.step(ctx, r.inventorySlots, StepInventoryAudit, key+"/audit", inventoryAuditPrompt, windowContext, auditInput, inventorySchema(), &candidate, validate)
	}
	if err != nil {
		return nil, err
	}
	missing := uncoveredOwned(candidate, owned)
	if len(missing) > 0 {
		missingIDs := make([]string, 0, len(missing))
		for _, segment := range missing {
			missingIDs = append(missingIDs, segment.ID)
		}
		var recovered Inventory
		recoveryContext := asJSON(map[string]any{"context_segments": compactSegments(missing)})
		if err = r.step(ctx, r.inventorySlots, StepInventoryRecovery, key+"/recover", inventoryRecoveryPrompt, recoveryContext, map[string]any{"owned_segment_ids": missingIDs}, inventorySchema(), &recovered, func() error {
			return validateInventory(recovered, missing, r.index)
		}); err != nil {
			return nil, err
		}
		for i := range recovered.Units {
			recovered.Units[i].ID = fmt.Sprintf("recovered_%d_%s", i+1, recovered.Units[i].ID)
		}
		candidate.Units = append(candidate.Units, recovered.Units...)
		candidate.Excluded = append(candidate.Excluded, recovered.Excluded...)
	}
	if err = validateInventory(candidate, owned, r.index); err != nil {
		return nil, err
	}
	sort.SliceStable(candidate.Units, func(i, j int) bool {
		return positions[candidate.Units[i].SegmentIDs[0]] < positions[candidate.Units[j].SegmentIDs[0]]
	})
	return candidate.Units, nil
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

// assessmentSchema asks the provider only for fields it decides. Title and topic
// come from the inventory, order from the unit position and score from status.
func (r *Runner) assessmentSchema() map[string]any {
	props := r.Schema["properties"].(map[string]any)
	// Copy the provider schema before replacing evidence proposals.
	var itemSchema map[string]any
	_ = json.Unmarshal([]byte(asJSON(props["items"].(map[string]any)["items"])), &itemSchema)
	properties := itemSchema["properties"].(map[string]any)
	properties["evidence"] = array(object(map[string]any{"segment_id": str(), "quote": str()}))
	for _, key := range []string{"title", "topic", "order", "score"} {
		delete(properties, key)
	}
	return object(map[string]any{"items": array(object(properties))})
}

func (r *Runner) assess(ctx context.Context, units []Unit) error {
	schema := r.assessmentSchema()
	key := "assess/" + units[0].ID
	byID := make(map[string]Unit, len(units))
	for _, u := range units {
		byID[u.ID] = u
	}
	firstRoundFailure := func(err error) error {
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
	accepted := make(map[string]map[string]any, len(units))
	pending := units
	var issues []auditIssue
	var previous []map[string]any
	for round := 0; len(pending) > 0; round++ {
		input := map[string]any{"assigned_units": pending}
		if round > 0 {
			// Re-assess only the flagged cards; accepted ones are never regenerated.
			input["audit_issues"] = issues
			input["previous_output"] = previous
		}
		current := pending
		var output struct {
			Items []map[string]any `json:"items"`
		}
		if err := r.step(ctx, r.assessSlots, StepAssessment, fmt.Sprintf("%s/repair%d", key, round), assessmentPrompt, r.assessmentContext, input, schema, &output, func() error {
			return r.validateItems(output.Items, current)
		}); err != nil {
			if round == 0 {
				return firstRoundFailure(err)
			}
			// The previous candidates already passed the structural and evidence
			// checks, so a failed repair keeps them with an explicit warning.
			acceptWithWarning(accepted, previous)
			break
		}
		var err error
		if issues, err = r.auditAssessment(ctx, fmt.Sprintf("%s/audit%d", key, round), current, output.Items); err != nil {
			if round == 0 {
				return firstRoundFailure(err)
			}
			acceptWithWarning(accepted, output.Items)
			break
		}
		flagged := make(map[string]bool, len(issues))
		for _, issue := range issues {
			flagged[issue.ID] = true
		}
		pending, previous = nil, nil
		for _, item := range output.Items {
			id := text(item["id"])
			if flagged[id] {
				pending = append(pending, byID[id])
				previous = append(previous, item)
				continue
			}
			accepted[id] = item
		}
		if len(pending) > 0 && round == maxAssessmentRepairs {
			// One subjective audit disagreement must not abort an otherwise
			// complete call; keep the checked candidate and show the warning.
			acceptWithWarning(accepted, previous)
			break
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range units {
		item := accepted[u.ID]
		if item == nil {
			return fmt.Errorf("incomplete_coverage: unit %s has no accepted assessment", u.ID)
		}
		item["processing_status"] = "ready"
		// Each real question and atomic requirement is scored once. A
		// cross-cutting rule remains one aggregate requirement card.
		item["contributes_to_overall"] = true
		r.assessed[u.ID] = item
	}
	r.refreshLocked()
	return r.publishLocked(ctx, nil)
}

func acceptWithWarning(accepted map[string]map[string]any, items []map[string]any) {
	for _, item := range items {
		item["validation_warning"] = repairWarning
		accepted[text(item["id"])] = item
	}
}

// auditAssessment returns only actionable must-fix issues. Unfiltered audits
// listed confirmations and requests for unasked depth as issues, which forced
// repair rounds that could not converge. An issue about a card outside the batch
// or without a reason cannot be acted on, so it is dropped instead of retried.
func (r *Runner) auditAssessment(ctx context.Context, key string, units []Unit, items []map[string]any) ([]auditIssue, error) {
	var audit struct {
		Issues []auditIssue `json:"issues"`
	}
	schema := object(map[string]any{"issues": array(object(map[string]any{"id": str(), "category": enum(auditCategories...), "reason": str(), "must_fix": map[string]any{"type": "boolean"}}))})
	input := map[string]any{"assigned_units": units, "candidate_result": map[string]any{"items": items}}
	if err := r.step(ctx, r.assessSlots, StepAssessmentAudit, key, assessmentAuditPrompt, r.assessmentContext, input, schema, &audit, func() error { return nil }); err != nil {
		return nil, err
	}
	assigned := make(map[string]bool, len(units))
	for _, u := range units {
		assigned[u.ID] = true
	}
	issues := make([]auditIssue, 0, len(audit.Issues))
	for _, issue := range audit.Issues {
		if issue.MustFix && assigned[issue.ID] && nonempty(issue.Reason) {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

func (r *Runner) validateItems(items []map[string]any, units []Unit) error {
	if len(items) != len(units) {
		return errors.New("incomplete_coverage: missing assigned items")
	}
	expected := map[string]Unit{}
	for _, u := range units {
		expected[u.ID] = u
	}
	index := r.index
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
		normalizeSpeakerMarkers(item, r.Segments)
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
			// A requirement has no segments of its own: it is checked against the
			// whole conversation, so its evidence may come from anywhere in it.
			// Dropping that evidence left requirement cards without a moment to
			// jump to.
			if !ok || (!allowedEvidence[segmentID] && u.Kind != "requirement") {
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
		if requirement, ok := r.requirementMeta[u.ID]; ok {
			stampScorecardRequirement(item, requirement)
			if !slices.ContainsFunc(validSources, func(v any) bool { return text(v) == requirement.InstructionID.String() }) {
				validSources = append(validSources, requirement.InstructionID.String())
			}
		}
		item["instruction_sources"] = validSources
		if u.Kind == "requirement" && len(validSources) == 0 {
			return errors.New("invalid requirement without instruction source")
		}
	}
	return nil
}

// stampScorecardRequirement gives a requirement card its identity from the
// scorecard. The weight is the scorecard's too: the model is told to return 1
// for requirements, and whatever it returns is ignored.
func stampScorecardRequirement(item map[string]any, requirement models.AnalysisRequirement) {
	also := make([]any, 0, len(requirement.AlsoCriterionKeys))
	for _, key := range requirement.AlsoCriterionKeys {
		also = append(also, key.String())
	}
	item["criterion_key"] = requirement.CriterionKey.String()
	item["also_criterion_keys"] = also
	item["scorecard_uuid"] = requirement.ScorecardID.String()
	item["is_critical"] = requirement.IsCritical
	item["weight"] = requirement.Weight
}

// normalizeSpeakerMarkers rewrites markers that cite a segment ID, e.g.
// {{speaker:s218.1}}, to that segment's speaker so the name still resolves.
func normalizeSpeakerMarkers(value any, segments []Segment) {
	speakers := make(map[string]bool)
	bySegment := make(map[string]string, len(segments))
	for _, s := range segments {
		speakers[s.Speaker] = true
		bySegment[s.ID] = s.Speaker
	}
	var walk func(any) any
	walk = func(value any) any {
		switch v := value.(type) {
		case string:
			return speakerMarker.ReplaceAllStringFunc(v, func(marker string) string {
				id := speakerMarker.FindStringSubmatch(marker)[1]
				if speaker, ok := bySegment[id]; ok && !speakers[id] && speaker != "" {
					return "{{speaker:" + speaker + "}}"
				}
				return marker
			})
		case map[string]any:
			for key, nested := range v {
				v[key] = walk(nested)
			}
		case []any:
			for i, nested := range v {
				v[i] = walk(nested)
			}
		}
		return value
	}
	walk(value)
}

// summaryItems keeps each card's reasoning but drops evidence quotes and render
// fields: recommendations cite cards by ID, and quotes were most of the input.
func summaryItems(items []map[string]any) []map[string]any {
	keys := []string{"id", "kind", "title", "topic", "question_speaker", "required_question", "status", "information_status", "fulfilled_earlier", "weight", "answer_summary", "explanation", "strengths", "gaps", "improvement_kind", "improvement", "instruction_sources", "validation_warning"}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		compact := make(map[string]any, len(keys))
		for _, key := range keys {
			if value, ok := item[key]; ok {
				compact[key] = value
			}
		}
		result = append(result, compact)
	}
	return result
}

// step runs one structured provider request with validation retries. A nil
// slots channel leaves the request outside the concurrency lanes.
func (r *Runner) step(ctx context.Context, slots chan struct{}, kind, key, prompt, sharedContext string, input map[string]any, schema map[string]any, out any, validate func() error) error {
	return r.stepAttempts(ctx, slots, kind, key, prompt, sharedContext, input, schema, out, func(int) error { return validate() })
}

// stepAttempts is step for a validation that is softer on the last attempt.
func (r *Runner) stepAttempts(ctx context.Context, slots chan struct{}, kind, key, prompt, sharedContext string, input map[string]any, schema map[string]any, out any, validate func(attempt int) error) error {
	input["input_version"] = Version
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if r.tasks != nil && r.tasks.failed() {
			return fmt.Errorf("%s: %w", key, errPipelineStopped)
		}
		if last != nil {
			input["validation_errors"] = last.Error()
		}
		task := models.AnalysisTask{Name: kind, System: commonPrompt + "\n" + prompt, Context: sharedContext, Input: asJSON(input), Schema: schema, MaxTokens: 12288}
		if slots != nil {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		result, err := r.Execute(ctx, fmt.Sprintf("%s/try%d", key, attempt), task)
		if slots != nil {
			<-slots
		}
		if err != nil {
			last = err
			continue
		}
		r.mu.Lock()
		r.model = result.Model
		r.mu.Unlock()
		if err = json.Unmarshal(result.ResultJSON, out); err == nil {
			err = validate(attempt)
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
	summary["prompt_version"] = PromptVersion
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
	limitations := []any{}
	if scorecards := r.Request.Scorecards; scorecards != nil {
		applied := make([]any, 0, len(scorecards.Applied))
		for _, card := range scorecards.Applied {
			applied = append(applied, map[string]any{
				"scorecard_uuid": card.ScorecardID.String(), "instruction_uuid": card.InstructionID.String(),
				"instruction_version_uuid": card.VersionID.String(), "revision": card.Revision,
			})
		}
		summary["scorecards"] = applied
		summary["scorecard_mode"] = scorecards.Mode
		summary["scorecard_limit_applied"] = scorecards.LimitApplied
		if scorecards.LimitApplied {
			limitations = append(limitations, "Критериев в инструкциях больше предела на звонок; оценены первые "+strconv.Itoa(len(scorecards.Requirements))+".")
		}
	}
	summary["coverage"] = map[string]any{"status": "partial", "actual_question_count": r.progress.QuestionsFound, "analyzed_actual_question_count": analyzed, "required_question_count": required, "complete_without_separate_question": early, "limitations": limitations}
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
