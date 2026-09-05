package privacy

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) RunProviderCleanupWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processNextProviderCleanup(ctx)
			}
		}
	}()
	return done
}

func (s *Service) processNextProviderCleanup(ctx context.Context) {
	if s.cleaner == nil {
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	var id uuid.UUID
	var jobID string
	err = tx.QueryRowContext(ctx, `SELECT provider_attempt_uuid,provider_job_id FROM transcription_provider_attempts
		WHERE status IN ('delete_pending','delete_failed') AND available_at<=now() AND provider_job_id IS NOT NULL
		ORDER BY available_at,provider_attempt_uuid FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &jobID)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE transcription_provider_attempts SET status='deleting',attempts=attempts+1,locked_at=now(),locked_by='privacy-provider-cleanup',updated_at=now() WHERE provider_attempt_uuid=$1`, id); err != nil {
		return
	}
	if err = tx.Commit(); err != nil {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = s.cleaner.DeleteArtifact(cleanupCtx, jobID)
	cancel()
	if err == nil {
		_, _ = s.db.ExecContext(context.Background(), `UPDATE transcription_provider_attempts SET status='deleted',deleted_at=now(),locked_at=NULL,locked_by=NULL,last_error_code=NULL,last_error_message_safe=NULL,updated_at=now() WHERE provider_attempt_uuid=$1`, id)
		return
	}
	_, _ = s.db.ExecContext(context.Background(), `UPDATE transcription_provider_attempts SET status='delete_failed',available_at=now()+LEAST(attempts,24)*interval '1 hour',locked_at=NULL,locked_by=NULL,last_error_code='provider_cleanup_failed',last_error_message_safe='Не удалось удалить транскрипцию у провайдера',updated_at=now() WHERE provider_attempt_uuid=$1`, id)
	s.log.Error(ctx, "provider transcript cleanup failed", zap.String("provider_attempt_id", id.String()), zap.String("error_kind", safeCleanupError(err)))
}

func safeCleanupError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.ToLower(err.Error())
	if strings.Contains(value, "timeout") || strings.Contains(value, "deadline") {
		return "timeout"
	}
	return "provider_error"
}
