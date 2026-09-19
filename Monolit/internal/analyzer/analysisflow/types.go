// Package analysisflow owns exhaustive, resumable analysis independently of the provider.
package analysisflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"verbatrace/monolit/internal/models"
)

const Version = "universal-staged-v7"

// PromptVersion names the prompt texts. v3.2 described every status level and
// took the weight of scorecard requirements away from the model.
const PromptVersion = "universal-v3.2"

// Step kinds travel in AnalysisTask.Name. A provider sees them as the name of
// the JSON schema, so they are plain identifiers; a deterministic analyzer
// needs them to tell the steps apart without reading the prompt text.
const (
	StepInventory         = "inventory"
	StepInventoryAudit    = "inventory_audit"
	StepInventoryRecovery = "inventory_recovery"
	StepRequirements      = "requirements"
	StepAssessment        = "assessment"
	StepAssessmentAudit   = "assessment_audit"
	StepSummary           = "summary"
)

type Segment struct {
	ID      string   `json:"id"`
	Speaker string   `json:"speaker"`
	Start   *float64 `json:"start_seconds,omitempty"`
	End     *float64 `json:"end_seconds,omitempty"`
	Text    string   `json:"text"`
}

type Unit struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Title            string   `json:"title"`
	Topic            string   `json:"topic"`
	SegmentIDs       []string `json:"segment_ids"`
	Parts            []string `json:"parts"`
	RequiredQuestion bool     `json:"required_question"`
	QuestionSpeaker  string   `json:"question_speaker,omitempty"`
}

type Inventory struct {
	Units    []Unit `json:"units"`
	Excluded []struct {
		SegmentID string `json:"segment_id"`
		Reason    string `json:"reason"`
	} `json:"excluded"`
}

type Progress struct {
	Stage          string `json:"stage"`
	StageRank      int    `json:"stage_rank"`
	WindowsDone    int    `json:"windows_done"`
	WindowsTotal   int    `json:"windows_total"`
	ItemsDone      int    `json:"items_done"`
	ItemsTotal     int    `json:"items_total"`
	QuestionsFound int    `json:"questions_found"`
}

type Runner struct {
	mu       sync.Mutex
	Request  models.AnalysisRequest
	Segments []Segment
	Schema   map[string]any
	// Execute durably caches and meters each distinct task before returning.
	Execute  func(context.Context, string, models.AnalysisTask) (models.AnalysisResult, error)
	Publish  func(context.Context, json.RawMessage) error
	items    []map[string]any
	units    []Unit
	progress Progress
	model    *string
	// assessmentContext is the transcript and run-wide inputs shared verbatim by
	// every assessment and audit step, so the provider can cache it once.
	assessmentContext string

	index        map[string]Segment
	windows      [][]Segment
	windowUnits  [][]Unit
	windowDone   []bool
	requirements []Unit
	// requirementMeta holds the scorecard identity of requirement units built
	// from scorecards, keyed by unit ID.
	requirementMeta map[string]models.AnalysisRequirement
	contextReady    bool
	requirementsOn  bool
	scheduled       map[unitRef]bool
	assessed        map[string]map[string]any
	tasks           *taskGroup
	inventorySlots  chan struct{}
	assessSlots     chan struct{}
}

// unitRef addresses inventory unit index of window, before cross-window merging.
type unitRef struct{ window, index int }

// taskGroup runs pipeline tasks that may spawn further tasks. After the first
// error it accepts no new tasks and steps stop sending provider requests.
type taskGroup struct {
	wg  sync.WaitGroup
	mu  sync.Mutex
	err error
}

func (g *taskGroup) Go(fn func() error) {
	if g.failed() {
		return
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := fn(); err != nil {
			g.mu.Lock()
			if g.err == nil {
				g.err = err
			}
			g.mu.Unlock()
		}
	}()
}

func (g *taskGroup) failed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err != nil
}

func (g *taskGroup) Wait() error {
	g.wg.Wait()
	return g.err
}

func SourceSegments(t models.Transcription) []Segment {
	source := t.Segments
	if len(source) == 0 && t.Text != nil {
		source = []models.TranscriptionSegment{{Text: *t.Text}}
	}
	var result []Segment
	for i, s := range source {
		// Bound even a single very long ASR turn without dropping its tail.
		runes := []rune(s.Text)
		for start, part := 0, 0; start < len(runes); part++ {
			end := min(start+2400, len(runes))
			if end < len(runes) {
				for j := end; j > start+1200; j-- {
					if runes[j-1] == ' ' || runes[j-1] == '\n' {
						end = j
						break
					}
				}
			}
			result = append(result, Segment{ID: fmt.Sprintf("s%d.%d", i+1, part+1), Speaker: s.Speaker, Start: s.StartSeconds, End: s.EndSeconds, Text: string(runes[start:end])})
			start = end
		}
	}
	return result
}

// compactSegments renders segments as [id, speaker, text] tuples. Per-segment
// JSON keys and timestamps are a large share of a long transcript prompt, and
// evidence timing is always restored from the source segment anyway.
func compactSegments(segments []Segment) [][3]string {
	result := make([][3]string, 0, len(segments))
	for _, s := range segments {
		result = append(result, [3]string{s.ID, s.Speaker, s.Text})
	}
	return result
}

func object(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for k := range properties {
		required = append(required, k)
	}
	// Stable serialization is necessary for task hashes across worker restarts.
	sort.Strings(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func str() map[string]any                      { return map[string]any{"type": "string"} }
func array(item map[string]any) map[string]any { return map[string]any{"type": "array", "items": item} }
func enum(values ...string) map[string]any     { return map[string]any{"type": "string", "enum": values} }
func inventorySchema() map[string]any {
	return object(map[string]any{"units": array(object(map[string]any{"id": str(), "kind": enum("question", "episode", "requirement"), "title": str(), "topic": str(), "segment_ids": array(str()), "parts": array(str()), "required_question": map[string]any{"type": "boolean"}})), "excluded": array(object(map[string]any{"segment_id": str(), "reason": str()}))})
}
func asJSON(value any) string { data, _ := json.Marshal(value); return string(data) }
func sourceIndex(segments []Segment) map[string]Segment {
	result := map[string]Segment{}
	for _, s := range segments {
		result[s.ID] = s
	}
	return result
}
func nonempty(s string) bool { return strings.TrimSpace(s) != "" }
