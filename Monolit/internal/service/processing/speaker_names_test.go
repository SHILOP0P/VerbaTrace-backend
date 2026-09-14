package processing

import (
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestInferSpeakerNamesFromExplicitDialogue(t *testing.T) {
	names := inferSpeakerNames([]models.TranscriptionSegment{
		{Speaker: "A", Text: "Здравствуйте, как я могу к вам обращаться?"},
		{Speaker: "C", Text: "Виктор."},
		{Speaker: "B", Text: "Меня зовут Ирина, я оператор."},
	})
	if names["C"] != "Виктор" || names["B"] != "Ирина" {
		t.Fatalf("unexpected names: %#v", names)
	}
}

func TestInferSpeakerNamesDoesNotGuessFromLongAnswer(t *testing.T) {
	names := inferSpeakerNames([]models.TranscriptionSegment{
		{Speaker: "A", Text: "Как к вам обращаться?"},
		{Speaker: "B", Text: "Добрый день, я хотел сначала подробно рассказать о нашей компании и текущем проекте"},
	})
	if len(names) != 0 {
		t.Fatalf("unexpected names: %#v", names)
	}
}

func TestInferSpeakerNamesKeepsExplicitNameAfterNameQuestion(t *testing.T) {
	names := inferSpeakerNames([]models.TranscriptionSegment{
		{Speaker: "A", Text: "Как вас зовут?"},
		{Speaker: "B", Text: "Меня зовут Дмитрий."},
	})
	if names["B"] != "Дмитрий" {
		t.Fatalf("unexpected names: %#v", names)
	}
}
