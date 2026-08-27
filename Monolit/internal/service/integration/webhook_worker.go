package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/mediafetch"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type WebhookRepository interface {
	ClaimWebhook(context.Context, string, time.Duration) (models.ClaimedWebhookEvent, error)
	RecordWebhookDelivery(context.Context, models.ClaimedWebhookEvent, models.WebhookTarget, string, *int, int64, int64, []byte, string, time.Duration) error
}
type WebhookWorker struct {
	repo   WebhookRepository
	client *http.Client
	id     string
	log    logger.Logger
}

func NewWebhookWorker(repo WebhookRepository, log logger.Logger) *WebhookWorker {
	if log == nil {
		log = logger.NewNop()
	}
	return &WebhookWorker{repo: repo, client: mediafetch.NewWebhookClient(15 * time.Second), id: "webhook-" + uuid.NewString(), log: log}
}
func (w *WebhookWorker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if err := w.runOne(ctx); err != nil && !errors.Is(err, models.ErrIngestNotFound) && ctx.Err() == nil {
				w.log.Error(ctx, "webhook worker iteration failed", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}
func (w *WebhookWorker) runOne(ctx context.Context) error {
	event, err := w.repo.ClaimWebhook(ctx, w.id, 5*time.Minute)
	if err != nil {
		return err
	}
	for _, target := range event.Targets {
		envelope := map[string]any{"id": event.EventID, "type": event.EventType, "occurred_at": event.CreatedAt, "data": json.RawMessage(event.Payload)}
		body, _ := json.Marshal(envelope)
		timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(target.SigningSecret))
		_, _ = mac.Write([]byte(timestamp + "." + string(body)))
		signature := "v1=" + hex.EncodeToString(mac.Sum(nil))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "VerbaTrace-Webhooks/1.0")
		req.Header.Set("X-VerbaTrace-Event-ID", event.EventID.String())
		req.Header.Set("X-VerbaTrace-Timestamp", timestamp)
		req.Header.Set("X-VerbaTrace-Signature", signature)
		started := time.Now()
		resp, callErr := w.client.Do(req)
		latency := time.Since(started).Milliseconds()
		status := "retryable_failed"
		errorCode := "network_error"
		var httpStatus *int
		var size int64
		var responseHash []byte
		var retryAfter time.Duration
		if callErr == nil {
			code := resp.StatusCode
			httpStatus = &code
			hash := sha256.New()
			size, _ = io.Copy(io.MultiWriter(io.Discard, hash), io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			responseHash = hash.Sum(nil)
			switch {
			case code >= 200 && code < 300:
				status = "succeeded"
				errorCode = ""
			case code == 408 || code == 425 || code == 429 || code >= 500:
				status = "retryable_failed"
				errorCode = "http_retryable"
				retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now().UTC())
			default:
				status = "permanent_failed"
				errorCode = "http_permanent"
			}
		}
		if status == "retryable_failed" && target.Attempt >= 10 {
			status = "permanent_failed"
			errorCode = "retry_exhausted"
		}
		if err = w.repo.RecordWebhookDelivery(ctx, event, target, status, httpStatus, latency, size, responseHash, errorCode, retryAfter); err != nil {
			return err
		}
	}
	return nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > 24*time.Hour {
			return 24 * time.Hour
		}
		return delay
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		delay := at.Sub(now)
		if delay > 24*time.Hour {
			return 24 * time.Hour
		}
		return delay
	}
	return 0
}
