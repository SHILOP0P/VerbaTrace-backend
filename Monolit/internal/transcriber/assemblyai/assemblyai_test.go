package assemblyai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestTranscribeUploadsDiarizesAndIdentifiesCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "test-key" {
			t.Fatalf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v2/upload":
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != "audio bytes" {
				t.Fatalf("upload body = %q, %v", body, err)
			}
			_, _ = w.Write([]byte(`{"upload_url":"https://upload.example/audio"}`))
		case "/v2/transcript":
			var request transcriptRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode transcript request: %v", err)
			}
			if request.AudioURL == "" || !request.SpeakerLabels || !request.LanguageDetection {
				t.Fatalf("unexpected transcript request: %+v", request)
			}
			if request.SpeechUnderstanding == nil {
				t.Fatalf("speaker identification is missing")
			}
			identification := request.SpeechUnderstanding.Request.SpeakerIdentification
			if identification.SpeakerType != "name" || len(identification.Speakers) != 3 || identification.Speakers[0].Name != "Анна Иванова" || identification.Speakers[2].Name != "Председатель комиссии" {
				t.Fatalf("speaker identification = %+v", request.SpeechUnderstanding)
			}
			_, _ = w.Write([]byte(`{"id":"transcript-id","status":"queued"}`))
		case "/v2/transcript/transcript-id":
			_, _ = w.Write([]byte(`{"id":"transcript-id","status":"completed","text":"hello world","language_code":"ru","words":[{"text":"hello","start":1000,"end":1500,"confidence":0.99},{"text":"world","start":1600,"end":2500,"confidence":0.98}],"utterances":[{"speaker":"Менеджер","start":1000,"end":2500,"text":"hello world"}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	transcriber, err := New("test-key", true, true)
	if err != nil {
		t.Fatal(err)
	}
	transcriber.baseURL = server.URL
	transcriber.client = server.Client()

	result, err := transcriber.Transcribe(context.Background(), models.File{Content: io.NopCloser(strings.NewReader("audio bytes")), SpeakerCandidates: []models.SpeakerCandidate{
		{Label: "Анна Иванова", Kind: "person"}, {Label: "Петр Смирнов", Kind: "person"}, {Label: "Председатель комиссии", Kind: "role", Description: "Объявляет решение"},
	}})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "hello world" || result.Language == nil || *result.Language != "ru" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Segments) != 1 || result.Segments[0].Speaker != "Менеджер" || *result.Segments[0].StartSeconds != 1 || *result.Segments[0].EndSeconds != 2.5 {
		t.Fatalf("segments = %+v", result.Segments)
	}
	if len(result.Words) != 2 || result.Words[0].Text != "hello" || result.Words[0].StartSeconds != 1 || result.Words[1].EndSeconds != 2.5 {
		t.Fatalf("words = %+v", result.Words)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(" ", false, false); err == nil {
		t.Fatal("expected an API key error")
	}
}

func TestSpeakerIdentificationRequestUsesRoleTypeForRoleOnlyCandidates(t *testing.T) {
	request := speakerIdentificationRequest([]models.SpeakerCandidate{
		{Label: "Интервьюер", Description: "Задаёт вопросы", Kind: "role"},
		{Label: "Кандидат", Description: "Отвечает на вопросы", Kind: "role"},
	})
	identification := request.Request.SpeakerIdentification
	if identification.SpeakerType != "role" || len(identification.Speakers) != 2 || identification.Speakers[0].Role != "Интервьюер" || identification.Speakers[0].Name != "" {
		t.Fatalf("unexpected role identification: %+v", identification)
	}
}

func TestStandardTranscriptDisablesSpeakerLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request transcriptRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.SpeakerLabels || request.SpeechUnderstanding != nil {
			t.Fatalf("standard request unexpectedly enables speaker processing: %+v", request)
		}
		_, _ = w.Write([]byte(`{"id":"transcript-id"}`))
	}))
	defer server.Close()

	transcriber, err := New("test-key", false, false)
	if err != nil {
		t.Fatal(err)
	}
	transcriber.baseURL = server.URL
	transcriber.client = server.Client()
	if _, err := transcriber.createTranscript(context.Background(), "https://upload.example/audio", nil); err != nil {
		t.Fatalf("create transcript: %v", err)
	}
}

func TestIdentifiedTranscriptWithoutCandidatesFallsBackToDiarization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var request transcriptRequest
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if !request.SpeakerLabels {
			t.Fatal("identified mode must keep speaker diarization enabled")
		}
		if request.SpeechUnderstanding != nil {
			t.Fatalf("speaker identification must be omitted without candidates: %s", body)
		}
		_, _ = w.Write([]byte(`{"id":"transcript-id"}`))
	}))
	defer server.Close()

	transcriber, err := New("test-key", true, true)
	if err != nil {
		t.Fatal(err)
	}
	transcriber.baseURL = server.URL
	transcriber.client = server.Client()
	if _, err := transcriber.createTranscript(context.Background(), "https://upload.example/audio", nil); err != nil {
		t.Fatalf("create transcript: %v", err)
	}
}

func TestPrivacyTranscriptRequestIsStrictAndUsesEntityMarkers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request transcriptRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.RedactPII == nil || !*request.RedactPII || request.RedactPIISub != "entity_name" {
			t.Fatalf("privacy flags = %+v", request)
		}
		if request.RedactPIIReturnUnredacted == nil || *request.RedactPIIReturnUnredacted {
			t.Fatal("unredacted transcript must not be returned")
		}
		want := []string{"email_address", "location_address", "location_address_street", "location_zip", "person_name"}
		if strings.Join(request.RedactPIIPolicies, ",") != strings.Join(want, ",") {
			t.Fatalf("policies = %v, want %v", request.RedactPIIPolicies, want)
		}
		if request.SpeechUnderstanding != nil {
			t.Fatal("speaker identification must be disabled for privacy request")
		}
		_, _ = w.Write([]byte(`{"id":"privacy-id"}`))
	}))
	defer server.Close()
	transcriber, _ := New("test-key", true, true)
	transcriber.baseURL = server.URL
	transcriber.client = server.Client()
	_, err := transcriber.createTranscriptWithPrivacy(context.Background(), "https://upload.example/audio", []models.SpeakerCandidate{{Label: "Анна", Kind: "person"}}, &models.TranscriptionPrivacyRequest{MarkerContract: "ru-v1", EntityTypes: []string{"person_name", "address", "email_address"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNormalizePrivacyResultProducesRussianMarkersAndSpans(t *testing.T) {
	start, end := 1.0, 2.0
	result, err := normalizePrivacyResult(models.TranscriptionResult{Text: "Меня зовут [PERSON_NAME]", Segments: []models.TranscriptionSegment{{Text: "Меня зовут [PERSON_NAME]", StartSeconds: &start, EndSeconds: &end}}, Words: []models.TranscriptionWord{{Text: "Меня", StartSeconds: 1, EndSeconds: 1.2}, {Text: "зовут", StartSeconds: 1.2, EndSeconds: 1.5}, {Text: "[PERSON_NAME]", StartSeconds: 1.5, EndSeconds: 2}}}, &models.TranscriptionPrivacyRequest{MarkerContract: "ru-v1", EntityTypes: []string{"person_name"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Меня зовут [ИМЯ]" || result.Words[2].Text != "[ИМЯ]" || len(result.RedactionSpans) != 1 {
		t.Fatalf("result=%+v spans=%+v", result, result.RedactionSpans)
	}
	span := result.RedactionSpans[0]
	if span.EntityType != "person_name" || span.WordStartIndex != 2 || span.StartSeconds != 1.5 {
		t.Fatalf("span=%+v", span)
	}
}

func TestNormalizePrivacyResultAllowsPunctuationButRejectsEmbeddedMarker(t *testing.T) {
	request := &models.TranscriptionPrivacyRequest{MarkerContract: "ru-v1", EntityTypes: []string{"person_name"}}
	result, err := normalizePrivacyResult(models.TranscriptionResult{Text: "[PERSON_NAME].", Words: []models.TranscriptionWord{{Text: "[PERSON_NAME].", StartSeconds: 0, EndSeconds: 1}}}, request)
	if err != nil || result.Words[0].Text != "[ИМЯ]." || len(result.RedactionSpans) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err = normalizePrivacyResult(models.TranscriptionResult{Text: "x[PERSON_NAME]", Words: []models.TranscriptionWord{{Text: "x[PERSON_NAME]", StartSeconds: 0, EndSeconds: 1}}}, request); err == nil {
		t.Fatal("embedded marker must be rejected")
	}
}

func TestDeleteArtifactTreatsNotFoundAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/transcript/job-id" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	transcriber, _ := New("test-key", false, false)
	transcriber.baseURL = server.URL
	transcriber.client = server.Client()
	if err := transcriber.DeleteArtifact(context.Background(), "job-id"); err != nil {
		t.Fatal(err)
	}
}
