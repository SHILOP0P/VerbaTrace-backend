package assemblyai

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"
)

var providerMarkerPattern = regexp.MustCompile(`\[([A-Z0-9_]+)\]`)

var providerEntityToInternal = map[string]string{
	"PERSON_NAME": "person_name", "PHONE_NUMBER": "phone_number", "EMAIL_ADDRESS": "email_address",
	"LOCATION_ADDRESS": "address", "LOCATION_ADDRESS_STREET": "address", "LOCATION_ZIP": "address",
	"DATE_OF_BIRTH": "date_of_birth", "PASSPORT_NUMBER": "passport_number", "DRIVERS_LICENSE": "drivers_license",
	"ACCOUNT_NUMBER": "account_number", "BANKING_INFORMATION": "banking_information",
	"CREDIT_CARD_NUMBER": "credit_card_number", "CREDIT_CARD_CVV": "credit_card_cvv",
	"CREDIT_CARD_EXPIRATION": "credit_card_expiration", "PASSWORD": "password", "IP_ADDRESS": "ip_address",
	"USERNAME": "username", "MEDICAL_CONDITION": "medical_condition", "MONEY_AMOUNT": "money_amount",
	"ORGANIZATION": "organization",
}

func providerPolicies(entityTypes []string) ([]string, error) {
	result := make([]string, 0, len(entityTypes)+2)
	for _, entity := range entityTypes {
		switch entity {
		case "address":
			result = append(result, "location_address", "location_address_street", "location_zip")
		default:
			if _, ok := models.PrivacyMarkersRUv1[entity]; !ok {
				return nil, fmt.Errorf("unknown privacy entity type %q", entity)
			}
			result = append(result, entity)
		}
	}
	sort.Strings(result)
	return result, nil
}

func normalizePrivacyResult(result models.TranscriptionResult, request *models.TranscriptionPrivacyRequest) (models.TranscriptionResult, error) {
	if request == nil {
		return result, nil
	}
	if request.MarkerContract != models.PrivacyMarkerContractRUv1 {
		return models.TranscriptionResult{}, fmt.Errorf("unsupported marker contract %q", request.MarkerContract)
	}
	allowed := map[string]struct{}{}
	for _, entity := range request.EntityTypes {
		allowed[entity] = struct{}{}
	}
	spans := make([]models.RedactionSpan, 0)
	for index := range result.Words {
		word := &result.Words[index]
		trimmed := strings.TrimSpace(word.Text)
		match := providerMarkerPattern.FindStringSubmatch(trimmed)
		if len(match) == 0 {
			continue
		}
		location := providerMarkerPattern.FindStringIndex(trimmed)
		prefix, suffix := trimmed[:location[0]], trimmed[location[1]:]
		if !providerMarkerAffix(prefix) || !providerMarkerAffix(suffix) {
			return models.TranscriptionResult{}, fmt.Errorf("privacy provider marker is embedded in a word")
		}
		entity, ok := providerEntityToInternal[match[1]]
		if !ok {
			return models.TranscriptionResult{}, fmt.Errorf("unknown privacy provider marker %q", match[1])
		}
		if _, ok = allowed[entity]; !ok {
			return models.TranscriptionResult{}, fmt.Errorf("provider returned marker outside requested policy: %s", entity)
		}
		marker := models.PrivacyMarkersRUv1[entity]
		word.Text = prefix + marker + suffix
		spans = append(spans, models.RedactionSpan{EntityType: entity, Marker: marker, WordStartIndex: index, WordEndIndex: index, StartSeconds: word.StartSeconds, EndSeconds: word.EndSeconds, Source: "provider", ProviderPolicy: strings.ToLower(match[1])})
	}
	var err error
	result.Text, err = replaceProviderMarkers(result.Text, allowed)
	if err != nil {
		return models.TranscriptionResult{}, err
	}
	for index := range result.Segments {
		result.Segments[index].Text, err = replaceProviderMarkers(result.Segments[index].Text, allowed)
		if err != nil {
			return models.TranscriptionResult{}, err
		}
	}
	result.RedactionSpans = spans
	return result, nil
}

func providerMarkerAffix(value string) bool {
	return strings.Trim(value, " \t\r\n.,!?;:()[]{}«»\"'—–-") == ""
}

func replaceProviderMarkers(input string, allowed map[string]struct{}) (string, error) {
	var replaceErr error
	output := providerMarkerPattern.ReplaceAllStringFunc(input, func(raw string) string {
		match := providerMarkerPattern.FindStringSubmatch(raw)
		entity, ok := providerEntityToInternal[match[1]]
		if !ok {
			replaceErr = fmt.Errorf("unknown privacy provider marker %q", match[1])
			return raw
		}
		if _, ok = allowed[entity]; !ok {
			replaceErr = fmt.Errorf("provider returned marker outside requested policy: %s", entity)
			return raw
		}
		return models.PrivacyMarkersRUv1[entity]
	})
	return output, replaceErr
}
