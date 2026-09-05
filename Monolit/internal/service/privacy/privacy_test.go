package privacy

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestExistingMaskOperationsRejectedBeforeStorage(t *testing.T) {
	s := &Service{}
	for _, operation := range []string{"change_category", "remove_mask"} {
		input := CorrectionInput{Operation: operation, ExpectedRevision: 1, Reason: "Проверка неизменности"}
		if _, err := s.PreviewCorrection(context.Background(), models.Call{}, uuid.New(), input); err != ErrCorrectionInvalid {
			t.Fatalf("preview accepted %s: %v", operation, err)
		}
		if _, err := s.ApplyCorrection(context.Background(), models.Call{}, uuid.New(), input); err != ErrCorrectionInvalid {
			t.Fatalf("apply accepted %s: %v", operation, err)
		}
	}
}

func TestCanonicalPrivacyConfigDefaultsExcludeMoney(t *testing.T) {
	config := models.DefaultPrivacyPolicyConfig()
	config.Enabled = true
	canonical, _, err := CanonicalPrivacyConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range canonical.EntityTypes {
		if entity == "money_amount" {
			t.Fatal("money must not be enabled by default")
		}
	}
}

func TestCanonicalPrivacyConfigRejectsUnsafeRestrictedMediaCombination(t *testing.T) {
	config := models.DefaultPrivacyPolicyConfig()
	config.Enabled = true
	config.OriginalMediaAccess = "uploader_only"
	config.SanitizedMedia = "off"
	if _, _, err := CanonicalPrivacyConfig(config); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestPrivacyPolicyCandidatesPreferDepartmentAndFallBackToCompany(t *testing.T) {
	departmentID := uuid.New()
	companyID := uuid.New()
	candidates := privacyPolicyCandidates(models.Call{
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
	})
	if len(candidates) != 2 || candidates[0].Scope != models.PrivacyScopeDepartment || candidates[0].ID != departmentID || candidates[1].Scope != models.PrivacyScopeCompany || candidates[1].ID != companyID {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestPrivacyPolicyCandidatesUsePersonalOnlyOutsideCompany(t *testing.T) {
	userID := uuid.New()
	candidates := privacyPolicyCandidates(models.Call{UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true}})
	if len(candidates) != 1 || candidates[0].Scope != models.PrivacyScopePersonal || candidates[0].ID != userID {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestCanonicalIntervalsMergesAndPads(t *testing.T) {
	spans := []models.RedactionSpan{{StartSeconds: 1, EndSeconds: 2}, {StartSeconds: 2.05, EndSeconds: 3}}
	got := canonicalIntervals(spans, 10)
	if len(got) != 1 || got[0].Start != .92 || got[0].End != 3.08 {
		t.Fatalf("intervals=%+v", got)
	}
}

func TestAudioFilterKeepsTimelineAndUsesSignal(t *testing.T) {
	filter := audioFilter([]interval{{Start: 1, End: 2}}, 5)
	if !strings.Contains(filter, "sine=") || !strings.Contains(filter, "amix=") || !strings.Contains(filter, "between(t\\,1.000\\,2.000)") {
		t.Fatalf("filter=%s", filter)
	}
}

func TestApplyCorrectionAddChangeAndRemove(t *testing.T) {
	actor := uuid.New()
	words := []models.TranscriptionWord{{Text: "Меня", StartSeconds: 0, EndSeconds: .2}, {Text: "зовут", StartSeconds: .2, EndSeconds: .4}, {Text: "Анна", StartSeconds: .4, EndSeconds: .7}, {Text: "Иванова", StartSeconds: .7, EndSeconds: 1}}
	start, end := 2, 3
	masked, spans, _, err := applyCorrection(words, nil, CorrectionInput{Operation: "add_mask", WordStartIndex: &start, WordEndIndex: &end, EntityType: "person_name"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(masked) != 3 || masked[2].Text != "[ИМЯ]" || len(spans) != 1 || spans[0].EndSeconds != 1 {
		t.Fatalf("masked=%+v spans=%+v", masked, spans)
	}
	spans[0].ID = uuid.New()
	spanID := spans[0].ID
	for _, operation := range []string{"change_category", "remove_mask"} {
		_, _, _, err = applyCorrection(masked, spans, CorrectionInput{Operation: operation, SpanID: &spanID, EntityType: "phone_number", ReplacementText: "Анна Иванова"}, actor)
		if err != ErrCorrectionInvalid || masked[2].Text != "[ИМЯ]" || len(spans) != 1 {
			t.Fatalf("existing mask changed by %s: %v", operation, err)
		}
	}
}
