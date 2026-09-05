package dto

type TranscriptionResponse struct {
	ID                string                          `json:"id"`
	CallUUID          string                          `json:"call_uuid"`
	Status            string                          `json:"status"`
	Text              *string                         `json:"text"`
	Segments          []TranscriptionSegmentResponse  `json:"segments"`
	Words             []TranscriptionWordResponse     `json:"words"`
	Language          *string                         `json:"language"`
	Provider          string                          `json:"provider"`
	ErrorMessage      *string                         `json:"error_message"`
	CreatedAt         string                          `json:"created_at"`
	UpdatedAt         string                          `json:"updated_at"`
	Revision          int                             `json:"revision,omitempty"`
	Edited            bool                            `json:"edited"`
	Editable          bool                            `json:"editable"`
	EditabilityReason string                          `json:"editability_reason,omitempty"`
	Redaction         *TranscriptionRedactionResponse `json:"redaction,omitempty"`
}

type TranscriptionWordResponse struct {
	Text         string                              `json:"text"`
	StartSeconds float64                             `json:"start_seconds"`
	EndSeconds   float64                             `json:"end_seconds"`
	Confidence   *float64                            `json:"confidence,omitempty"`
	Speaker      string                              `json:"speaker,omitempty"`
	Redaction    *TranscriptionWordRedactionResponse `json:"redaction,omitempty"`
}

type TranscriptionWordRedactionResponse struct {
	SpanUUID   string `json:"span_uuid"`
	EntityType string `json:"entity_type"`
	Label      string `json:"label"`
	Marker     string `json:"marker"`
}

type TranscriptionRedactionCount struct {
	EntityType string `json:"entity_type"`
	Label      string `json:"label"`
	Marker     string `json:"marker"`
	Count      int    `json:"count"`
}

type TranscriptionRedactionResponse struct {
	Status         string                        `json:"status"`
	MarkerContract string                        `json:"marker_contract"`
	SpansCount     int                           `json:"spans_count"`
	EntityCounts   []TranscriptionRedactionCount `json:"entity_counts"`
}

type TranscriptionSegmentResponse struct {
	Speaker      string   `json:"speaker"`
	StartSeconds *float64 `json:"start_seconds,omitempty"`
	EndSeconds   *float64 `json:"end_seconds,omitempty"`
	Text         string   `json:"text"`
}

type TranscriptionWordEditRequest struct {
	WordIndex int     `json:"word_index"`
	Text      *string `json:"text,omitempty"`
	Speaker   *string `json:"speaker,omitempty"`
}

type UpdateTranscriptionRequest struct {
	ExpectedRevision int                            `json:"expected_revision"`
	Reason           string                         `json:"reason"`
	Edits            []TranscriptionWordEditRequest `json:"edits"`
}

type TranscriptionUpdateResponse struct {
	Transcription      TranscriptionResponse `json:"transcription"`
	Revision           int                   `json:"revision"`
	Reason             string                `json:"reason"`
	ChangedWordIndexes []int                 `json:"changed_word_indexes"`
}

type RestoreTranscriptionRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	Reason           string `json:"reason"`
}

type TranscriptionRevisionSummary struct {
	ID                 string `json:"id"`
	CallUUID           string `json:"call_uuid"`
	Revision           int    `json:"revision"`
	Reason             string `json:"reason"`
	ChangedWordIndexes []int  `json:"changed_word_indexes"`
	CreatedAt          string `json:"created_at"`
	IsCurrent          bool   `json:"is_current"`
}

type TranscriptionRevisionListResponse struct {
	Items []TranscriptionRevisionSummary `json:"items"`
	Total int                            `json:"total"`
}

type TranscriptionRevisionContentResponse struct {
	Revision  int                            `json:"revision"`
	IsCurrent bool                           `json:"is_current"`
	Text      string                         `json:"text"`
	Segments  []TranscriptionSegmentResponse `json:"segments"`
	Words     []TranscriptionWordResponse    `json:"words"`
}
