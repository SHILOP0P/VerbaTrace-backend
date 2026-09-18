// Package scorecardflow turns the text of one instruction version into the
// criteria of its scorecard with a single structured model call. It knows the
// prompt, the schema and the checks; storing the result and paying for it is the
// caller's business.
package scorecardflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"
)

const (
	// StepCompile names the task, so a deterministic analyzer can answer it.
	StepCompile = "scorecard_compile"
	// CompilerVersion changes whenever the prompt, the schema or the checks do.
	CompilerVersion = "scorecard-v1"
	// MaxEnabled is how many criteria may be switched on in one scorecard.
	MaxEnabled    = 40
	MaxAttempts   = 3
	maxTitleRunes = 120
	minExcerpt    = 10
	maxTokens     = 12288
)

// Warning values the model may report; excerpt_unverified is set only by the
// server.
const (
	WarningCompound          = "compound"
	WarningNotObservable     = "not_observable"
	WarningVague             = "vague"
	WarningNegativeWording   = "negative_wording"
	WarningExcerptUnverified = "excerpt_unverified"
)

var modelWarnings = map[string]bool{WarningCompound: true, WarningNotObservable: true, WarningVague: true, WarningNegativeWording: true}

// ErrInvalidOutput means every attempt returned output that failed the checks.
// Retrying would most likely pay for the same mistake again.
var ErrInvalidOutput = errors.New("scorecard compile output failed validation")

type InstructionRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Scope string `json:"scope"`
}

type PreviousCriterion struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Requirement string `json:"requirement"`
}

type Input struct {
	InputVersion     string              `json:"input_version"`
	Instruction      InstructionRef      `json:"instruction"`
	InstructionText  string              `json:"instruction_text"`
	PreviousCriteria []PreviousCriterion `json:"previous_criteria,omitempty"`
	ValidationErrors string              `json:"validation_errors,omitempty"`
}

type Criterion struct {
	Title            string   `json:"title"`
	Requirement      string   `json:"requirement"`
	SourceExcerpt    string   `json:"source_excerpt"`
	Applicability    string   `json:"applicability"`
	Depth            string   `json:"depth"`
	RequiredQuestion bool     `json:"required_question"`
	CrossCutting     bool     `json:"cross_cutting"`
	Weight           int      `json:"weight"`
	IsCritical       bool     `json:"is_critical"`
	Warnings         []string `json:"warnings"`
	SameAs           *string  `json:"same_as"`
}

type Output struct {
	Criteria         []Criterion `json:"criteria"`
	NoCriteriaReason string      `json:"no_criteria_reason"`
}

type Result struct {
	Output   Output
	Model    string
	Attempts int
}

// Executor runs one provider request. The caller meters it; key identifies the
// attempt so the charge is idempotent.
type Executor func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error)

// Task builds the provider request for one attempt.
func Task(input Input) models.AnalysisTask {
	input.InputVersion = CompilerVersion
	raw, _ := json.Marshal(input)
	return models.AnalysisTask{
		Name:      StepCompile,
		System:    compileCommonPrompt + "\n" + scorecardCompilePrompt,
		Input:     string(raw),
		Schema:    Schema(),
		MaxTokens: maxTokens,
	}
}

// Compile asks for the criteria up to MaxAttempts times, passing the previous
// attempt's validation errors back. A provider error ends the call at once:
// retrying a transport failure is the queue's job, with a delay. An empty
// criteria list with a reason is a valid answer, not an error.
func Compile(ctx context.Context, execute Executor, input Input) (Result, error) {
	var last error
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		if last != nil {
			input.ValidationErrors = last.Error()
		}
		result, err := execute(ctx, fmt.Sprintf("try%d", attempt), Task(input))
		if err != nil {
			return Result{Attempts: attempt + 1}, err
		}
		model := ""
		if result.Model != nil {
			model = *result.Model
		}
		var output Output
		if err := json.Unmarshal(result.ResultJSON, &output); err != nil {
			last = fmt.Errorf("ответ не соответствует схеме: %v", err)
			continue
		}
		checked, err := Validate(output, input, attempt == MaxAttempts-1)
		if err == nil {
			return Result{Output: checked, Model: model, Attempts: attempt + 1}, nil
		}
		last = err
	}
	return Result{Attempts: MaxAttempts}, fmt.Errorf("%w: %v", ErrInvalidOutput, last)
}

