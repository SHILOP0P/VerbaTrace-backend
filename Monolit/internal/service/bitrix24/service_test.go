package bitrix24

import (
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestHasTaskScopeUsesOfficialBitrixScope(t *testing.T) {
	if !hasTaskScope([]string{"telephony", "user", "task"}) {
		t.Fatal("official Bitrix24 task scope must enable task writes")
	}
	if hasTaskScope([]string{"telephony", "user", "tasks"}) {
		t.Fatal("nonexistent tasks scope must not enable task writes")
	}
}

func TestWithAuthRestrictsRecordingToPortal(t *testing.T) {
	value, err := withAuth("https://company.bitrix24.ru/recording/42?download=1", "company.bitrix24.ru", "secret")
	if err != nil {
		t.Fatalf("expected same-host URL to pass: %v", err)
	}
	if value != "https://company.bitrix24.ru/recording/42?auth=secret&download=1" {
		t.Fatalf("unexpected authorized URL: %s", value)
	}
	cdnURL := "https://storage-gw-com-02.voximplant.com/recording/42?sessionid=signed"
	value, err = withAuth(cdnURL, "company.bitrix24.ru", "secret")
	if err != nil {
		t.Fatalf("expected official recording host to pass: %v", err)
	}
	if value != cdnURL {
		t.Fatalf("credential must not be added to recording CDN URL: %s", value)
	}
	for _, raw := range []string{
		"http://company.bitrix24.ru/recording/42",
		"https://attacker.example/recording/42",
		"https://company.bitrix24.ru:444/recording/42",
		"https://user@company.bitrix24.ru/recording/42",
	} {
		if _, err = withAuth(raw, "company.bitrix24.ru", "secret"); err == nil {
			t.Fatalf("expected URL to be rejected: %s", raw)
		}
	}
}

func TestNormalizeMappingChangesCanonicalAndStrict(t *testing.T) {
	internalID, departmentID := uuid.New(), uuid.New()
	changes := []models.BitrixMappingChange{
		{ExternalUserID: " 20 ", Status: "ignored", ExpectedLockVersion: 2},
		{ExternalUserID: "10", Status: "mapped", InternalUserID: uuid.NullUUID{UUID: internalID, Valid: true}, DepartmentID: uuid.NullUUID{UUID: departmentID, Valid: true}, ExpectedLockVersion: 1},
		{ExternalUserID: "15", Status: "mapped", InternalUserID: uuid.NullUUID{UUID: internalID, Valid: true}, ExpectedLockVersion: 1},
	}
	normalized, err := normalizeMappingChanges(changes)
	if err != nil {
		t.Fatalf("normalize valid changes: %v", err)
	}
	if normalized[0].ExternalUserID != "10" || normalized[1].ExternalUserID != "15" || normalized[2].ExternalUserID != "20" {
		t.Fatalf("changes are not canonical: %#v", normalized)
	}
	invalid := [][]models.BitrixMappingChange{
		{{ExternalUserID: "7", Status: "ignored"}, {ExternalUserID: " 7 ", Status: "unmapped"}},
		{{ExternalUserID: "8", Status: "mapped"}},
		{{ExternalUserID: "9", Status: "ignored", InternalUserID: uuid.NullUUID{UUID: internalID, Valid: true}}},
		{{ExternalUserID: "", Status: "unmapped"}},
	}
	for _, input := range invalid {
		if _, err = normalizeMappingChanges(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %#v, got %v", input, err)
		}
	}
}

func TestTaskReconciliationNormalizationAndLinkSafety(t *testing.T) {
	if got := externalStatusCodeForAction("cancelled"); got != "6" {
		t.Fatalf("cancelled action must use a valid classic Bitrix24 task status, got %s", got)
	}
	if got := normalizeExternalTaskStatus("5"); got != "completed" {
		t.Fatalf("unexpected status: %s", got)
	}
	if got := actionStatusForExternal("in_progress", "open", time.Now().Add(time.Hour), time.Now()); got != "in_progress" {
		t.Fatalf("unexpected action status: %s", got)
	}
	if got := actionStatusForExternal("pending", "open", time.Now().Add(-time.Hour), time.Now()); got != "overdue" {
		t.Fatalf("overdue task was not recognized: %s", got)
	}
	if link, ok := safeBitrixTaskLink("https://portal.bitrix24.ru/company/personal/user/1/tasks/task/view/42/", "portal.bitrix24.ru"); !ok || link == "" {
		t.Fatal("same-portal HTTPS task link must be accepted")
	}
	for _, raw := range []string{"http://portal.bitrix24.ru/task/42", "https://evil.example/task/42", "https://user@portal.bitrix24.ru/task/42", "https://portal.bitrix24.ru:444/task/42"} {
		if _, ok := safeBitrixTaskLink(raw, "portal.bitrix24.ru"); ok {
			t.Fatalf("unsafe task link accepted: %s", raw)
		}
	}
}

func TestNormalizeBackfillRange(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	to := from.Add(7 * 24 * time.Hour)
	normalizedFrom, normalizedTo, err := normalizeBackfillRange(from, to)
	if err != nil {
		t.Fatalf("expected valid range: %v", err)
	}
	if normalizedFrom.Location() != time.UTC || normalizedTo.Location() != time.UTC {
		t.Fatal("backfill range must be normalized to UTC")
	}
	for _, invalidTo := range []time.Time{from, from.Add(-time.Second), from.Add(maxBackfillRange + time.Second)} {
		if _, _, err = normalizeBackfillRange(from, invalidTo); err == nil {
			t.Fatalf("expected invalid range to be rejected: %s - %s", from, invalidTo)
		}
	}
}

func TestExtractTaskID(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{map[string]any{"task": map[string]any{"id": "17"}}, "17"},
		{map[string]any{"id": float64(18)}, "18"},
		{"19", "19"},
	}
	for _, item := range cases {
		if got := extractTaskID(item.value); got != item.want {
			t.Fatalf("extractTaskID(%v)=%q, want %q", item.value, got, item.want)
		}
	}
}

func TestOAuthCallbackOrigin(t *testing.T) {
	service := &Service{config: Config{PublicBaseURL: "https://app.verbatrace.example/path"}}
	if got := service.OAuthCallbackOrigin(); got != "https://app.verbatrace.example" {
		t.Fatalf("unexpected origin %q", got)
	}
	service.config.PublicBaseURL = "javascript:alert(1)"
	if got := service.OAuthCallbackOrigin(); got != "" {
		t.Fatalf("unsafe origin was accepted: %q", got)
	}
}
