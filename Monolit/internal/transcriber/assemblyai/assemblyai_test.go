package assemblyai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"calllens/monolit/internal/models"
)

func TestTranscribeUploadsDiarizesAndIdentifiesRoles(t *testing.T) {
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
			if request.SpeechUnderstanding == nil || request.SpeechUnderstanding.Request.SpeakerIdentification.SpeakerType != "role" {
				t.Fatalf("speaker identification = %+v", request.SpeechUnderstanding)
			}
			_, _ = w.Write([]byte(`{"id":"transcript-id","status":"queued"}`))
		case "/v2/transcript/transcript-id":
			_, _ = w.Write([]byte(`{"id":"transcript-id","status":"completed","text":"hello world","language_code":"ru","utterances":[{"speaker":"Менеджер","start":1000,"end":2500,"text":"hello world"}]}`))
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

	result, err := transcriber.Transcribe(context.Background(), models.File{Content: io.NopCloser(strings.NewReader("audio bytes"))})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "hello world" || result.Language == nil || *result.Language != "ru" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Segments) != 1 || result.Segments[0].Speaker != "Менеджер" || *result.Segments[0].StartSeconds != 1 || *result.Segments[0].EndSeconds != 2.5 {
		t.Fatalf("segments = %+v", result.Segments)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(" ", false, false); err == nil {
		t.Fatal("expected an API key error")
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
	if _, err := transcriber.createTranscript(context.Background(), "https://upload.example/audio"); err != nil {
		t.Fatalf("create transcript: %v", err)
	}
}
