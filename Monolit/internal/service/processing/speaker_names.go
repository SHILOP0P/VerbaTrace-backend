package processing

import (
	"regexp"
	"strings"
	"unicode"

	"verbatrace/monolit/internal/models"
)

var selfIntroductionPattern = regexp.MustCompile(`(?i)(?:меня\s+зовут|мо[её]\s+имя|это)\s+([А-ЯЁ][а-яё-]{1,30})`)

func inferSpeakerNames(segments []models.TranscriptionSegment) map[string]string {
	result := make(map[string]string)
	for index, segment := range segments {
		text := strings.TrimSpace(segment.Text)
		if match := selfIntroductionPattern.FindStringSubmatch(text); len(match) == 2 {
			result[segment.Speaker] = match[1]
			continue
		}
		if index == 0 || !asksForName(segments[index-1].Text) || segments[index-1].Speaker == segment.Speaker {
			continue
		}
		if name := shortNameAnswer(text); name != "" {
			result[segment.Speaker] = name
		}
	}
	return result
}

func asksForName(text string) bool {
	normalized := strings.ToLower(text)
	return strings.Contains(normalized, "как вас зовут") || strings.Contains(normalized, "как к вам обращаться") || strings.Contains(normalized, "как я могу к вам обращаться") || strings.Contains(normalized, "представьтесь")
}

func shortNameAnswer(text string) string {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 || len(words) > 8 {
		return ""
	}
	for _, raw := range words {
		if _, generic := nonNameAnswerWords[strings.ToLower(strings.TrimFunc(raw, func(r rune) bool { return !unicode.IsLetter(r) }))]; generic {
			continue
		}
		word := strings.Trim(raw, `.,!?;:—–-«»"()`)
		runes := []rune(word)
		if len(runes) >= 2 && len(runes) <= 31 && unicode.IsUpper(runes[0]) {
			valid := true
			for _, r := range runes[1:] {
				if !unicode.IsLower(r) && r != '-' {
					valid = false
					break
				}
			}
			if valid {
				return word
			}
		}
	}
	return ""
}

var nonNameAnswerWords = map[string]struct{}{
	"да": {}, "нет": {}, "конечно": {}, "хорошо": {}, "здравствуйте": {},
	"меня": {}, "мое": {}, "моё": {}, "имя": {}, "зовут": {}, "это": {}, "я": {},
}
