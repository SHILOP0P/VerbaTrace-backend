package report

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"verbatrace/monolit/internal/models"
)

// A v3 analysis as the model left it: reference IDs, speaker markers and
// instruction IDs in its text.
const rawV3Analysis = `{
	"schema_version": 3, "summary": "Бронь оформлена и подтверждена (u1.7,u1.4).", "outcome": "{{speaker:B}} подтвердил итог брони (r16,r17).",
	"overall_score": 63, "overall_score_label": "63 / 100",
	"strengths": ["Сотрудник представился (u1.2,r1,r2,r3)."], "work_on": ["Подтвердить номер клиента (рекомендация rec1).", "(u1.4)"],
	"items": [
		{"id": "u1.4", "kind": "question", "title": "Какая дата? (s4.1)", "status": "mostly_met", "score": 75, "weight": 1,
		 "answer_summary": "{{speaker:A}} назвала дату в s4.1.", "explanation": "Ответ полный, см. u1.7.",
		 "strengths": ["Ясно (u1.4)"], "gaps": [{"text": "Не уточнила время (s4.2)", "explanation": "Нужно для u1.7"}],
		 "improvement": "Включать вопрос об удобстве (rec3, rec4).",
		 "instruction_sources": ["3f1f0a5e-8d7c-4b1a-9a53-2f0f2d7f9c11"], "instruction_titles": ["Скрипт бронирования"],
		 "evidence": [{"speaker": "A", "quote": "Бронь на завтра (u1.4)"}, {"speaker": "speaker_1", "quote": "Хорошо"}]},
		{"id": "u1.7", "kind": "question", "title": "Подтверждение", "status": "minimally_met", "score": 25,
		 "explanation": "Частично.", "instruction_sources": ["3f1f0a5e-8d7c-4b1a-9a53-2f0f2d7f9c11"], "evidence": []},
		{"id": "r1", "kind": "requirement", "title": "Тариф S1 назван", "criterion_key": "8f0c7c2e-3c55-4f5a-9d0e-4e8b7f2a1c10",
		 "status": "weird_status", "explanation": "Не видно.", "evidence": []}
	],
	"recommendations": [
		{"id": "rec1", "title": "Назвать цену заранее (u1.7)", "action": "Назовите цену до брони (u1.7).", "reason": "Клиент спросил в сегменте s5.1 дважды.",
		 "expected_result": "Меньше вопросов.", "priority": "high", "priority_score": 88},
		{"id": "rec2", "title": "Уточнить время", "action": "Спросите время.", "reason": "", "expected_result": "", "priority": "unresolved"}
	]}`

func v3ReportData() ReportData {
	analysis := testAnalysis(uuid.New(), uuid.New())
	analysis.ResultJSON = json.RawMessage(rawV3Analysis)
	model := "openai/gpt-internal"
	analysis.Model = &model
	analysis.Provider = "openrouter"
	return ReportData{
		Call: testCall(uuid.New()), Analysis: analysis, GeneratedAt: time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC),
		SpeakerNames: map[string]string{"A": "Анна — администратор"},
	}
}

func TestV3ReportTextIsReadable(t *testing.T) {
	data := v3ReportData()
	md := string(generateMarkdownReport(data))

	for _, want := range []string{
		"Бронь оформлена и подтверждена.",
		"**Результат разговора:** Спикер B подтвердил итог брони.",
		"- Сотрудник представился.",
		"- Подтвердить номер клиента.",
		"## Какая дата?",
		"**Статус:** В основном выполнено",
		"**Ответ или действие:** Анна — администратор назвала дату.",
		"**Разбор:** Ответ полный, см. «Подтверждение».",
		"- Ясно",
		"- Не уточнила время: Нужно для «Подтверждение»",
		"**Эталонный ответ или совет:** Включать вопрос об удобстве.",
		"- Скрипт бронирования",
		"- Анна — администратор: Бронь на завтра (u1.4)",
		"- Спикер 2: Хорошо",
		"## Подтверждение",
		"**Статус:** Минимально выполнено",
		"- Инструкция звонка",
		"## Тариф S1 назван",
		"**Статус:** Не указано",
		"**Назвать цену заранее:** Назовите цену до брони. Клиент спросил дважды. Меньше вопросов. [приоритет: высокий · 88/100]",
		"**Уточнить время:** Спросите время. [приоритет: не определён]",
	} {
		require.Contains(t, md, want)
	}
	// Quotes are what was said; everything else carries no IDs or codes.
	withoutQuotes := strings.ReplaceAll(md, "Бронь на завтра (u1.4)", "")
	for _, leaked := range []string{
		"u1.", "r16", "rec1", "rec3", "s4.1", "s5.1", "{{speaker", "3f1f0a5e", "mostly_met", "minimally_met", "weird_status", "high", "unresolved",
		"ID звонка", "ID анализа", "Провайдер", "Модель", "openai/gpt-internal", "openrouter", data.Call.ID.String(), data.Analysis.ID.String(),
	} {
		require.NotContains(t, withoutQuotes, leaked)
	}
}

