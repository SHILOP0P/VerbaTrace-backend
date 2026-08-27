package integration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/integrationcrypto"
	"verbatrace/monolit/internal/mediafetch"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type Repository interface {
	CreateConnection(context.Context, models.CreateIntegrationConnectionInput) (models.IntegrationConnection, error)
	ListConnections(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationConnection, error)
	GetConnection(context.Context, uuid.UUID, uuid.UUID) (models.IntegrationConnection, error)
	ChangeConnectionStatus(context.Context, uuid.UUID, uuid.UUID, string, int64) (models.IntegrationConnection, error)
	UpdateConnection(context.Context, models.UpdateIntegrationConnectionInput) (models.IntegrationConnection, error)
	AcceptURLIngest(context.Context, models.IntegrationPrincipal, models.IngestCallInput, string, [32]byte, []byte) (models.IngestItem, bool, error)
	AcceptUploadIngest(context.Context, models.IntegrationPrincipal, models.IngestCallInput, string, [32]byte, []byte) (models.IngestItem, bool, error)
	GetIngest(context.Context, uuid.UUID, models.IntegrationPrincipal) (models.IngestItem, error)
	ListIngest(context.Context, uuid.UUID, uuid.UUID, int, int) ([]models.IngestItem, int, error)
	RetryIngest(context.Context, uuid.UUID, uuid.UUID) (models.IngestItem, error)
	CancelIngest(context.Context, uuid.UUID, uuid.UUID) (models.IngestItem, error)
	ListAudit(context.Context, uuid.UUID, uuid.UUID, int, int) ([]models.IntegrationAuditEvent, int, error)
	CreateWebhook(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, []string) (models.WebhookEndpoint, string, error)
	ListWebhooks(context.Context, uuid.UUID, uuid.UUID) ([]models.WebhookEndpoint, error)
	RevokeWebhook(context.Context, uuid.UUID, uuid.UUID) error
	ListDeliveries(context.Context, uuid.UUID, uuid.UUID) ([]models.WebhookDelivery, error)
	QueueWebhookTest(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error)
	ListDestinations(context.Context, models.IntegrationPrincipal) ([]models.IntegrationDestination, error)
	ListFolders(context.Context, models.IntegrationPrincipal, string, uuid.UUID, uuid.UUID) ([]models.IntegrationFolder, error)
	GetCall(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationCallView, error)
	GetTranscription(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationTranscriptionView, error)
	GetAnalysis(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationAnalysisView, error)
}
type KeyAuthenticator interface {
	AuthenticateIntegrationKey(context.Context, string, string, string) (models.IntegrationPrincipal, error)
}
type Service struct {
	repo       Repository
	auth       KeyAuthenticator
	cipher     *integrationcrypto.Cipher
	stagingDir string
}

func NewService(repo Repository, auth KeyAuthenticator, cipher *integrationcrypto.Cipher, stagingDir ...string) *Service {
	dir := ""
	if len(stagingDir) > 0 {
		dir = stagingDir[0]
	}
	return &Service{repo: repo, auth: auth, cipher: cipher, stagingDir: dir}
}

