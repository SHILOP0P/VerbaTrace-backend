package speech

import (
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/stretchr/testify/require"
)

func word(speaker, text string, start, end float64) models.TranscriptionWord {
	return models.TranscriptionWord{Speaker: speaker, Text: text, StartSeconds: start, EndSeconds: end}
}

// A hand-marked conversation with the numbers counted by hand:
//
//	A1 0.0–1.8  3 words, 1 question    B1 2.5–3.6  2 words
//	A2 4.6–6.2  3 words, 1 question    B2 11.0–13.4 5 words (after a 4.8 s pause)
//	A3 13.9–14.2 "угу"                 B3 14.5–15.4 2 words
//
// Talk: A 1.8+1.6+0.3 = 3.7 s, B 1.1+2.4+0.9 = 4.4 s, share 45.68 / 54.32.
// Pace: A 7 words in 3.7 s = 114 wpm, B 9 in 4.4 s = 123 wpm. Duration 15.4 s:
// A's two questions are 467.5 an hour. Response pauses: B 0.7, 4.8, 0.3 →
// median 700 ms; A 1.0, 0.5 → 750 ms. One pause of 4 s or more, the longest
// 4.8 s → 5. Turns of three words or more: A1, A2, B2 → one switch in 15.4 s,
// 19.5 per five minutes.
func TestComputeMatchesTheHandCount(t *testing.T) {
	words := []models.TranscriptionWord{
		word("A", "Здравствуйте,", 0.0, 0.8), word("A", "как", 1.0, 1.2), word("A", "дела?", 1.3, 1.8),
		word("B", "Хорошо,", 2.5, 3.0), word("B", "спасибо.", 3.1, 3.6),
		word("A", "Расскажите", 4.6, 5.2), word("A", "о", 5.3, 5.4), word("A", "проекте?", 5.5, 6.2),
		word("B", "Мы", 11.0, 11.2), word("B", "делаем", 11.3, 11.8), word("B", "сайт", 11.9, 12.4), word("B", "для", 12.5, 12.7), word("B", "магазина.", 12.8, 13.4),
		word("A", "Угу.", 13.9, 14.2),
		word("B", "И", 14.5, 14.6), word("B", "интеграцию.", 14.7, 15.4),
		{Text: "шум", StartSeconds: 15.5, EndSeconds: 15.6},
	}
	call, speakers := Compute(words)
	require.Len(t, speakers, 2)
	a, b := speakers[0], speakers[1]
	require.Equal(t, "A", a.Key)

	require.Equal(t, 4, a.TalkSeconds)
	require.InDelta(t, 45.68, a.TalkShare, 0.001)
	require.InDelta(t, 54.32, b.TalkShare, 0.001)
	require.Equal(t, 7, a.Words)
	require.Equal(t, 9, b.Words)
	require.Equal(t, 114, *a.WordsPerMinute)
	require.Equal(t, 123, *b.WordsPerMinute)
	require.Equal(t, 2, a.LongestMonologueSeconds)
	require.Equal(t, 2, b.LongestMonologueSeconds)
	require.Equal(t, 2, a.Questions)
	require.InDelta(t, 467.5, *a.QuestionsPerHour, 0.001)
	require.Equal(t, 750, *a.ResponsePauseMedianMs)
	require.Equal(t, 700, *b.ResponsePauseMedianMs)

	require.Equal(t, 1, call.PausesOverThreshold)
	require.Equal(t, 5, call.LongestPauseSeconds)
	require.InDelta(t, 19.5, *call.SpeakerSwitchesPer5Min, 0.001)
}

func TestComputeWithoutSpeakersIsEmpty(t *testing.T) {
	call, speakers := Compute([]models.TranscriptionWord{{Text: "слово", StartSeconds: 0, EndSeconds: 1}})
	require.Empty(t, speakers)
	require.Nil(t, call.SpeakerSwitchesPer5Min)
}
