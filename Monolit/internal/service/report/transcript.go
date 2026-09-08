package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"
)

func formatTranscript(t models.Transcription) string { return formatTranscriptWithSpeakers(t, nil) }

// Match TranscriptPreview: timed words take precedence, adjacent speaker turns
// form dialogue blocks, and a transcript without speakers remains plain text.
func formatTranscriptWithSpeakers(t models.Transcription, names map[string]string) string {
	words := make([]models.TranscriptionWord, 0, len(t.Words))
	hasSpeaker := false
	for _, word := range t.Words {
		if word.Text == "" || math.IsNaN(word.StartSeconds) || math.IsNaN(word.EndSeconds) || math.IsInf(word.StartSeconds, 0) || math.IsInf(word.EndSeconds, 0) {
			continue
		}
		words = append(words, word)
		hasSpeaker = hasSpeaker || strings.TrimSpace(word.Speaker) != ""
	}
	if len(words) > 0 {
		if !hasSpeaker {
			return joinTranscriptWords(words)
		}
		parts := []string{}
		for start := 0; start < len(words); {
			end := start + 1
			for end < len(words) && strings.TrimSpace(words[end].Speaker) == strings.TrimSpace(words[start].Speaker) {
				end++
			}
			label := reportSpeakerLabel(words[start].Speaker, names)
			parts = append(parts, fmt.Sprintf("%s · %s – %s\n%s", label, transcriptTimestamp(words[start].StartSeconds), transcriptTimestamp(words[end-1].EndSeconds), joinTranscriptWords(words[start:end])))
			start = end
		}
		return strings.Join(parts, "\n\n")
	}
	parts := []string{}
	for _, segment := range t.Segments {
		if strings.TrimSpace(segment.Text) == "" {
			continue
		}
		if strings.TrimSpace(segment.Speaker) == "" {
			parts = nil
			break
		}
		label := reportSpeakerLabel(segment.Speaker, names)
		if segment.StartSeconds != nil {
			label += " · " + transcriptTimestamp(*segment.StartSeconds)
		}
		if segment.EndSeconds != nil {
			label += " – " + transcriptTimestamp(*segment.EndSeconds)
		}
		parts = append(parts, label+"\n"+strings.TrimSpace(segment.Text))
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	if t.Text != nil {
		return strings.TrimSpace(*t.Text)
	}
	return ""
}

func joinTranscriptWords(words []models.TranscriptionWord) string {
	var b strings.Builder
	for index, word := range words {
		first, _ := utf8.DecodeRuneInString(word.Text)
		if index > 0 && !strings.ContainsRune(",.;:!?%)]}»”’…", first) {
			b.WriteByte(' ')
		}
		b.WriteString(word.Text)
	}
	return b.String()
}
func reportSpeakerLabel(key string, names map[string]string) string {
	key = strings.TrimSpace(key)
	if value := strings.TrimSpace(names[key]); value != "" {
		return value
	}
	if key == "" || key == "unknown" {
		return "Спикер не указан"
	}
	if strings.HasPrefix(strings.ToLower(key), "speaker_") {
		if number, err := strconv.Atoi(key[8:]); err == nil && number >= 0 {
			return fmt.Sprintf("Спикер %d", number+1)
		}
	}
	return key
}
func transcriptTimestamp(value float64) string {
	seconds := int(math.Max(0, math.Floor(value)))
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
