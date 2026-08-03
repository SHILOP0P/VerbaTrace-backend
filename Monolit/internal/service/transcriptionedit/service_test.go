package transcriptionedit

import (
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestRenderWordsPreservesPunctuation(t *testing.T) {
	words := []models.TranscriptionWord{{Text: "Здравствуйте"}, {Text: ","}, {Text: "Дмитрий"}, {Text: "!"}}
	if got, want := renderWords(words), "Здравствуйте, Дмитрий!"; got != want {
		t.Fatalf("renderWords=%q want %q", got, want)
	}
}

func TestRenderSegmentsGroupsAdjacentSpeakers(t *testing.T) {
	words := []models.TranscriptionWord{{Text: "Добрый", Speaker: "manager", StartSeconds: 0, EndSeconds: 1}, {Text: "день", Speaker: "manager", StartSeconds: 1, EndSeconds: 2}, {Text: "Здравствуйте", Speaker: "client", StartSeconds: 2, EndSeconds: 3}}
	segments := renderSegments(words)
	if len(segments) != 2 || segments[0].Text != "Добрый день" || segments[1].Speaker != "client" {
		t.Fatalf("segments=%+v", segments)
	}
}

func TestReasonValidation(t *testing.T) {
	if validateReason("ok") == nil {
		t.Fatal("short reason accepted")
	}
	if validateReason("Исправлен говорящий") != nil {
		t.Fatal("valid reason rejected")
	}
}