func (s *Service) AcceptUploadIngest(ctx context.Context, p models.IntegrationPrincipal, in models.IngestCallInput, idempotency string, content io.Reader) (models.IngestItem, bool, error) {
	if err := applyAIMode(&in, p); err != nil {
		return models.IngestItem{}, false, err
	}
	if err := validateIngestInput(in, false, idempotency); err != nil {
		return models.IngestItem{}, false, err
	}
	if s.stagingDir == "" || content == nil || s.cipher == nil {
		return models.IngestItem{}, false, models.ErrIntegrationDisabled
	}
	if err := os.MkdirAll(s.stagingDir, 0700); err != nil {
		return models.IngestItem{}, false, err
	}
	tmp, err := os.CreateTemp(s.stagingDir, "upload-*.part")
	if err != nil {
		return models.IngestItem{}, false, err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(content, (500<<20)+1))
	if err != nil {
		return models.IngestItem{}, false, err
	}
	if size <= 0 || size > 500<<20 {
		return models.IngestItem{}, false, models.ErrInvalidBillingInput
	}
	if err = tmp.Sync(); err != nil {
		return models.IngestItem{}, false, err
	}
	if err = tmp.Close(); err != nil {
		return models.IngestItem{}, false, err
	}
	finalName := uuid.NewString() + ".media"
	finalPath := filepath.Join(s.stagingDir, finalName)
	if err = os.Rename(tmpPath, finalPath); err != nil {
		return models.IngestItem{}, false, err
	}
	tmpPath = finalPath
	in.RecordingURL = ""
	canonical, err := json.Marshal(struct {
		Input       models.IngestCallInput `json:"input"`
		MediaSHA256 string                 `json:"media_sha256"`
		Size        int64                  `json:"size"`
	}{in, fmt.Sprintf("%x", hash.Sum(nil)), size})
	if err != nil {
		return models.IngestItem{}, false, err
	}
	requestHash := sha256.Sum256(append([]byte("POST\n/ingest/calls/upload\n"+p.ApplicationUUID.String()+"\n"), canonical...))
	locator, err := s.cipher.Encrypt([]byte(finalName), "ingest_items/application/"+p.ApplicationUUID.String()+"/recording_locator")
	if err != nil {
		return models.IngestItem{}, false, err
	}
	item, deduplicated, err := s.repo.AcceptUploadIngest(ctx, p, in, strings.TrimSpace(idempotency), requestHash, locator)
	if err != nil {
		return item, false, err
	}
	if deduplicated {
		return item, true, nil
	}
	keep = true
	return item, false, nil
}
func (s *Service) CreateConnection(ctx context.Context, in models.CreateIntegrationConnectionInput) (models.IntegrationConnection, error) {
	return s.repo.CreateConnection(ctx, in)
}
func (s *Service) ListConnections(ctx context.Context, app, actor uuid.UUID) ([]models.IntegrationConnection, error) {
	return s.repo.ListConnections(ctx, app, actor)
}
func (s *Service) GetConnection(ctx context.Context, id, actor uuid.UUID) (models.IntegrationConnection, error) {
	return s.repo.GetConnection(ctx, id, actor)
}
func (s *Service) ChangeConnectionStatus(ctx context.Context, id, actor uuid.UUID, status string, version int64) (models.IntegrationConnection, error) {
	return s.repo.ChangeConnectionStatus(ctx, id, actor, status, version)
}
func (s *Service) UpdateConnection(ctx context.Context, input models.UpdateIntegrationConnectionInput) (models.IntegrationConnection, error) {
	return s.repo.UpdateConnection(ctx, input)
}
func (s *Service) Authenticate(ctx context.Context, key, environment, scope string) (models.IntegrationPrincipal, error) {
	return s.auth.AuthenticateIntegrationKey(ctx, key, environment, scope)
}
func (s *Service) AcceptURLIngest(ctx context.Context, p models.IntegrationPrincipal, in models.IngestCallInput, idempotency string) (models.IngestItem, bool, error) {
	if err := applyAIMode(&in, p); err != nil {
		return models.IngestItem{}, false, err
	}
	if err := validateIngestInput(in, true, idempotency); err != nil {
		return models.IngestItem{}, false, err
	}
	if err := mediafetch.ValidateURL(in.RecordingURL); err != nil {
		return models.IngestItem{}, false, err
	}
	canonical, err := json.Marshal(in)
	if err != nil {
		return models.IngestItem{}, false, models.ErrInvalidBillingInput
	}
	hash := sha256.Sum256(append([]byte("POST\n/ingest/calls\n"+p.ApplicationUUID.String()+"\n"), canonical...))
	// Repository allocates the authoritative row UUID; bind ciphertext to the
	// application when row identity is not known at the service boundary.
	locator, err := s.cipher.Encrypt([]byte(strings.TrimSpace(in.RecordingURL)), "ingest_items/application/"+p.ApplicationUUID.String()+"/recording_locator")
	if err != nil {
		return models.IngestItem{}, false, err
	}
	return s.repo.AcceptURLIngest(ctx, p, in, strings.TrimSpace(idempotency), hash, locator)
}

func applyAIMode(in *models.IngestCallInput, p models.IntegrationPrincipal) error {
	if in.AIMode == "" {
		if p.Environment == "sandbox" {
			in.AIMode = "mock"
		} else {
			in.AIMode = "real"
		}
	}
	if p.Environment == "production" && in.AIMode != "real" {
		return models.ErrInvalidBillingInput
	}
	if p.Environment == "sandbox" && in.AIMode != "mock" {
		return models.ErrInvalidBillingInput
	}
	if in.AIMode != "mock" && in.AIMode != "real" {
		return models.ErrInvalidBillingInput
	}
	return nil
}

