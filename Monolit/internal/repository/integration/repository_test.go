//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	"verbatrace/monolit/internal/integrationcrypto"
	"verbatrace/monolit/internal/models"
	billingrepo "verbatrace/monolit/internal/repository/billing"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type RepositorySuite struct {
	suite.Suite
	ctx     context.Context
	db      *sql.DB
	repo    *Repository
	billing *billingrepo.Repository
	cipher  *integrationcrypto.Cipher
}

func (s *RepositorySuite) SetupSuite() {
	s.ctx = context.Background()
	s.db = repositorytest.OpenTestDB(s.T())
	repositorytest.RunMigrations(s.T(), s.db)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	s.cipher, _ = integrationcrypto.NewFromBase64(key, 1)
}
func (s *RepositorySuite) SetupTest() {
	repositorytest.TruncateTables(s.T(), s.db)
	s.repo = NewRepository(s.db, s.cipher)
	s.billing = billingrepo.NewRepository(s.db)
}
func TestRepositorySuite(t *testing.T) { suite.Run(t, new(RepositorySuite)) }

func (s *RepositorySuite) TestConnectionIngestDedupClaimAndOutbox() {
	user := s.createUser("integration-runtime@example.com")
	app, err := s.billing.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{OwnerType: "user", OwnerUUID: user, CreatedByUserUUID: user, Name: "Runtime", Environment: "sandbox", Capabilities: []string{"calls:write", "calls:read"}})
	s.Require().NoError(err)
	connections, err := s.repo.ListConnections(s.ctx, app.ID, user)
	s.Require().NoError(err)
	s.Require().Len(connections, 1)
	connection := connections[0]
	updatedConnection, err := s.repo.UpdateConnection(s.ctx, models.UpdateIntegrationConnectionInput{ConnectionID: connection.ID, ActorID: user, Name: "Updated runtime", DisablePolicy: "cancel", Settings: []byte(`{"schema_version":1,"source":"test","inherit_scope_instructions":true}`), ExpectedLockVersion: connection.LockVersion})
	s.Require().NoError(err)
	s.Require().Equal("Updated runtime", updatedConnection.Name)
	s.Require().Equal(connection.LockVersion+1, updatedConnection.LockVersion)
	s.Require().Equal(connection.SettingsVersion+1, updatedConnection.SettingsVersion)
	_, err = s.repo.UpdateConnection(s.ctx, models.UpdateIntegrationConnectionInput{ConnectionID: connection.ID, ActorID: user, Name: "Stale", DisablePolicy: "pause", Settings: []byte(`{"schema_version":1}`), ExpectedLockVersion: connection.LockVersion})
	s.Require().ErrorIs(err, models.ErrIntegrationConflict)
	connection = updatedConnection
	p := models.IntegrationPrincipal{ApplicationUUID: app.ID, ConnectionUUID: connection.ID, BillingAccountUUID: app.BillingAccountUUID, ServiceAccountUUID: uuid.New(), Environment: "sandbox", Scopes: []string{"calls:write"}}
	in := models.IngestCallInput{SchemaVersion: 1, ExternalEventID: "evt-1", ExternalCallID: "call-1", Title: "Client call", RecordingURL: "https://media.example.test/call.mp3", OriginalFilename: "call.mp3", InstructionMode: "scope_and_folder"}
	hash := sha256.Sum256([]byte("stable"))
	locator, err := s.cipher.Encrypt([]byte(in.RecordingURL), "ingest_items/application/"+app.ID.String()+"/recording_locator")
	s.Require().NoError(err)
	first, dedup, err := s.repo.AcceptURLIngest(s.ctx, p, in, "idem-1", hash, locator)
	s.Require().NoError(err)
	s.Require().False(dedup)
	s.Require().Equal("mock", first.AIMode)
	s.Require().Equal("sandbox", first.BillingEnvironment)
	s.Require().True(first.InheritScopeInstructions)
	realInput := in
	realInput.ExternalEventID = "evt-real"
	realInput.ExternalCallID = "call-real"
	realInput.AIMode = "real"
	realHash := sha256.Sum256([]byte("stable-real"))
	realItem, _, err := s.repo.AcceptURLIngest(s.ctx, p, realInput, "idem-real", realHash, locator)
	s.Require().NoError(err)
	s.Require().Equal("real", realItem.AIMode)
	s.Require().Equal("production", realItem.BillingEnvironment)
	auditEvents, totalAuditEvents, err := s.repo.ListAudit(s.ctx, connection.ID, user, 10, 0)
	s.Require().NoError(err)
	s.Require().Equal(len(auditEvents), totalAuditEvents)
	s.Require().NotEmpty(auditEvents)
	second, dedup, err := s.repo.AcceptURLIngest(s.ctx, p, in, "idem-1", hash, locator)
	s.Require().NoError(err)
	s.Require().True(dedup)
	s.Require().Equal(first.ID, second.ID)
	other := sha256.Sum256([]byte("changed"))
	_, _, err = s.repo.AcceptURLIngest(s.ctx, p, in, "idem-1", other, locator)
	s.Require().ErrorIs(err, models.ErrIntegrationConflict)
	claimed, err := s.repo.ClaimIngest(s.ctx, "worker-test", time.Minute)
	s.Require().NoError(err)
	s.Require().Equal(first.ID, claimed.ID)
	s.Require().Equal(1, claimed.Attempts)
	s.Require().Equal("processing", claimed.Status)
	s.Require().True(claimed.InheritScopeInstructions)
	var outbox int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM integration_outbox WHERE aggregate_uuid=$1 AND event_type='ingest.accepted'`, first.ID).Scan(&outbox))
	s.Require().Equal(1, outbox)
}

func (s *RepositorySuite) TestWebhookSecretEncryptedAndDeliveryClaimed() {
	user := s.createUser("webhook-runtime@example.com")
	app, err := s.billing.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{OwnerType: "user", OwnerUUID: user, CreatedByUserUUID: user, Name: "Hooks", Environment: "sandbox", Capabilities: []string{"webhooks:manage"}})
	s.Require().NoError(err)
	connections, _ := s.repo.ListConnections(s.ctx, app.ID, user)
	endpoint, secret, err := s.repo.CreateWebhook(s.ctx, app.ID, connections[0].ID, user, "Events", "https://hooks.example.test/verbatrace", []string{"ingest.accepted"})
	s.Require().NoError(err)
	s.Require().NotEmpty(secret)
	var stored string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT encode(signing_secret_ciphertext,'hex') FROM integration_webhook_endpoints WHERE webhook_endpoint_uuid=$1`, endpoint.ID).Scan(&stored))
	s.Require().NotContains(stored, secret)
	tx, err := s.db.BeginTx(s.ctx, nil)
	s.Require().NoError(err)
	aggregate := uuid.New()
	s.Require().NoError(outbox(s.ctx, tx, app.ID, connections[0].ID, "ingest.accepted", aggregate, map[string]any{"status": "received"}))
	s.Require().NoError(tx.Commit())
	claimed, err := s.repo.ClaimWebhook(s.ctx, "hook-worker", time.Minute)
	s.Require().NoError(err)
	s.Require().Len(claimed.Targets, 1)
	s.Require().Equal("https://hooks.example.test/verbatrace", claimed.Targets[0].URL)
	s.Require().Equal(secret, claimed.Targets[0].SigningSecret)
}

