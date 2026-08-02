package dto

type TranscriptionResponse struct {
	ID           string                         `json:"id"`
	CallUUID     string                         `json:"call_uuid"`
	Status       string                         `json:"status"`
	Text         *string                        `json:"text"`
	Segments     []TranscriptionSegmentResponse `json:"segments"`
	Words        []TranscriptionWordResponse    `json:"words"`
	Language     *string                        `json:"language"`
	Provider     string                         `json:"provider"`
	ErrorMessage *string                        `json:"error_message"`
	CreatedAt    string                         `json:"created_at"`
	UpdatedAt    string                         `json:"updated_at"`
}

type TranscriptionWordResponse struct {
	Text         string   `json:"text"`
	StartSeconds float64  `json:"start_seconds"`
	EndSeconds   float64  `json:"end_seconds"`
	Confidence   *float64 `json:"confidence,omitempty"`
	Speaker      string   `json:"speaker,omitempty"`
}

type TranscriptionSegmentResponse struct {
	Speaker      string   `json:"speaker"`
	StartSeconds *float64 `json:"start_seconds,omitempty"`
	EndSeconds   *float64 `json:"end_seconds,omitempty"`
	Text         string   `json:"text"`
}
