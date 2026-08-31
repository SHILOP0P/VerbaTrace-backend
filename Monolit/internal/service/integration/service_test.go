package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/integrationcrypto"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type uploadRepository struct {
	locator []byte
	input   models.IngestCallInput
	dedup   bool
}

func (*uploadRepository) CreateConnection(context.Context, models.CreateIntegrationConnectionInput) (models.IntegrationConnection, error) {
	return models.IntegrationConnection{}, nil
}
func (*uploadRepository) ListConnections(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationConnection, error) {
	return nil, nil
}
func (*uploadRepository) GetConnection(context.Context, uuid.UUID, uuid.UUID) (models.IntegrationConnection, error) {
	return models.IntegrationConnection{}, nil
}
func (*uploadRepository) ChangeConnectionStatus(context.Context, uuid.UUID, uuid.UUID, string, int64) (models.IntegrationConnection, error) {
	return models.IntegrationConnection{}, nil
}
func (*uploadRepository) UpdateConnection(context.Context, models.UpdateIntegrationConnectionInput) (models.IntegrationConnection, error) {
	return models.IntegrationConnection{}, nil
}
func (*uploadRepository) AcceptURLIngest(context.Context, models.IntegrationPrincipal, models.IngestCallInput, string, [32]byte, []byte) (models.IngestItem, bool, error) {
	return models.IngestItem{}, false, nil
}
func (*uploadRepository) GetIngest(context.Context, uuid.UUID, models.IntegrationPrincipal) (models.IngestItem, error) {
	return models.IngestItem{}, nil
}
func (*uploadRepository) ListIngest(context.Context, uuid.UUID, uuid.UUID, int, int) ([]models.IngestItem, int, error) {
	return nil, 0, nil
}
func (*uploadRepository) RetryIngest(context.Context, uuid.UUID, uuid.UUID) (models.IngestItem, error) {
	return models.IngestItem{}, nil
}
func (*uploadRepository) CancelIngest(context.Context, uuid.UUID, uuid.UUID) (models.IngestItem, error) {
	return models.IngestItem{}, nil
}
func (*uploadRepository) ListAudit(context.Context, uuid.UUID, uuid.UUID, int, int) ([]models.IntegrationAuditEvent, int, error) {
	return nil, 0, nil
}
func (*uploadRepository) CreateWebhook(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, []string) (models.WebhookEndpoint, string, error) {
	return models.WebhookEndpoint{}, "", nil
}
func (*uploadRepository) ListWebhooks(context.Context, uuid.UUID, uuid.UUID) ([]models.WebhookEndpoint, error) {
	return nil, nil
}
func (*uploadRepository) RevokeWebhook(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (*uploadRepository) ListDeliveries(context.Context, uuid.UUID, uuid.UUID) ([]models.WebhookDelivery, error) {
	return nil, nil
}
func (*uploadRepository) QueueWebhookTest(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (*uploadRepository) ReplayWebhookDelivery(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (*uploadRepository) ListDestinations(context.Context, models.IntegrationPrincipal) ([]models.IntegrationDestination, error) {
	return nil, nil
}
func (*uploadRepository) ListFolders(context.Context, models.IntegrationPrincipal, string, uuid.UUID, uuid.UUID) ([]models.IntegrationFolder, error) {
	return nil, nil
}
func (*uploadRepository) GetCall(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationCallView, error) {
	return models.IntegrationCallView{}, nil
}
func (*uploadRepository) GetCallBySourceRef(context.Context, models.IntegrationPrincipal, string) (models.IntegrationCallView, error) {
	return models.IntegrationCallView{}, nil
}
func (*uploadRepository) ListCalls(context.Context, models.IntegrationPrincipal, models.IntegrationCallFilter) ([]models.IntegrationCallView, error) {
	return nil, nil
}
func (*uploadRepository) GetTranscription(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationTranscriptionView, error) {
	return models.IntegrationTranscriptionView{}, nil
}
func (*uploadRepository) GetAnalysis(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationAnalysisView, error) {
	return models.IntegrationAnalysisView{}, nil
}
func (*uploadRepository) GetUsage(context.Context, models.IntegrationPrincipal, time.Time) (models.IntegrationUsageView, error) {
	return models.IntegrationUsageView{}, nil
}

func (r *uploadRepository) AcceptUploadIngest(_ context.Context, _ models.IntegrationPrincipal, in models.IngestCallInput, _ string, _ [32]byte, locator []byte) (models.IngestItem, bool, error) {
	r.locator, r.input = locator, in
	return models.IngestItem{ID: uuid.New()}, r.dedup, nil
}

func TestAcceptUploadIngestStagesOpaqueFile(t *testing.T) {
	dir := t.TempDir()
	cipher, err := integrationcrypto.NewFromBase64(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	repo := &uploadRepository{}
	service := NewService(repo, nil, cipher, dir)
	principal := models.IntegrationPrincipal{ApplicationUUID: uuid.New()}
	_, dedup, err := service.AcceptUploadIngest(context.Background(), principal, models.IngestCallInput{SchemaVersion: 1, ExternalEventID: "evt", ExternalCallID: "call", Title: "Call"}, "idem", bytes.NewReader([]byte("media")))
	if err != nil || dedup {
		t.Fatalf("AcceptUploadIngest() = dedup %v, err %v", dedup, err)
	}
	if repo.input.RecordingURL != "" {
		t.Fatal("upload persisted a client-controlled recording URL")
	}
	name, err := cipher.Decrypt(repo.locator, "ingest_items/application/"+principal.ApplicationUUID.String()+"/recording_locator")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := readDirNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != string(name) {
		t.Fatalf("staged entries = %v, locator = %q", entries, name)
	}
}

func TestApplyAIModeSeparatesSandboxMockFromBilledReal(t *testing.T) {
	t.Run("sandbox defaults to mock", func(t *testing.T) {
		input := models.IngestCallInput{}
		err := applyAIMode(&input, models.IntegrationPrincipal{Environment: "sandbox"})
		if err != nil || input.AIMode != "mock" {
			t.Fatalf("expected sandbox mock, mode=%q err=%v", input.AIMode, err)
		}
	})
	t.Run("sandbox never accepts real AI", func(t *testing.T) {
		input := models.IngestCallInput{AIMode: "real"}
		err := applyAIMode(&input, models.IntegrationPrincipal{Environment: "sandbox"})
		if !errors.Is(err, models.ErrInvalidBillingInput) {
			t.Fatalf("expected real mode rejection, got %v", err)
		}
	})
	t.Run("production cannot request mock", func(t *testing.T) {
		input := models.IngestCallInput{AIMode: "mock"}
		if err := applyAIMode(&input, models.IntegrationPrincipal{Environment: "production"}); !errors.Is(err, models.ErrInvalidBillingInput) {
			t.Fatalf("expected invalid input, got %v", err)
		}
	})
}

func TestValidateIngestInstructionMode(t *testing.T) {
	base := models.IngestCallInput{SchemaVersion: 2, ExternalEventID: "evt", ExternalCallID: "call", Title: "Call"}
	for _, mode := range []string{"", "folder", "scope_and_folder"} {
		input := base
		input.InstructionMode = mode
		if err := validateIngestInput(input, false, "idem"); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	input := base
	input.InstructionMode = "all"
	if err := validateIngestInput(input, false, "idem"); !errors.Is(err, models.ErrInvalidBillingInput) {
		t.Fatalf("unsupported mode accepted: %v", err)
	}
}

func TestAcceptUploadIngestDeletesDuplicateStagingFile(t *testing.T) {
	dir := t.TempDir()
	cipher, _ := integrationcrypto.NewFromBase64(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)), 1)
	repo := &uploadRepository{dedup: true}
	service := NewService(repo, nil, cipher, dir)
	_, dedup, err := service.AcceptUploadIngest(context.Background(), models.IntegrationPrincipal{ApplicationUUID: uuid.New()}, models.IngestCallInput{SchemaVersion: 1, ExternalEventID: "evt", ExternalCallID: "call", Title: "Call"}, "idem", bytes.NewReader([]byte("media")))
	if err != nil || !dedup {
		t.Fatalf("AcceptUploadIngest() = dedup %v, err %v", dedup, err)
	}
	entries, _ := readDirNames(dir)
	if len(entries) != 0 {
		t.Fatalf("duplicate left orphan files: %v", entries)
	}
}
