package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"
)

type ReportData struct {
	Call              models.Call
	Analysis          models.CallAnalysis
	TranscriptionText string
	GeneratedAt       time.Time
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