func TestV3ReportHeadersLeaveOutServerDetails(t *testing.T) {
	data := v3ReportData()
	internal := []string{"ID звонка", "ID анализа", "Провайдер", "Модель", "openrouter", data.Call.ID.String(), data.Analysis.ID.String()}

	document, err := generateDOCXReport(data)
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(document), int64(len(document)))
	require.NoError(t, err)
	for _, part := range archive.File {
		if part.Name != "word/document.xml" {
			continue
		}
		reader, err := part.Open()
		require.NoError(t, err)
		xml, err := io.ReadAll(reader)
		require.NoError(t, reader.Close())
		require.NoError(t, err)
		require.Contains(t, string(xml), "Статус анализа")
		require.Contains(t, string(xml), "Скрипт бронирования")
		for _, value := range internal {
			require.NotContains(t, string(xml), value)
		}
	}

	sheet, err := generateXLSXReport(data)
	require.NoError(t, err)
	workbook, err := excelize.OpenReader(bytes.NewReader(sheet))
	require.NoError(t, err)
	defer func() { require.NoError(t, workbook.Close()) }()
	meta, err := workbook.GetRows("Метаданные")
	require.NoError(t, err)
	require.Equal(t, []string{"Статус анализа", string(models.CallAnalysisStatusDone)}, meta[len(meta)-1])
	cards, err := workbook.GetRows("Карточки")
	require.NoError(t, err)
	require.Equal(t, []string{"Вопрос", "Какая дата?", "В основном выполнено", "75", "1", "Ответ полный, см. «Подтверждение».", "Анна — администратор: Бронь на завтра (u1.4)\nСпикер 2: Хорошо"}, cards[1])
	for _, name := range workbook.GetSheetList() {
		rows, err := workbook.GetRows(name)
		require.NoError(t, err)
		for _, row := range rows {
			for _, cell := range row {
				for _, value := range internal {
					require.NotContains(t, cell, value)
				}
			}
		}
	}

	pdf, err := generatePDFReport(data)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
}

type namedSpeakersRepository struct{ reportTranscriptRepository }

func (namedSpeakersRepository) GetReportSpeakerNames(context.Context, uuid.UUID) (map[string]string, error) {
	return map[string]string{"A": "Анна — администратор"}, nil
}

func TestCreateNamesSpeakersOfTheAnalysis(t *testing.T) {
	callID := uuid.New()
	analysis := testAnalysis(callID, uuid.New())
	analysis.ResultJSON = json.RawMessage(rawV3Analysis)
	storage := &fakeReportStorage{}
	svc := NewService(&fakeCallRepository{call: testCall(callID)}, &fakeAnalysisRepository{analysis: analysis}, &namedSpeakersRepository{}, &fakeReportRepository{}, storage)
	_, err := svc.Create(context.Background(), models.CreateReportInput{CallUUID: callID, UserUUID: uuid.New(), Format: models.ReportFormatMD})
	require.NoError(t, err)
	require.Contains(t, storage.content, "**Ответ или действие:** Анна — администратор назвала дату.")
	require.Contains(t, storage.content, "- Анна — администратор: Бронь на завтра")
}

func TestStatusAndPriorityLabelsNeverShowCodes(t *testing.T) {
	statuses := map[string]string{
		"met": "Выполнено", "mostly_met": "В основном выполнено", "partially_met": "Частично выполнено",
		"minimally_met": "Минимально выполнено", "missed": "Не выполнено", "not_applicable": "Не применимо",
		"unclear": "Неясно", "conflict": "Конфликт требований", "not_assessed": "Не оценено", "": "Не указано", "new_code": "Не указано",
	}
	for status, want := range statuses {
		t.Run("status "+status, func(t *testing.T) { require.Equal(t, want, criterionStatusLabel(status)) })
	}
	priorities := map[string]string{"high": "высокий", "medium": "средний", "low": "низкий", "unresolved": "не определён", "": "не указан", "urgent": "не указан"}
	for priority, want := range priorities {
		t.Run("priority "+priority, func(t *testing.T) { require.Equal(t, want, priorityLabel(priority)) })
	}
}

func TestInstructionBasisNamesInstructions(t *testing.T) {
	cases := []struct {
		name string
		item universalItem
		want []string
	}{
		{name: "titles", item: universalItem{InstructionSources: []string{"id-1", "id-2"}, InstructionTitles: []string{"Скрипт", " Прайс "}}, want: []string{"Скрипт", "Прайс"}},
		{name: "only IDs", item: universalItem{InstructionSources: []string{"id-1"}}, want: []string{"Инструкция звонка"}},
		{name: "none", item: universalItem{}, want: []string{"Не указано"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, instructionBasis(tc.item)) })
	}
}
