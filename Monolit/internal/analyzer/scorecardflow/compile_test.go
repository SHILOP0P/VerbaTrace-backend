package scorecardflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
)

const instruction = "Менеджер обязан:\n- выяснить бюджет клиента;\n- назвать   следующий шаг и срок.\nГрубое нарушение — перебивать клиента."

func ptr(value string) *string { return &value }

func TestValidateUsesTheSpecifiedMessages(t *testing.T) {
	input := Input{InstructionText: instruction, PreviousCriteria: []PreviousCriterion{{Key: "k1"}, {Key: "k2"}}}
	output := Output{Criteria: []Criterion{
		{Title: "", Requirement: "x", SourceExcerpt: "выяснить бюджет клиента"},
		{Title: "Выяснил бюджет", Requirement: "", SourceExcerpt: "коротко"},
		{Title: "выяснил  БЮДЖЕТ", Requirement: "x", SourceExcerpt: "такого текста в инструкции нет", SameAs: ptr("missing")},
		{Title: "Назвал шаг", Requirement: "x", SourceExcerpt: "назвать следующий шаг и срок", SameAs: ptr("k1")},
		{Title: "Не перебивал", Requirement: "x", SourceExcerpt: "перебивать клиента", SameAs: ptr("k1")},
	}, NoCriteriaReason: "лишнее"}

	_, err := Validate(output, input, false)
	require.EqualError(t, err, strings.Join([]string{
		"no_criteria_reason заполнен при непустом criteria",
		"criteria[0].title пуст или длиннее 120 символов",
		"criteria[1].requirement пуст",
		"criteria[1].source_excerpt короче 10 символов",
		"criteria[2].title повторяет criteria[1].title",
		"criteria[2].source_excerpt не найден в instruction_text дословно",
		"criteria[2].same_as не существует в previous_criteria",
		"same_as k1 использован больше одного раза",
	}, "; "))
}

func TestValidateRejectsSameAsWithoutPreviousCriteria(t *testing.T) {
	_, err := Validate(Output{Criteria: []Criterion{{Title: "Выяснил бюджет", Requirement: "x", SourceExcerpt: "выяснить бюджет клиента", SameAs: ptr("k1")}}}, Input{InstructionText: instruction}, false)
	require.EqualError(t, err, "criteria[0].same_as должен быть null: previous_criteria не передан")
}

func TestValidateAcceptsWhitespaceDifferencesAndCleansFields(t *testing.T) {
	output, err := Validate(Output{Criteria: []Criterion{{
		Title: " Назвал следующий шаг ", Requirement: "Назвать следующий шаг и срок", SourceExcerpt: "назвать следующий шаг и срок",
		Weight: 7, Warnings: []string{"vague", "made_up", "vague"},
	}}}, Input{InstructionText: instruction}, false)
	require.NoError(t, err)
	require.Equal(t, "Назвал следующий шаг", output.Criteria[0].Title)
	require.Equal(t, 3, output.Criteria[0].Weight)
	require.Equal(t, []string{"vague"}, output.Criteria[0].Warnings)
}

func TestValidateOnLastAttemptKeepsCriterionWithUnverifiedExcerpt(t *testing.T) {
	input := Input{InstructionText: instruction}
	output := Output{Criteria: []Criterion{{Title: "Выяснил бюджет", Requirement: "x", SourceExcerpt: "не из текста вовсе"}}}

	_, err := Validate(output, input, false)
	require.Error(t, err)
	checked, err := Validate(output, input, true)
	require.NoError(t, err)
	require.Empty(t, checked.Criteria[0].SourceExcerpt)
	require.Equal(t, []string{WarningExcerptUnverified}, checked.Criteria[0].Warnings)
}

// Seen on a live compile: the model misspelt one word of a quote and changed a
// sign in another, and the whole paid compile was asked again twice.
func TestValidateRepairsAnAlmostVerbatimExcerptWithTheInstructionText(t *testing.T) {
	text := "### Подготовь три технические истории\n\nОбязательно уточни верхнюю границу диапазона: должно выполняться `0 <= n <= max`; иначе цикл может не завершиться."
	output, err := Validate(Output{Criteria: []Criterion{
		{Title: "Истории", Requirement: "x", SourceExcerpt: "Подготовь три техничесные истории"},
		{Title: "Граница", Requirement: "x", SourceExcerpt: "уточни верхнюю границу диапазона: должно выполняться 0 <= n < max"},
	}}, Input{InstructionText: text}, false)
	require.NoError(t, err)
	require.Equal(t, "Подготовь три технические истории", output.Criteria[0].SourceExcerpt)
	require.Equal(t, "уточни верхнюю границу диапазона: должно выполняться `0 <= n <= max", output.Criteria[1].SourceExcerpt)
	for _, criterion := range output.Criteria {
		require.Empty(t, criterion.Warnings)
		require.Contains(t, collapseSpaces(text), criterion.SourceExcerpt, "a repaired excerpt is the instruction's own text")
	}
}

