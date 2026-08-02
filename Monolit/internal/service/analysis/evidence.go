package analysis

import (
	"encoding/json"
	"strings"
	"unicode"

	"verbatrace/monolit/internal/models"
)

const (
	evidenceMatched   = "matched"
	evidenceAmbiguous = "ambiguous"
	evidenceNotFound  = "not_found"
)

type structuredEvidence struct {
	Quote          string   `json:"quote"`
	StartSeconds   *float64 `json:"start_seconds,omitempty"`
	EndSeconds     *float64 `json:"end_seconds,omitempty"`
	WordStartIndex *int     `json:"word_start_index,omitempty"`
	WordEndIndex   *int     `json:"word_end_index,omitempty"`
	Speaker        string   `json:"speaker,omitempty"`
	MatchStatus    string   `json:"match_status"`
}

func enrichEvidence(resultJSON []byte, words []models.TranscriptionWord) (json.RawMessage, error) {
	var root any
	if err := json.Unmarshal(resultJSON, &root); err != nil {
		return nil, err
	}
	enrichEvidenceNode(root, words)
	return json.Marshal(root)
}

func enrichEvidenceNode(node any, words []models.TranscriptionWord) {
	switch value := node.(type) {
	case []any:
		for _, child := range value {
			enrichEvidenceNode(child, words)
		}
	case map[string]any:
		if _, exists := value["evidence"]; !exists {
			quotes := make([]string, 0)
			if quote, ok := value["quote"].(string); ok && strings.TrimSpace(quote) != "" {
				quotes = append(quotes, quote)
			}
			if raw, ok := value["evidence_quotes"].([]any); ok {
				for _, item := range raw {
					if quote, ok := item.(string); ok && strings.TrimSpace(quote) != "" {
						quotes = append(quotes, quote)
					}
				}
			}
			if len(quotes) > 0 {
				evidence := make([]structuredEvidence, 0, len(quotes))
				for _, quote := range quotes {
					evidence = append(evidence, matchEvidence(quote, words))
				}
				value["evidence"] = evidence
			}
		}
		for _, child := range value {
			enrichEvidenceNode(child, words)
		}
	}
}

func matchEvidence(quote string, words []models.TranscriptionWord) structuredEvidence {
	result := structuredEvidence{Quote: quote, MatchStatus: evidenceNotFound}
	query := normalizedTokens(quote)
	if len(query) == 0 {
		return result
	}
	transcript := make([]string, len(words))
	for i, word := range words {
		transcript[i] = normalizedWord(word.Text)
	}
	matchStart := -1
	matchCount := 0
	for start := 0; start+len(query) <= len(transcript); start++ {
		matched := true
		for offset := range query {
			if query[offset] != transcript[start+offset] {
				matched = false
				break
			}
		}
		if matched {
			matchStart = start
			matchCount++
		}
	}
	if matchCount != 1 {
		if matchCount > 1 {
			result.MatchStatus = evidenceAmbiguous
		}
		return result
	}
	end := matchStart + len(query) - 1
	startSeconds, endSeconds := words[matchStart].StartSeconds, words[end].EndSeconds
	startIndex, endIndex := matchStart, end
	result.StartSeconds = &startSeconds
	result.EndSeconds = &endSeconds
	result.WordStartIndex = &startIndex
	result.WordEndIndex = &endIndex
	for i := matchStart; i <= end; i++ {
		if strings.TrimSpace(words[i].Speaker) != "" {
			if result.Speaker == "" {
				result.Speaker = words[i].Speaker
			} else if result.Speaker != words[i].Speaker {
				result.Speaker = ""
				break
			}
		}
	}
	result.MatchStatus = evidenceMatched
	return result
}

func normalizedTokens(value string) []string {
	parts := strings.Fields(value)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if token := normalizedWord(part); token != "" {
			result = append(result, token)
		}
	}
	return result
}

func normalizedWord(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "ё", "е")
	runes := []rune(value)
	for len(runes) > 0 && unicode.IsPunct(runes[0]) {
		runes = runes[1:]
	}
	for len(runes) > 0 && unicode.IsPunct(runes[len(runes)-1]) {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
