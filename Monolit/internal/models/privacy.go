package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const PrivacyMarkerContractRUv1 = "ru-v1"

type PrivacyScopeType string

const (
	PrivacyScopePersonal   PrivacyScopeType = "personal"
	PrivacyScopeCompany    PrivacyScopeType = "company"
	PrivacyScopeDepartment PrivacyScopeType = "department"
)

type PrivacyPolicyConfig struct {
	SchemaVersion       int      `json:"schema_version"`
	Enabled             bool     `json:"enabled"`
	Enforcement         string   `json:"enforcement"`
	MarkerContract      string   `json:"marker_contract"`
	EntityTypes         []string `json:"entity_types"`
	OriginalMediaAccess string   `json:"original_media_access"`
	SanitizedMedia      string   `json:"sanitized_media"`
	Exports             string   `json:"exports"`
	AnalysisInput       string   `json:"analysis_input"`
}

type PrivacyPolicyVersion struct {
	ID            uuid.UUID
	PolicyID      uuid.UUID
	Version       int
	Config        PrivacyPolicyConfig
	ConfigSHA256  []byte
	PublishReason string
	PublishedBy   uuid.UUID
	PublishedAt   time.Time
}

type PrivacyPolicyDraft struct {
	PolicyID     uuid.UUID
	Config       PrivacyPolicyConfig
	ConfigSHA256 []byte
	LockVersion  int64
	UpdatedBy    uuid.UUID
	UpdatedAt    time.Time
}

type PrivacyPolicyView struct {
	ScopeType          PrivacyScopeType
	ScopeID            uuid.UUID
	PolicyID           uuid.UUID
	ActiveVersion      *PrivacyPolicyVersion
	InheritedScopeType PrivacyScopeType
	InheritedScopeID   uuid.UUID
	InheritedVersion   *PrivacyPolicyVersion
	Draft              *PrivacyPolicyDraft
	CanManage          bool
	MarkerContract     string
}

type CallPrivacyState struct {
	CallID          uuid.UUID
	PolicyVersionID uuid.NullUUID
	PolicySource    string
	PolicySnapshot  PrivacyPolicyConfig
	MarkerContract  string
	Status          string
	Revision        int
	DetectedSpans   int
	LastErrorCode   *string
	UpdatedAt       time.Time
}

type RedactionSpan struct {
	ID              uuid.UUID
	TranscriptionID uuid.UUID
	Revision        int
	EntityType      string
	Marker          string
	WordStartIndex  int
	WordEndIndex    int
	StartSeconds    float64
	EndSeconds      float64
	Source          string
	ProviderPolicy  string
	CreatedByUserID uuid.NullUUID
}

type TranscriptionPrivacyRequest struct {
	MarkerContract string
	EntityTypes    []string
}

type TranscriptionRequest struct {
	File    File
	Mode    TranscriptionMode
	Privacy *TranscriptionPrivacyRequest
}

type MediaVariant struct {
	ID                    uuid.UUID
	CallID                uuid.UUID
	Variant               string
	TranscriptionRevision int
	Status                string
	StoragePath           string
	FileName              string
	MIMEType              string
	SizeBytes             int64
	ProcessorContract     string
	Container             string
	AudioCodec            string
	VideoCodec            string
	VideoStreamCopied     *bool
	OutputDurationMS      int64
	LastErrorCode         *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type PrivacyCapabilities struct {
	CanReadOriginalMedia     bool `json:"can_read_original_media"`
	CanRequestSanitizedMedia bool `json:"can_request_sanitized_media"`
	CanReviewRedactions      bool `json:"can_review_redactions"`
	CanManagePrivacyPolicy   bool `json:"can_manage_privacy_policy"`
}

type AnalysisRedactionMarker struct {
	Marker     string
	EntityType string
	Count      int
}

type AnalysisRedactionContext struct {
	MarkerContract string
	PresentMarkers []AnalysisRedactionMarker
}

type PrivacyAuditEvent struct {
	ID         uuid.UUID
	CallID     uuid.NullUUID
	ActorID    uuid.NullUUID
	EventType  string
	EntityType string
	EntityID   uuid.NullUUID
	Metadata   json.RawMessage
	CreatedAt  time.Time
}

var DefaultPrivacyEntityTypes = []string{
	"person_name", "phone_number", "email_address", "address", "date_of_birth",
	"passport_number", "drivers_license", "account_number", "banking_information",
	"credit_card_number", "credit_card_cvv", "credit_card_expiration", "password",
}

var PrivacyMarkersRUv1 = map[string]string{
	"person_name":            "[ИМЯ]",
	"phone_number":           "[ТЕЛЕФОН]",
	"email_address":          "[ЭЛЕКТРОННАЯ_ПОЧТА]",
	"address":                "[АДРЕС]",
	"date_of_birth":          "[ДАТА_РОЖДЕНИЯ]",
	"passport_number":        "[НОМЕР_ПАСПОРТА]",
	"drivers_license":        "[НОМЕР_ВОДИТЕЛЬСКОГО_УДОСТОВЕРЕНИЯ]",
	"account_number":         "[НОМЕР_СЧЁТА]",
	"banking_information":    "[БАНКОВСКИЕ_ДАННЫЕ]",
	"credit_card_number":     "[НОМЕР_КАРТЫ]",
	"credit_card_cvv":        "[КОД_КАРТЫ]",
	"credit_card_expiration": "[СРОК_ДЕЙСТВИЯ_КАРТЫ]",
	"password":               "[СЕКРЕТ]",
	"ip_address":             "[СЕТЕВОЙ_АДРЕС]",
	"username":               "[ИМЯ_ПОЛЬЗОВАТЕЛЯ]",
	"medical_condition":      "[МЕДИЦИНСКИЕ_ДАННЫЕ]",
	"money_amount":           "[ДЕНЕЖНАЯ_СУММА]",
	"organization":           "[ОРГАНИЗАЦИЯ]",
}

func DefaultPrivacyPolicyConfig() PrivacyPolicyConfig {
	return PrivacyPolicyConfig{
		SchemaVersion: 1, Enabled: false, Enforcement: "required",
		MarkerContract:      PrivacyMarkerContractRUv1,
		EntityTypes:         append([]string(nil), DefaultPrivacyEntityTypes...),
		OriginalMediaAccess: "call_acl", SanitizedMedia: "on_demand",
		Exports: "redacted_by_default", AnalysisInput: "redacted",
	}
}