func (s *RepositorySuite) TestSandboxIngestUsesOneReadOnlyTestFolder() {
	user := s.createUser("integration-folders@example.com")
	app, err := s.billing.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{OwnerType: "user", OwnerUUID: user, CreatedByUserUUID: user, Name: "Folders", Environment: "sandbox", Capabilities: []string{"calls:write", "destinations:read"}})
	s.Require().NoError(err)
	connections, err := s.repo.ListConnections(s.ctx, app.ID, user)
	s.Require().NoError(err)
	s.Require().Len(connections, 1)
	connection := connections[0]
	p := models.IntegrationPrincipal{ApplicationUUID: app.ID, ConnectionUUID: connection.ID, BillingAccountUUID: app.BillingAccountUUID, ServiceAccountUUID: uuid.New(), Environment: "sandbox"}
	locator, err := s.cipher.Encrypt([]byte("https://media.example.test/call.mp3"), "ingest_items/application/"+app.ID.String()+"/recording_locator")
	s.Require().NoError(err)

	firstInput := models.IngestCallInput{SchemaVersion: 2, ExternalEventID: "folder-evt-1", ExternalCallID: "folder-call-1", Title: "First", RecordingURL: "https://media.example.test/call.mp3"}
	first, _, err := s.repo.AcceptURLIngest(s.ctx, p, firstInput, "folder-idem-1", sha256.Sum256([]byte("one")), locator)
	s.Require().NoError(err)
	s.Require().Equal("personal", first.DestinationScope)
	s.Require().True(first.DestinationFolderID.Valid)
	s.Require().Equal("system_sandbox", first.PlacementSource)
	s.Require().False(first.InheritScopeInstructions)

	secondInput := firstInput
	secondInput.ExternalEventID, secondInput.ExternalCallID = "folder-evt-2", "folder-call-2"
	second, _, err := s.repo.AcceptURLIngest(s.ctx, p, secondInput, "folder-idem-2", sha256.Sum256([]byte("two")), locator)
	s.Require().NoError(err)
	s.Require().Equal(first.DestinationFolderID, second.DestinationFolderID)
	var systemFolders int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM call_folders WHERE user_uuid=$1 AND system_type='sandbox_test' AND deleted_at IS NULL`, user).Scan(&systemFolders))
	s.Require().Equal(1, systemFolders)

	customFolder := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `INSERT INTO call_folders(folder_uuid,scope,user_uuid,name,created_by_user_uuid) VALUES($1,'personal',$2,'CRM',$2)`, customFolder, user)
	s.Require().NoError(err)
	overrideInput := firstInput
	overrideInput.ExternalEventID, overrideInput.ExternalCallID = "folder-evt-3", "folder-call-3"
	overrideInput.Destination = &models.IngestDestination{Scope: "personal", FolderID: customFolder}
	accepted, _, err := s.repo.AcceptURLIngest(s.ctx, p, overrideInput, "folder-idem-3", sha256.Sum256([]byte("three")), locator)
	s.Require().NoError(err)
	s.Require().Equal(first.DestinationFolderID.UUID, accepted.DestinationFolderID.UUID)
	s.Require().Equal("system_sandbox", accepted.PlacementSource)
}

func (s *RepositorySuite) createUser(email string) uuid.UUID {
	id := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `WITH account AS (INSERT INTO users(user_uuid,email,password_hash,role,created_at) VALUES($1,$2,'hash','user',now()) RETURNING user_uuid) INSERT INTO user_profiles(user_uuid,full_name,full_surname,username) SELECT user_uuid,'Dmitry','Mukhachev',$3 FROM account`, id, email, "u"+id.String()[:8])
	s.Require().NoError(err)
	return id
}