func validateIngestInput(in models.IngestCallInput, requireURL bool, idempotency string) error {
	if (in.SchemaVersion != 1 && in.SchemaVersion != 2) || strings.TrimSpace(idempotency) == "" || len(idempotency) > 200 || strings.TrimSpace(in.ExternalEventID) == "" || len(in.ExternalEventID) > 200 || strings.TrimSpace(in.ExternalCallID) == "" || len(in.ExternalCallID) > 200 || strings.TrimSpace(in.Title) == "" || len(in.Title) > 500 || len(in.OriginalFilename) > 255 || len(in.Participants) > 100 {
		return models.ErrInvalidBillingInput
	}
	if in.SchemaVersion == 1 && in.Destination != nil {
		return models.ErrInvalidBillingInput
	}
	if in.Destination != nil && in.Destination.Scope != "personal" && in.Destination.Scope != "company" && in.Destination.Scope != "department" {
		return models.ErrInvalidBillingInput
	}
	if in.InstructionMode != "" && in.InstructionMode != "folder" && in.InstructionMode != "scope_and_folder" {
		return models.ErrInvalidBillingInput
	}
	if requireURL != (strings.TrimSpace(in.RecordingURL) != "") {
		return models.ErrInvalidBillingInput
	}
	for _, participant := range in.Participants {
		if len(participant) > 10 {
			return models.ErrInvalidBillingInput
		}
		for key, value := range participant {
			if len(key) > 64 || len(value) > 500 {
				return models.ErrInvalidBillingInput
			}
		}
	}
	encoded, err := json.Marshal(in.Metadata)
	if err != nil || len(encoded) > 64<<10 || !validMetadataShape(in.Metadata, 0) {
		return models.ErrInvalidBillingInput
	}
	return nil
}

func validMetadataShape(value any, depth int) bool {
	if depth > 6 {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > 100 {
			return false
		}
		for key, child := range typed {
			if len(key) > 128 || !validMetadataShape(child, depth+1) {
				return false
			}
		}
	case []any:
		if len(typed) > 100 {
			return false
		}
		for _, child := range typed {
			if !validMetadataShape(child, depth+1) {
				return false
			}
		}
	case string:
		return len(typed) <= 4096
	case nil, bool, float64:
		return true
	default:
		return false
	}
	return true
}
func (s *Service) GetIngest(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IngestItem, error) {
	return s.repo.GetIngest(ctx, id, p)
}
func (s *Service) ListIngest(ctx context.Context, connection, actor uuid.UUID, limit, offset int) ([]models.IngestItem, int, error) {
	return s.repo.ListIngest(ctx, connection, actor, limit, offset)
}
func (s *Service) RetryIngest(ctx context.Context, id, actor uuid.UUID) (models.IngestItem, error) {
	return s.repo.RetryIngest(ctx, id, actor)
}
func (s *Service) CancelIngest(ctx context.Context, id, actor uuid.UUID) (models.IngestItem, error) {
	return s.repo.CancelIngest(ctx, id, actor)
}
func (s *Service) ListAudit(ctx context.Context, connection, actor uuid.UUID, limit, offset int) ([]models.IntegrationAuditEvent, int, error) {
	return s.repo.ListAudit(ctx, connection, actor, limit, offset)
}

func (s *Service) CreateWebhook(ctx context.Context, app, connection, actor uuid.UUID, name, url string, eventTypes []string) (models.WebhookEndpoint, string, error) {
	if err := mediafetch.ValidateURL(url); err != nil {
		return models.WebhookEndpoint{}, "", models.ErrRecordingURLForbidden
	}
	allowed := map[string]bool{"ingest.accepted": true, "ingest.completed": true, "ingest.failed": true}
	for _, value := range eventTypes {
		if !allowed[value] {
			return models.WebhookEndpoint{}, "", models.ErrInvalidBillingInput
		}
	}
	return s.repo.CreateWebhook(ctx, app, connection, actor, strings.TrimSpace(name), strings.TrimSpace(url), eventTypes)
}
func (s *Service) ListWebhooks(ctx context.Context, connection, actor uuid.UUID) ([]models.WebhookEndpoint, error) {
	return s.repo.ListWebhooks(ctx, connection, actor)
}
func (s *Service) RevokeWebhook(ctx context.Context, id, actor uuid.UUID) error {
	return s.repo.RevokeWebhook(ctx, id, actor)
}
func (s *Service) ListDeliveries(ctx context.Context, connection, actor uuid.UUID) ([]models.WebhookDelivery, error) {
	return s.repo.ListDeliveries(ctx, connection, actor)
}
func (s *Service) QueueWebhookTest(ctx context.Context, connection, actor uuid.UUID) (uuid.UUID, error) {
	return s.repo.QueueWebhookTest(ctx, connection, actor)
}
func (s *Service) ListDestinations(ctx context.Context, p models.IntegrationPrincipal) ([]models.IntegrationDestination, error) {
	return s.repo.ListDestinations(ctx, p)
}
func (s *Service) ListFolders(ctx context.Context, p models.IntegrationPrincipal, scope string, company, department uuid.UUID) ([]models.IntegrationFolder, error) {
	return s.repo.ListFolders(ctx, p, scope, company, department)
}
func (s *Service) GetCall(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IntegrationCallView, error) {
	return s.repo.GetCall(ctx, p, id)
}
func (s *Service) GetTranscription(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IntegrationTranscriptionView, error) {
	return s.repo.GetTranscription(ctx, p, id)
}
func (s *Service) GetAnalysis(ctx context.Context, p models.IntegrationPrincipal, id uuid.UUID) (models.IntegrationAnalysisView, error) {
	return s.repo.GetAnalysis(ctx, p, id)
}
