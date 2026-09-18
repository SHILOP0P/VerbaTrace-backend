package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestXLSXListsSchemaV3Cards(t *testing.T) {
	analysis := testAnalysis(uuid.New(), uuid.New())
	analysis.ResultJSON = json.RawMessage(`{
		"schema_version": 3, "summary": "Итог", "overall_score": 63, "overall_score_label": "63 / 100",
		"items": [
			{"kind": "requirement", "title": "Выяснил бюджет", "status": "missed", "score": 0, "weight": 2,
			 "explanation": "Бюджет не обсуждали", "evidence": [{"speaker": "A", "quote": "Когда удобно?"}]},
			{"kind": "question", "title": "Какой срок?", "status": "met", "score": 100, "weight": 1, "explanation": "Ответ полный"}
		],
		"recommendations": []}`)

	sheet, err := generateXLSXReport(ReportData{Call: testCall(uuid.New()), Analysis: analysis})
	require.NoError(t, err)
	workbook, err := excelize.OpenReader(bytes.NewReader(sheet))
	require.NoError(t, err)
	defer func() { require.NoError(t, workbook.Close()) }()
	require.Contains(t, workbook.GetSheetList(), "Карточки")

	rows, err := workbook.GetRows("Карточки")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, []string{"Требование", "Выяснил бюджет", criterionStatusLabel("missed"), "0", "2", "Бюджет не обсуждали", "A: Когда удобно?"}, rows[1])
	require.Equal(t, "Вопрос", rows[2][0])
	require.Equal(t, "100", rows[2][3])
}
