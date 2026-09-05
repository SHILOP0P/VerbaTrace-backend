//go:build live

package assemblyai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
)

// TestLiveRussianPrivacyRedaction is an explicit paid acceptance test. It is
// excluded from ordinary CI and always deletes the provider-side transcript.
func TestLiveRussianPrivacyRedaction(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("ASSEMBLYAI_API_KEY"))
	audioPath := strings.TrimSpace(os.Getenv("ASSEMBLYAI_LIVE_AUDIO"))
	if apiKey == "" || audioPath == "" {
		t.Skip("live AssemblyAI credentials or audio are not configured")
	}
	file, err := os.Open(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := New(apiKey, true, false)
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Timeout = 2 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := provider.TranscribeRequest(ctx, models.TranscriptionRequest{File: models.File{Content: file, OriginalFilename: info.Name(), MimeType: "audio/wav", SizeBytes: info.Size()}, Mode: models.TranscriptionModeDiarized, Privacy: &models.TranscriptionPrivacyRequest{MarkerContract: models.PrivacyMarkerContractRUv1, EntityTypes: []string{"person_name", "phone_number", "email_address"}}})
	if result.ProviderJobID != "" {
		defer func() {
			if deleteErr := provider.DeleteArtifact(context.Background(), result.ProviderJobID); deleteErr != nil {
				t.Errorf("delete provider artifact: %v", deleteErr)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderJobID == "" {
		t.Fatal("provider job id is missing")
	}
	if len(result.RedactionSpans) == 0 {
		t.Fatalf("provider returned no PII markers: %q", result.Text)
	}
	for _, span := range result.RedactionSpans {
		if !strings.HasPrefix(span.Marker, "[") || strings.ContainsAny(span.Marker, "abcdefghijklmnopqrstuvwxyz") {
			t.Fatalf("marker is not the Russian contract: %q", span.Marker)
		}
	}
	if strings.Contains(strings.ToLower(result.Text), "anna.ivanova") || strings.Contains(result.Text, "Анна Иванова") {
		t.Fatalf("redacted result contains the original test identity: %q", result.Text)
	}
	t.Logf("language=%v spans=%d redacted_text=%q", result.Language, len(result.RedactionSpans), result.Text)
}