// Validate applies the server checks. On the last attempt an excerpt that
// cannot be found no longer fails the compile: text extracted from a PDF is
// often too dirty for the model to quote exactly, and losing a whole scorecard
// over that would be worse than keeping the criterion without a quote.
func Validate(output Output, input Input, lastAttempt bool) (Output, error) {
	reason := strings.TrimSpace(output.NoCriteriaReason)
	if len(output.Criteria) == 0 {
		if reason == "" {
			return output, errors.New("criteria пуст, а no_criteria_reason не заполнен")
		}
		output.NoCriteriaReason = reason
		return output, nil
	}
	var problems []string
	if reason != "" {
		problems = append(problems, "no_criteria_reason заполнен при непустом criteria")
	}
	text := collapseSpaces(input.InstructionText)
	previous := map[string]bool{}
	for _, criterion := range input.PreviousCriteria {
		previous[criterion.Key] = true
	}
	titles := map[string]int{}
	usedSameAs := map[string]bool{}
	reportedSameAs := map[string]bool{}
	checked := make([]Criterion, len(output.Criteria))
	for n, raw := range output.Criteria {
		c := raw
		c.Title = strings.TrimSpace(c.Title)
		c.Requirement = strings.TrimSpace(c.Requirement)
		c.SourceExcerpt = strings.TrimSpace(c.SourceExcerpt)
		c.Applicability = strings.TrimSpace(c.Applicability)
		c.Depth = strings.TrimSpace(c.Depth)
		if c.Weight < 1 {
			c.Weight = 1
		}
		if c.Weight > 3 {
			c.Weight = 3
		}
		c.Warnings = knownWarnings(c.Warnings)

		if c.Title == "" || utf8.RuneCountInString(c.Title) > maxTitleRunes {
			problems = append(problems, fmt.Sprintf("criteria[%d].title пуст или длиннее 120 символов", n))
		} else if m, seen := titles[NormalizeTitle(c.Title)]; seen {
			problems = append(problems, fmt.Sprintf("criteria[%d].title повторяет criteria[%d].title", n, m))
		} else {
			titles[NormalizeTitle(c.Title)] = n
		}
		if c.Requirement == "" {
			problems = append(problems, fmt.Sprintf("criteria[%d].requirement пуст", n))
		}
		switch {
		case utf8.RuneCountInString(c.SourceExcerpt) < minExcerpt:
			if lastAttempt {
				c.SourceExcerpt = ""
				c.Warnings = appendWarning(c.Warnings, WarningExcerptUnverified)
			} else {
				problems = append(problems, fmt.Sprintf("criteria[%d].source_excerpt короче 10 символов", n))
			}
		case !strings.Contains(text, collapseSpaces(c.SourceExcerpt)):
			if lastAttempt {
				c.SourceExcerpt = ""
				c.Warnings = appendWarning(c.Warnings, WarningExcerptUnverified)
			} else {
				problems = append(problems, fmt.Sprintf("criteria[%d].source_excerpt не найден в instruction_text дословно", n))
			}
		}
		if c.SameAs != nil {
			key := strings.TrimSpace(*c.SameAs)
			switch {
			case key == "":
				c.SameAs = nil
			case len(input.PreviousCriteria) == 0:
				problems = append(problems, fmt.Sprintf("criteria[%d].same_as должен быть null: previous_criteria не передан", n))
			case !previous[key]:
				problems = append(problems, fmt.Sprintf("criteria[%d].same_as не существует в previous_criteria", n))
			case usedSameAs[key]:
				if !reportedSameAs[key] {
					problems = append(problems, fmt.Sprintf("same_as %s использован больше одного раза", key))
					reportedSameAs[key] = true
				}
			default:
				usedSameAs[key] = true
				c.SameAs = &key
			}
		}
		checked[n] = c
	}
	if len(problems) > 0 {
		return output, errors.New(strings.Join(problems, "; "))
	}
	output.Criteria = checked
	output.NoCriteriaReason = ""
	return output, nil
}

// NormalizeTitle is the comparison form of a title or a requirement: case,
// runs of whitespace and ё do not make two criteria different.
func NormalizeTitle(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	return strings.ReplaceAll(value, "ё", "е")
}

func collapseSpaces(value string) string { return strings.Join(strings.Fields(value), " ") }

func knownWarnings(values []string) []string {
	result := []string{}
	for _, value := range values {
		if modelWarnings[value] && !contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func appendWarning(values []string, warning string) []string {
	if contains(values, warning) {
		return values
	}
	return append(values, warning)
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Schema is the strict JSON schema of the compile answer.
func Schema() map[string]any {
	str := map[string]any{"type": "string"}
	boolean := map[string]any{"type": "boolean"}
	criterion := object(map[string]any{
		"title": str, "requirement": str, "source_excerpt": str, "applicability": str, "depth": str,
		"required_question": boolean, "cross_cutting": boolean, "is_critical": boolean,
		"weight":   map[string]any{"type": "integer", "minimum": 1, "maximum": 3},
		"warnings": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{WarningCompound, WarningNotObservable, WarningVague, WarningNegativeWording}}},
		"same_as":  map[string]any{"type": []string{"string", "null"}},
	})
	return object(map[string]any{
		"criteria":           map[string]any{"type": "array", "items": criterion},
		"no_criteria_reason": str,
	})
}

func object(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	// A stable order keeps the task hash, and so the charge, idempotent.
	sort.Strings(required)
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
