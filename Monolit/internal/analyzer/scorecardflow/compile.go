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
	"unicode"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"
)

const (
	// StepCompile names the task, so a deterministic analyzer can answer it.
	StepCompile = "scorecard_compile"
	// CompilerVersion changes whenever the prompt, the schema or the checks do.
	CompilerVersion = "scorecard-v3"
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
	// Rejections are the validation errors of the attempts that were asked
	// again, in order; each one cost a full provider call.
	Rejections []string
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
	var rejections []string
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		if last != nil {
			input.ValidationErrors = last.Error()
			rejections = append(rejections, last.Error())
		}
		result, err := execute(ctx, fmt.Sprintf("try%d", attempt), Task(input))
		if err != nil {
			return Result{Attempts: attempt + 1, Rejections: rejections}, err
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
			return Result{Output: checked, Model: model, Attempts: attempt + 1, Rejections: rejections}, nil
		}
		last = err
	}
	rejections = append(rejections, last.Error())
	return Result{Attempts: MaxAttempts, Rejections: rejections}, fmt.Errorf("%w: %v", ErrInvalidOutput, last)
}

// Validate applies the server checks. An excerpt the model quoted almost word
// for word (a typo, a changed sign, a dropped word) is replaced by the text it
// was quoting. One that still cannot be found fails the attempt only when it is
// more than a few: every attempt repeats the whole paid compile, and asking
// again for 43 criteria over two bad quotes costs more than it gives, while the
// criterion keeps its warning for the owner to see. On the last attempt no
// excerpt fails the compile: text extracted from a PDF is often too dirty to
// quote exactly, and losing a whole scorecard over that would be worse.
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
	var textWords []wordSpan
	// Excerpt problems are tracked apart: alone and few, they do not fail the
	// attempt.
	excerptProblem := map[int]bool{}
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
			if textWords == nil {
				textWords = splitWords(input.InstructionText)
			}
			if repaired, ok := repairExcerpt(input.InstructionText, textWords, c.SourceExcerpt); ok {
				c.SourceExcerpt = collapseSpaces(repaired)
				break
			}
			c.SourceExcerpt = ""
			c.Warnings = appendWarning(c.Warnings, WarningExcerptUnverified)
			excerptProblem[len(problems)] = true
			problems = append(problems, fmt.Sprintf("criteria[%d].source_excerpt не найден в instruction_text дословно", n))
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
	onlyExcerpts := len(excerptProblem) == len(problems)
	if lastAttempt || (onlyExcerpts && len(excerptProblem) <= tolerableUnverified(len(output.Criteria))) {
		kept := problems[:0]
		for i, problem := range problems {
			if !excerptProblem[i] {
				kept = append(kept, problem)
			}
		}
		problems = kept
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

// tolerableUnverified is how many criteria may keep an unverified excerpt
// without asking the model again: one in ten.
func tolerableUnverified(criteria int) int { return criteria / 10 }

const (
	// repairMinWords keeps short quotes out of the repair: with few words a
	// near match says little about where the quote came from.
	repairMinWords = 4
	// repairMinShare is the share of the quote's words that must appear, in
	// order, in the span it is replaced with.
	repairMinShare = 0.8
)

// wordSpan is one word of a text in comparison form, with its place in the
// original.
type wordSpan struct {
	norm       string
	start, end int
}

func splitWords(text string) []wordSpan {
	var words []wordSpan
	start := -1
	flush := func(end int) {
		if start >= 0 {
			words = append(words, wordSpan{norm: strings.ReplaceAll(strings.ToLower(text[start:end]), "ё", "е"), start: start, end: end})
			start = -1
		}
	}
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(text))
	return words
}

// repairExcerpt finds the span of the instruction the model was quoting when
// its quote is not verbatim. Words are compared without case, punctuation and
// markup, and the span must hold most of the quote's words in the same order.
// What is returned is the instruction's own text, so a repaired excerpt is
// still a verbatim quote.
func repairExcerpt(text string, textWords []wordSpan, excerpt string) (string, bool) {
	quoted := splitWords(excerpt)
	if len(quoted) < repairMinWords {
		return "", false
	}
	// A span starts where one of the first words of the quote does, so a typo in
	// the very first word does not lose the match.
	starters := map[string]bool{}
	for _, word := range quoted[:min(3, len(quoted))] {
		if utf8.RuneCountInString(word.norm) >= 3 {
			starters[word.norm] = true
		}
	}
	bestShare, bestStart, bestEnd := 0.0, -1, -1
	window := len(quoted) + len(quoted)/5 + 2
	for j := range textWords {
		if !starters[textWords[j].norm] {
			continue
		}
		candidate := textWords[j:min(len(textWords), j+window)]
		matched, last := inOrder(quoted, candidate)
		share := float64(matched) / float64(len(quoted))
		if share > bestShare {
			bestShare, bestStart, bestEnd = share, textWords[j].start, candidate[last].end
		}
	}
	if bestShare < repairMinShare {
		return "", false
	}
	return text[bestStart:bestEnd], true
}

// sameWord forgives a typo: a word of four letters or more matches one that
// differs in at most a quarter of its letters and starts the same way.
func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	limit := min(len(ra), len(rb)) / 4
	if limit == 0 || ra[0] != rb[0] || len(ra)-len(rb) > limit || len(rb)-len(ra) > limit {
		return false
	}
	previous := make([]int, len(rb)+1)
	current := make([]int, len(rb)+1)
	for k := range previous {
		previous[k] = k
	}
	for i := 1; i <= len(ra); i++ {
		current[0] = i
		for k := 1; k <= len(rb); k++ {
			cost := 1
			if ra[i-1] == rb[k-1] {
				cost = 0
			}
			current[k] = min(previous[k]+1, current[k-1]+1, previous[k-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(rb)] <= limit
}

// inOrder is the longest common subsequence of two word lists, with the index
// in the second list of the last word it uses.
func inOrder(quoted, candidate []wordSpan) (int, int) {
	previous := make([]int, len(candidate)+1)
	current := make([]int, len(candidate)+1)
	for _, word := range quoted {
		for k := range candidate {
			switch {
			case sameWord(word.norm, candidate[k].norm):
				current[k+1] = previous[k] + 1
			case previous[k+1] >= current[k]:
				current[k+1] = previous[k+1]
			default:
				current[k+1] = current[k]
			}
		}
		previous, current = current, previous
	}
	best := previous[len(candidate)]
	for k := 1; k <= len(candidate); k++ {
		if previous[k] == best {
			return best, k - 1
		}
	}
	return 0, 0
}

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
