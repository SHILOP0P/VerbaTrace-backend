package billing

import (
	"testing"

	"calllens/monolit/internal/models"
)

func TestTranscriptionModeForPlan(t *testing.T) {
	tests := []struct {
		code models.PlanCode
		kind models.PlanType
		want models.TranscriptionMode
	}{
		{models.PlanCodePersonalStart, models.PlanTypePersonal, models.TranscriptionModeStandard},
		{models.PlanCodePersonalPlus, models.PlanTypePersonal, models.TranscriptionModeDiarized},
		{models.PlanCodePersonalPro, models.PlanTypePersonal, models.TranscriptionModeIdentified},
		{models.PlanCodeBusinessStart, models.PlanTypeBusiness, models.TranscriptionModeDiarized},
		{models.PlanCodeBusinessPlus, models.PlanTypeBusiness, models.TranscriptionModeIdentified},
		{models.PlanCodeBusinessPro, models.PlanTypeBusiness, models.TranscriptionModeIdentified},
	}
	for _, test := range tests {
		if got := transcriptionModeForPlan(models.Plan{Code: test.code, Type: test.kind}); got != test.want {
			t.Fatalf("plan %s: mode = %s, want %s", test.code, got, test.want)
		}
	}
}
