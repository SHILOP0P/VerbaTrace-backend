package assemblyai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"calllens/monolit/internal/models"
	"calllens/monolit/internal/transcriber/cleaner"
)

const (
	defaultBaseURL = "https://api.assemblyai.com"
	providerName   = "assemblyai"
	pollInterval   = 3 * time.Second
)

type Transcriber struct {
	apiKey        string
	speakerLabels bool
	identifyRoles bool
	baseURL       string
	client        *http.Client
	pollInterval  time.Duration
}

type transcriptRequest struct {
	AudioURL            string               `json:"audio_url"`
	SpeechModels        []string             `json:"speech_models"`
	LanguageDetection   bool                 `json:"language_detection"`
	SpeakerLabels       bool                 `json:"speaker_labels"`
	SpeechUnderstanding *speechUnderstanding `json:"speech_understanding,omitempty"`
}

type speechUnderstanding struct {
	Request struct {
		SpeakerIdentification speakerIdentification `json:"speaker_identification"`
	} `json:"request"`
}

type speakerIdentification struct {
	SpeakerType string            `json:"speaker_type"`
	Speakers    []speakerIdentity `json:"speakers"`
}

type speakerIdentity struct {
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	Description string `json:"description"`
}

type transcriptResponse struct {
	ID           string      `json:"id"`
	Status       string      `json:"status"`
	Error        string      `json:"error"`
	Text         string      `json:"text"`
	LanguageCode string      `json:"language_code"`
	Utterances   []utterance `json:"utterances"`
}

