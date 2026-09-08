package report

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"verbatrace/monolit/internal/models"
)

type reportTranscriptRepository struct {
	fakeTranscriptionRepository
	requested int
}

func (r *reportTranscriptRepository) GetReportTranscription(_ context.Context, _ uuid.UUID, revision int) (models.Transcription, int, error) {
	r.requested = revision
	if revision == 99 {
		return models.Transcription{}, 0, models.ErrTranscriptionNotFound
	}
	text := "Выбранная версия разговора"
	return models.Transcription{Text: &text}, revision, nil
}

func TestTranscriptExportWithoutAnalysisOrExportPlan(t *testing.T) {
	repo := &reportTranscriptRepository{}
	storage := &fakeReportStorage{}
	svc := NewService(&fakeCallRepository{call: testCall(uuid.New())}, nil, repo, &fakeReportRepository{}, storage)
	svc.SetBillingLimiter(&fakeBillingLimiter{subscription: models.Subscription{Plan: models.Plan{ExportEnabled: false}}})
	report, err := svc.Create(context.Background(), models.CreateReportInput{CallUUID: uuid.New(), UserUUID: uuid.New(), Format: models.ReportFormatMD, Content: "transcription", TranscriptionRevision: 2})
	require.NoError(t, err)
	require.Equal(t, 2, repo.requested)
	require.Equal(t, 2, report.TranscriptionRevision)
	require.Equal(t, uuid.Nil, report.AnalysisUUID)
	require.Contains(t, storage.content, "Выбранная версия разговора")
	require.NotContains(t, storage.content, "## Анализ")
	require.NotContains(t, storage.content, "Качество менеджера")
	_, err = svc.Create(context.Background(), models.CreateReportInput{CallUUID: uuid.New(), UserUUID: uuid.New(), Format: models.ReportFormatMD, Content: "transcription", TranscriptionRevision: 99})
	require.ErrorIs(t, err, models.ErrTranscriptionNotFound)
}

func TestTranscriptFormatsExcludeExistingAnalysis(t *testing.T) {
	data := ReportData{Call: testCall(uuid.New()), Analysis: testAnalysis(uuid.New(), uuid.New()), TranscriptionOnly: true, TranscriptionRevision: 3, TranscriptionText: "[00:12] Оператор: Добрый день!\n\n[00:14] Клиент: Здравствуйте."}
	doc, err := generateDOCXReport(data)
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(doc), int64(len(doc)))
	require.NoError(t, err)
	for _, part := range archive.File {
		if part.Name == "word/document.xml" {
			reader, openErr := part.Open()
			require.NoError(t, openErr)
			xml, readErr := io.ReadAll(reader)
			require.NoError(t, reader.Close())
			require.NoError(t, readErr)
			require.Contains(t, string(xml), "версия 3")
			require.NotContains(t, string(xml), "ID анализа")
			require.Contains(t, string(xml), "Оператор: Добрый день!")
		}
	}
	sheet, err := generateXLSXReport(data)
	require.NoError(t, err)
	workbook, err := excelize.OpenReader(bytes.NewReader(sheet))
	require.NoError(t, err)
	defer func() { require.NoError(t, workbook.Close()) }()
	require.Equal(t, []string{"Метаданные", "Транскрипция"}, workbook.GetSheetList())
	meta, err := workbook.GetRows("Метаданные")
	require.NoError(t, err)
	for _, row := range meta {
		for _, cell := range row {
			require.NotContains(t, cell, "анализа")
		}
	}
	pdf, err := generatePDFReport(data)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
}

func TestTranscriptPresentationMatchesTariffData(t *testing.T) {
	original := "Устаревший сплошной текст"
	words := []models.TranscriptionWord{{Text: "Добрый", Speaker: "speaker_0", StartSeconds: 12, EndSeconds: 13}, {Text: "день", Speaker: "speaker_0", StartSeconds: 13, EndSeconds: 14}, {Text: "!", Speaker: "speaker_0", StartSeconds: 14, EndSeconds: 14}, {Text: "Здравствуйте.", Speaker: "speaker_1", StartSeconds: 15, EndSeconds: 16}}
	transcript := models.Transcription{Text: &original, Words: words}
	result := formatTranscriptWithSpeakers(transcript, map[string]string{"speaker_0": "Анна — менеджер"})
	require.Contains(t, result, "Анна — менеджер · 00:12 – 00:14\nДобрый день!")
	require.Contains(t, result, "Спикер 2 · 00:15 – 00:16\nЗдравствуйте.")
	require.NotContains(t, result, original)
	for index := range words {
		words[index].Speaker = ""
	}
	require.Equal(t, "Добрый день! Здравствуйте.", formatTranscript(transcript))
	transcript.Words = nil
	require.Equal(t, original, formatTranscript(transcript))
}
