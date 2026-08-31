package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) CreateWebhook(ctx context.Context, appID, connectionID, actorID uuid.UUID, name, url string, eventTypes []string) (models.WebhookEndpoint, string, error) {
	if err := r.authorizeApplication(ctx, appID, actorID); err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	if len(eventTypes) == 0 {
		return models.WebhookEndpoint{}, "", models.ErrInvalidBillingInput
	}
	var belongs bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM integration_connections WHERE connection_uuid=$1 AND application_uuid=$2 AND status<>'revoked')`, connectionID, appID).Scan(&belongs); err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	if !belongs {
		return models.WebhookEndpoint{}, "", models.ErrIntegrationNotFound
	}
	id, _ := uuid.NewV7()
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	urlCipher, err := r.cipher.Encrypt([]byte(url), "integration_webhook_endpoints/"+id.String()+"/url")
	if err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	secretCipher, err := r.cipher.Encrypt([]byte(secret), "integration_webhook_endpoints/"+id.String()+"/signing_secret")
	if err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_webhook_endpoints(webhook_endpoint_uuid,application_uuid,connection_uuid,name,url_ciphertext,key_version,signing_secret_ciphertext,signing_key_version,status,event_types) VALUES($1,$2,$3,$4,$5,$6,$7,$6,'active',$8)`, id, appID, connectionID, name, urlCipher, r.cipher.Version(), secretCipher, eventTypes)
	if err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	if err = audit(ctx, tx, appID, uuid.NullUUID{UUID: connectionID, Valid: true}, "user", uuid.NullUUID{UUID: actorID, Valid: true}, "webhook.created", "webhook_endpoint", id, map[string]any{"event_types": eventTypes}); err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return models.WebhookEndpoint{}, "", err
	}
	now := time.Now().UTC()
	return models.WebhookEndpoint{ID: id, ApplicationID: appID, ConnectionID: uuid.NullUUID{UUID: connectionID, Valid: true}, Name: name, Status: "active", EventTypes: eventTypes, LockVersion: 1, CreatedAt: now, UpdatedAt: now}, secret, nil
}

