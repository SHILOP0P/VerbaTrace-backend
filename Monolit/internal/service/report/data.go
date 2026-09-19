package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
)

type ReportData struct {
	TranscriptionOnly     bool
	TranscriptionRevision int
	Call                  models.Call
	Analysis              models.CallAnalysis
	TranscriptionText     string
	GeneratedAt           time.Time
	// SpeakerNames are the names given to transcript speakers by key; the
	// analysis refers to speakers by key.
	SpeakerNames map[string]string
}

func (d ReportData) AnalysisJSONText() string {
	if len(d.Analysis.ResultJSON) == 0 {
		return ""
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, d.Analysis.ResultJSON, "", "  "); err != nil {
		return string(d.Analysis.ResultJSON)
	}

	return pretty.String()
}

func (d ReportData) AnalysisText() string {
	if d.Analysis.ResultText != nil && strings.TrimSpace(*d.Analysis.ResultText) != "" {
		return strings.TrimSpace(*d.Analysis.ResultText)
	}

	return d.AnalysisJSONText()
}

func (d ReportData) Title() string {
	if d.TranscriptionOnly {
		return "Транскрипция звонка: " + d.Call.Title
	}
	return "Отчет по звонку: " + d.Call.Title
}
