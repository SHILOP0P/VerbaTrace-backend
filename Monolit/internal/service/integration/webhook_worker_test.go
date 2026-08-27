package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type webhookRepositoryStub struct {
	event      models.ClaimedWebhookEvent
	status     string
	errorCode  string
	httpStatus *int
}

func (r *webhookRepositoryStub) ClaimWebhook(context.Context, string, time.Duration) (models.ClaimedWebhookEvent, error) {
	return r.event, nil
}
func (r *webhookRepositoryStub) RecordWebhookDelivery(_ context.Context, _ models.ClaimedWebhookEvent, _ models.WebhookTarget, status string, httpStatus *int, _ int64, _ int64, _ []byte, errorCode string, _ time.Duration) error {
	r.status = status
	r.httpStatus = httpStatus
	r.errorCode = errorCode
	return nil
}

func TestWebhookWorkerSignsStableEnvelope(t *testing.T) {
	secret := "whsec_test"
	var body, signature, timestamp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		signature = r.Header.Get("X-VerbaTrace-Signature")
		timestamp = r.Header.Get("X-VerbaTrace-Timestamp")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	repo := &webhookRepositoryStub{event: models.ClaimedWebhookEvent{OutboxID: uuid.New(), EventID: uuid.New(), ApplicationID: uuid.New(), ConnectionID: uuid.New(), AggregateID: uuid.New(), EventType: "ingest.completed", Payload: []byte(`{"status":"completed"}`), CreatedAt: time.Now().UTC(), Targets: []models.WebhookTarget{{EndpointID: uuid.New(), URL: server.URL, SigningSecret: secret, Attempt: 1}}}}
	worker := NewWebhookWorker(repo, nil)
	worker.client = server.Client()
	if err := worker.runOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + body))
	want := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(want)) {
		t.Fatalf("signature=%q want=%q", signature, want)
	}
	if repo.status != "succeeded" || repo.httpStatus == nil || *repo.httpStatus != 204 {
		t.Fatalf("delivery=%s http=%v", repo.status, repo.httpStatus)
	}
}

func TestWebhookWorkerStopsAfterRetryLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	repo := &webhookRepositoryStub{event: models.ClaimedWebhookEvent{EventID: uuid.New(), EventType: "ingest.failed", Payload: []byte(`{}`), Targets: []models.WebhookTarget{{EndpointID: uuid.New(), URL: server.URL, SigningSecret: "secret", Attempt: 10}}}}
	worker := NewWebhookWorker(repo, nil)
	worker.client = server.Client()
	if err := worker.runOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.status != "permanent_failed" || repo.errorCode != "retry_exhausted" {
		t.Fatalf("delivery=%s code=%s", repo.status, repo.errorCode)
	}
}

func TestParseRetryAfterCapsUntrustedDelay(t *testing.T) {
	now := time.Now().UTC()
	if got := parseRetryAfter("120", now); got != 2*time.Minute {
		t.Fatalf("seconds delay=%s", got)
	}
	if got := parseRetryAfter("999999", now); got != 24*time.Hour {
		t.Fatalf("capped delay=%s", got)
	}
	if got := parseRetryAfter("invalid", now); got != 0 {
		t.Fatalf("invalid delay=%s", got)
	}
}