func (r *Repository) ListWebhooks(ctx context.Context, connectionID, actorID uuid.UUID) ([]models.WebhookEndpoint, error) {
	var appID uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT application_uuid FROM integration_connections WHERE connection_uuid=$1`, connectionID).Scan(&appID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrIntegrationNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = r.authorizeApplication(ctx, appID, actorID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT webhook_endpoint_uuid,application_uuid,connection_uuid,name,status,event_types,lock_version,created_at,updated_at FROM integration_webhook_endpoints WHERE connection_uuid=$1 AND status<>'revoked' ORDER BY created_at DESC`, connectionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []models.WebhookEndpoint
	for rows.Next() {
		var x models.WebhookEndpoint
		if err = rows.Scan(&x.ID, &x.ApplicationID, &x.ConnectionID, &x.Name, &x.Status, &x.EventTypes, &x.LockVersion, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *Repository) RevokeWebhook(ctx context.Context, id, actorID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var app, connection uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT w.application_uuid,w.connection_uuid FROM integration_webhook_endpoints w JOIN developer_applications a USING(application_uuid) WHERE w.webhook_endpoint_uuid=$1 AND w.status<>'revoked' AND (`+actorAccessSQL+`) FOR UPDATE`, id, actorID).Scan(&app, &connection)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrIntegrationNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE integration_webhook_endpoints SET status='revoked',revoked_at=now(),updated_at=now(),lock_version=lock_version+1 WHERE webhook_endpoint_uuid=$1`, id)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, app, uuid.NullUUID{UUID: connection, Valid: true}, "user", uuid.NullUUID{UUID: actorID, Valid: true}, "webhook.revoked", "webhook_endpoint", id, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) QueueWebhookTest(ctx context.Context, connectionID, actorID uuid.UUID) (uuid.UUID, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var appID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT application_uuid FROM integration_connections WHERE connection_uuid=$1 AND status<>'revoked' FOR UPDATE`, connectionID).Scan(&appID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, models.ErrIntegrationNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if err = r.authorizeApplicationTx(ctx, tx, appID, actorID); err != nil {
		return uuid.Nil, err
	}
	eventID, _ := uuid.NewV7()
	payload := map[string]any{"connection_uuid": connectionID, "test": true}
	if err = outbox(ctx, tx, appID, connectionID, "webhook.test", eventID, payload); err != nil {
		return uuid.Nil, err
	}
	if err = audit(ctx, tx, appID, uuid.NullUUID{UUID: connectionID, Valid: true}, "user", uuid.NullUUID{UUID: actorID, Valid: true}, "webhook.test_queued", "connection", connectionID, payload); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(); err != nil {
		return uuid.Nil, err
	}
	return eventID, nil
}

func (r *Repository) ReplayWebhookDelivery(ctx context.Context, deliveryID, actorID uuid.UUID) (uuid.UUID, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var appID, connectionID, aggregateID uuid.UUID
	var eventType string
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT o.application_uuid,o.connection_uuid,o.aggregate_uuid,o.event_type,o.payload
		FROM integration_webhook_deliveries d JOIN integration_outbox o USING(outbox_uuid)
		JOIN developer_applications a ON a.application_uuid=o.application_uuid
		WHERE d.delivery_uuid=$1 AND (`+actorAccessSQL+`) FOR UPDATE OF o`, deliveryID, actorID).Scan(&appID, &connectionID, &aggregateID, &eventType, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, models.ErrForbidden
	}
	if err != nil {
		return uuid.Nil, err
	}
	outboxID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_outbox(outbox_uuid,application_uuid,connection_uuid,event_id,event_type,aggregate_uuid,payload,status) VALUES($1,$2,$3,$4,$5,$6,$7,'pending')`, outboxID, appID, connectionID, eventID, eventType, aggregateID, payload)
	if err != nil {
		return uuid.Nil, err
	}
	if err = audit(ctx, tx, appID, uuid.NullUUID{UUID: connectionID, Valid: true}, "user", uuid.NullUUID{UUID: actorID, Valid: true}, "webhook.delivery_replayed", "webhook_delivery", deliveryID, map[string]any{"replay_event_id": eventID}); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(); err != nil {
		return uuid.Nil, err
	}
	return eventID, nil
}

func (r *Repository) ClaimWebhook(ctx context.Context, worker string, lease time.Duration) (models.ClaimedWebhookEvent, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return models.ClaimedWebhookEvent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var e models.ClaimedWebhookEvent
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT outbox_uuid,event_id,application_uuid,connection_uuid,aggregate_uuid,event_type,payload,created_at FROM integration_outbox WHERE attempts<max_attempts AND available_at<=now() AND (status='pending' OR (status='delivering' AND locked_at<now()-make_interval(secs=>300))) ORDER BY available_at,created_at,outbox_uuid FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&e.OutboxID, &e.EventID, &e.ApplicationID, &e.ConnectionID, &e.AggregateID, &e.EventType, &payload, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, models.ErrIngestNotFound
	}
	if err != nil {
		return e, err
	}
	e.Payload = payload
	_, err = tx.ExecContext(ctx, `UPDATE integration_outbox SET status='delivering',attempts=attempts+1,locked_by=$2,locked_at=now() WHERE outbox_uuid=$1`, e.OutboxID, worker)
	if err != nil {
		return e, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT w.webhook_endpoint_uuid,w.url_ciphertext,w.key_version,w.signing_secret_ciphertext,w.signing_key_version,COALESCE((SELECT max(d.attempt) FROM integration_webhook_deliveries d WHERE d.outbox_uuid=$1 AND d.webhook_endpoint_uuid=w.webhook_endpoint_uuid),0)+1 FROM integration_webhook_endpoints w WHERE w.application_uuid=$2 AND w.status='active' AND (w.connection_uuid IS NULL OR w.connection_uuid=$3) AND ($4='webhook.test' OR $4=ANY(w.event_types))`, e.OutboxID, e.ApplicationID, e.ConnectionID, e.EventType)
	if err != nil {
		return e, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.UUID
		var urlCipher, secretCipher []byte
		var urlVersion, secretVersion, attempt int
		if err = rows.Scan(&id, &urlCipher, &urlVersion, &secretCipher, &secretVersion, &attempt); err != nil {
			return e, err
		}
		if urlVersion != r.cipher.Version() || secretVersion != r.cipher.Version() {
			return e, errors.New("webhook encryption key unavailable")
		}
		rawURL, err := r.cipher.Decrypt(urlCipher, "integration_webhook_endpoints/"+id.String()+"/url")
		if err != nil {
			return e, err
		}
		rawSecret, err := r.cipher.Decrypt(secretCipher, "integration_webhook_endpoints/"+id.String()+"/signing_secret")
		if err != nil {
			return e, err
		}
		e.Targets = append(e.Targets, models.WebhookTarget{EndpointID: id, URL: string(rawURL), SigningSecret: string(rawSecret), Attempt: attempt})
	}
	if err = rows.Err(); err != nil {
		return e, err
	}
	if len(e.Targets) == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE integration_outbox SET status='delivered',delivered_at=now(),locked_at=NULL,locked_by=NULL WHERE outbox_uuid=$1`, e.OutboxID)
		if err != nil {
			return e, err
		}
	}
	if err = tx.Commit(); err != nil {
		return e, err
	}
	return e, nil
}

func (r *Repository) RecordWebhookDelivery(ctx context.Context, event models.ClaimedWebhookEvent, target models.WebhookTarget, status string, httpStatus *int, latency, size int64, responseHash []byte, errorCode string, retryAfter time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO integration_webhook_deliveries(delivery_uuid,outbox_uuid,webhook_endpoint_uuid,attempt,status,http_status,latency_ms,response_size_bytes,response_sha256,error_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(outbox_uuid,webhook_endpoint_uuid,attempt) DO NOTHING`, id, event.OutboxID, target.EndpointID, target.Attempt, status, httpStatus, latency, size, responseHash, nullableString(errorCode))
	if err != nil {
		return err
	}
	var remaining int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM integration_webhook_endpoints w WHERE w.application_uuid=$1 AND w.status='active' AND (w.connection_uuid IS NULL OR w.connection_uuid=$2) AND $3=ANY(w.event_types) AND NOT EXISTS(SELECT 1 FROM integration_webhook_deliveries d WHERE d.outbox_uuid=$4 AND d.webhook_endpoint_uuid=w.webhook_endpoint_uuid AND d.status IN ('succeeded','permanent_failed'))`, event.ApplicationID, event.ConnectionID, event.EventType, event.OutboxID).Scan(&remaining)
	if err != nil {
		return err
	}
	if status == "retryable_failed" {
		delay := time.Minute * time.Duration(1<<min(target.Attempt, 6))
		if retryAfter > delay {
			delay = retryAfter
		}
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		_, err = tx.ExecContext(ctx, `UPDATE integration_outbox SET status='pending',available_at=now()+make_interval(secs=>$2),locked_at=NULL,locked_by=NULL WHERE outbox_uuid=$1`, event.OutboxID, int64(delay/time.Second))
		if err != nil {
			return err
		}
	} else if remaining == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE integration_outbox SET status='delivered',delivered_at=now(),locked_at=NULL,locked_by=NULL WHERE outbox_uuid=$1`, event.OutboxID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) ListDeliveries(ctx context.Context, connectionID, actorID uuid.UUID) ([]models.WebhookDelivery, error) {
	var app uuid.UUID
	if err := r.db.QueryRowContext(ctx, `SELECT application_uuid FROM integration_connections WHERE connection_uuid=$1`, connectionID).Scan(&app); err != nil {
		return nil, models.ErrIntegrationNotFound
	}
	if err := r.authorizeApplication(ctx, app, actorID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT d.delivery_uuid,d.outbox_uuid,d.webhook_endpoint_uuid,d.attempt,d.status,d.http_status,d.latency_ms,d.response_size_bytes,d.error_code,d.created_at FROM integration_webhook_deliveries d JOIN integration_outbox o USING(outbox_uuid) WHERE o.connection_uuid=$1 ORDER BY d.created_at DESC LIMIT 100`, connectionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		var hs sql.NullInt32
		var lat, size sql.NullInt64
		var ec sql.NullString
		if err = rows.Scan(&d.ID, &d.OutboxID, &d.EndpointID, &d.Attempt, &d.Status, &hs, &lat, &size, &ec, &d.CreatedAt); err != nil {
			return nil, err
		}
		if hs.Valid {
			x := int(hs.Int32)
			d.HTTPStatus = &x
		}
		if lat.Valid {
			x := lat.Int64
			d.LatencyMS = &x
		}
		if size.Valid {
			x := size.Int64
			d.ResponseSize = &x
		}
		d.ErrorCode = stringPtr(ec)
		out = append(out, d)
	}
	return out, rows.Err()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