func TestValidateToleratesAFewUnverifiableExcerptsInsteadOfAskingAgain(t *testing.T) {
	var lines []string
	var criteria []Criterion
	for i := 0; i < 10; i++ {
		requirement := fmt.Sprintf("пункт номер %d проверяется отдельно", i)
		lines = append(lines, "- "+requirement)
		criteria = append(criteria, Criterion{Title: fmt.Sprintf("Пункт %d", i), Requirement: "x", SourceExcerpt: requirement})
	}
	input := Input{InstructionText: strings.Join(lines, "\n")}
	criteria[4].SourceExcerpt = "совсем другой текст, которого нет"

	output, err := Validate(Output{Criteria: criteria}, input, false)
	require.NoError(t, err, "one in ten may stay unverified")
	require.Empty(t, output.Criteria[4].SourceExcerpt)
	require.Equal(t, []string{WarningExcerptUnverified}, output.Criteria[4].Warnings)

	criteria[7].SourceExcerpt = "и ещё одна выдуманная цитата"
	_, err = Validate(Output{Criteria: criteria}, input, false)
	require.EqualError(t, err, "criteria[4].source_excerpt не найден в instruction_text дословно; criteria[7].source_excerpt не найден в instruction_text дословно")
}

func TestValidateTreatsEmptyCriteriaWithReasonAsAnAnswer(t *testing.T) {
	output, err := Validate(Output{NoCriteriaReason: " Это справка о продукте "}, Input{InstructionText: "справка"}, false)
	require.NoError(t, err)
	require.Equal(t, "Это справка о продукте", output.NoCriteriaReason)

	_, err = Validate(Output{}, Input{InstructionText: "справка"}, false)
	require.EqualError(t, err, "criteria пуст, а no_criteria_reason не заполнен")
}

func TestCompileRetriesWithValidationErrorsAndStopsOnProviderErrors(t *testing.T) {
	var seen []Input
	answers := []Output{
		{Criteria: []Criterion{{Title: "Выяснил бюджет", Requirement: "x", SourceExcerpt: "нет такого"}}},
		{Criteria: []Criterion{{Title: "Выяснил бюджет", Requirement: "x", SourceExcerpt: "выяснить бюджет клиента"}}},
	}
	execute := func(_ context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		require.Equal(t, StepCompile, task.Name)
		var input Input
		require.NoError(t, json.Unmarshal([]byte(task.Input), &input))
		require.Equal(t, CompilerVersion, input.InputVersion)
		seen = append(seen, input)
		raw, _ := json.Marshal(answers[len(seen)-1])
		model := "m"
		return models.AnalysisResult{ResultJSON: raw, Model: &model}, nil
	}
	result, err := Compile(context.Background(), execute, Input{InstructionText: instruction})
	require.NoError(t, err)
	require.Equal(t, 2, result.Attempts)
	require.Equal(t, "m", result.Model)
	require.Empty(t, seen[0].ValidationErrors)
	require.Equal(t, "criteria[0].source_excerpt не найден в instruction_text дословно", seen[1].ValidationErrors)
	require.Equal(t, []string{seen[1].ValidationErrors}, result.Rejections)

	boom := errors.New("provider down")
	_, err = Compile(context.Background(), func(context.Context, string, models.AnalysisTask) (models.AnalysisResult, error) {
		return models.AnalysisResult{}, boom
	}, Input{InstructionText: instruction})
	require.ErrorIs(t, err, boom)
}

func TestCompileGivesUpAfterThreeInvalidAnswers(t *testing.T) {
	calls := 0
	_, err := Compile(context.Background(), func(context.Context, string, models.AnalysisTask) (models.AnalysisResult, error) {
		calls++
		return models.AnalysisResult{ResultJSON: json.RawMessage(`{"criteria":[],"no_criteria_reason":""}`)}, nil
	}, Input{InstructionText: instruction})
	require.ErrorIs(t, err, ErrInvalidOutput)
	require.Equal(t, MaxAttempts, calls)
}

func TestTaskOmitsEmptyOptionalInput(t *testing.T) {
	task := Task(Input{InstructionText: "текст"})
	require.NotContains(t, task.Input, "previous_criteria")
	require.NotContains(t, task.Input, "validation_errors")
	require.True(t, strings.HasPrefix(task.System, compileCommonPrompt+"\n"+"Этап C1:"))
}