type utterance struct {
	Speaker string  `json:"speaker"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
}

func New(apiKey string, speakerLabels, identifyRoles bool) (*Transcriber, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("assemblyai api key is required")
	}
	return &Transcriber{
		apiKey:        apiKey,
		speakerLabels: speakerLabels,
		identifyRoles: identifyRoles,
		baseURL:       defaultBaseURL,
		client:        &http.Client{},
		pollInterval:  pollInterval,
	}, nil
}

func (t *Transcriber) Provider() string {
	if t.identifyRoles {
		return providerName + "-speaker-identification"
	}
	return providerName
}

func (t *Transcriber) Transcribe(ctx context.Context, file models.File) (models.TranscriptionResult, error) {
	if file.Content == nil {
		return models.TranscriptionResult{}, fmt.Errorf("%w: empty audio content", models.ErrUnsupportedAudioType)
	}
	uploadURL, err := t.upload(ctx, file.Content)
	if err != nil {
		return models.TranscriptionResult{}, err
	}
	transcriptID, err := t.createTranscript(ctx, uploadURL, file.SpeakerCandidates)
	if err != nil {
		return models.TranscriptionResult{}, err
	}
	result, err := t.waitForTranscript(ctx, transcriptID)
	if err != nil {
		return models.TranscriptionResult{}, err
	}
	return normalizeTranscript(result)
}

func (t *Transcriber) upload(ctx context.Context, content io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint("/v2/upload"), content)
	if err != nil {
		return "", fmt.Errorf("build AssemblyAI upload request: %w", err)
	}
	req.Header.Set("Authorization", t.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload audio to AssemblyAI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", decodeError(resp)
	}
	var body struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode AssemblyAI upload response: %w", err)
	}
	if strings.TrimSpace(body.UploadURL) == "" {
		return "", errors.New("AssemblyAI upload response has no upload_url")
	}
	return body.UploadURL, nil
}

func (t *Transcriber) createTranscript(ctx context.Context, audioURL string, candidates []models.SpeakerCandidate) (string, error) {
	payload := transcriptRequest{
		AudioURL:          audioURL,
		SpeechModels:      []string{"universal-2"},
		LanguageDetection: true,
		SpeakerLabels:     t.speakerLabels,
	}
	if t.identifyRoles && len(candidates) > 0 {
		payload.SpeechUnderstanding = speakerIdentificationRequest(candidates)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal AssemblyAI transcript request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint("/v2/transcript"), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build AssemblyAI transcript request: %w", err)
	}
	req.Header.Set("Authorization", t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create AssemblyAI transcript: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", decodeError(resp)
	}
	var result transcriptResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode AssemblyAI transcript response: %w", err)
	}
	if strings.TrimSpace(result.ID) == "" {
		return "", errors.New("AssemblyAI transcript response has no id")
	}
	return result.ID, nil
}

func speakerIdentificationRequest(candidates []models.SpeakerCandidate) *speechUnderstanding {
	request := &speechUnderstanding{}
	allRoles := true
	for _, candidate := range candidates {
		if candidate.Kind != "role" {
			allRoles = false
			break
		}
	}
	speakerType := "name"
	if allRoles {
		speakerType = "role"
	}
	speakers := make([]speakerIdentity, 0, len(candidates))
	for _, candidate := range candidates {
		identity := speakerIdentity{Description: strings.TrimSpace(candidate.Description)}
		if speakerType == "role" {
			identity.Role = strings.TrimSpace(candidate.Label)
		} else {
			identity.Name = strings.TrimSpace(candidate.Label)
			if candidate.Kind == "role" {
				if identity.Description != "" {
					identity.Description += ". "
				}
				identity.Description += "Это роль участника, а не имя человека"
			}
		}
		speakers = append(speakers, identity)
	}
	request.Request.SpeakerIdentification = speakerIdentification{
		SpeakerType: speakerType,
		Speakers:    speakers,
	}
	return request
}

func (t *Transcriber) waitForTranscript(ctx context.Context, transcriptID string) (transcriptResponse, error) {
	for {
		result, err := t.getTranscript(ctx, transcriptID)
		if err != nil {
			return transcriptResponse{}, err
		}
		switch result.Status {
		case "completed":
			return result, nil
		case "error":
			return transcriptResponse{}, fmt.Errorf("AssemblyAI transcription failed: %s", strings.TrimSpace(result.Error))
		}
		select {
		case <-ctx.Done():
			return transcriptResponse{}, ctx.Err()
		case <-time.After(t.pollInterval):
		}
	}
}

func (t *Transcriber) getTranscript(ctx context.Context, transcriptID string) (transcriptResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.endpoint("/v2/transcript/"+transcriptID), nil)
	if err != nil {
		return transcriptResponse{}, fmt.Errorf("build AssemblyAI transcript poll request: %w", err)
	}
	req.Header.Set("Authorization", t.apiKey)
	resp, err := t.client.Do(req)
	if err != nil {
		return transcriptResponse{}, fmt.Errorf("poll AssemblyAI transcript: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return transcriptResponse{}, decodeError(resp)
	}
	var result transcriptResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return transcriptResponse{}, fmt.Errorf("decode AssemblyAI transcript poll response: %w", err)
	}
	return result, nil
}

func normalizeTranscript(result transcriptResponse) (models.TranscriptionResult, error) {
	text := cleaner.Clean(result.Text)
	if text == "" {
		return models.TranscriptionResult{}, errors.New("AssemblyAI transcription response is empty")
	}
	segments := make([]models.TranscriptionSegment, 0, len(result.Utterances))
	for _, utterance := range result.Utterances {
		segmentText := cleaner.Clean(utterance.Text)
		if segmentText == "" {
			continue
		}
		start, end := utterance.Start/1000, utterance.End/1000
		segments = append(segments, models.TranscriptionSegment{
			Speaker: strings.TrimSpace(utterance.Speaker), StartSeconds: &start, EndSeconds: &end, Text: segmentText,
		})
	}
	language := strings.TrimSpace(result.LanguageCode)
	transcript := models.TranscriptionResult{Text: text, Segments: segments}
	if language != "" {
		transcript.Language = &language
	}
	return transcript, nil
}

func (t *Transcriber) endpoint(path string) string {
	return strings.TrimRight(t.baseURL, "/") + path
}

func decodeError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("AssemblyAI request failed with status %d: read error response: %w", resp.StatusCode, err)
	}
	message := strings.TrimSpace(string(body))
	var apiError struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &apiError) == nil && strings.TrimSpace(apiError.Error) != "" {
		message = apiError.Error
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("AssemblyAI request failed with status %d: %s", resp.StatusCode, message)
}
