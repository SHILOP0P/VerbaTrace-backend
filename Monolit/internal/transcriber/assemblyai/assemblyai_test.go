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
