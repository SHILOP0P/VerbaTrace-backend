package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const callWorkerLock int64 = 0x564552424143414c
const instructionWorkerLock int64 = 0x5645524241494e53

type fileRef struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type Service struct {
	db           *sql.DB
	audio        storage.AudioStorage
	reports      storage.ReportStorage
	instructions storage.InstructionStorage
	log          logger.Logger
	now          func() time.Time
}

func NewService(db *sql.DB, audio storage.AudioStorage, reports storage.ReportStorage, instructions storage.InstructionStorage, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, audio: audio, reports: reports, instructions: instructions, log: log, now: func() time.Time { return time.Now().UTC() }}
}

type Worker struct {
	service      *Service
	interval     time.Duration
	batch        int
	instructions bool
}

func NewCallWorker(s *Service, interval time.Duration, batch int) *Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batch <= 0 {
		batch = 100
	}
	return &Worker{service: s, interval: interval, batch: batch}
}
func NewInstructionWorker(s *Service, interval time.Duration, batch int) *Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batch <= 0 {
		batch = 50
	}
	return &Worker{service: s, interval: interval, batch: batch, instructions: true}
}
func (w *Worker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(w.interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				w.RunOnce(ctx)
				timer.Reset(w.interval)
			}
		}
	}()
	return done
}
func (w *Worker) RunOnce(ctx context.Context) {
	if w.instructions {
		w.runInstructions(ctx)
	} else {
		w.runCalls(ctx)
	}
}

func (w *Worker) lock(ctx context.Context, key int64) (bool, error) {
	var ok bool
	err := w.service.db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok)
	return ok, err
}
func (w *Worker) unlock(ctx context.Context, key int64) {
	_, _ = w.service.db.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, key)
}
func audit(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, run, deletion uuid.UUID, entity string, entityID uuid.UUID, event string, count, bytes int64, meta any) error {
	raw, _ := json.Marshal(meta)
	_, err := tx.ExecContext(ctx, `INSERT INTO retention_audit_events(event_uuid,run_uuid,deletion_uuid,entity_type,entity_uuid,event_type,item_count,byte_count,metadata_safe) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, uuid.New(), run, nullableUUID(deletion), entity, nullableUUID(entityID), event, nullableInt(count), nullableInt(bytes), raw)
	return err
}
func nullableUUID(v uuid.UUID) any {
	if v == uuid.Nil {
		return nil
	}
	return v
}
func nullableInt(v int64) any {
	if v < 0 {
		return nil
	}
	return v
}

func (w *Worker) runCalls(ctx context.Context) {
	ok, err := w.lock(ctx, callWorkerLock)
	if err != nil || !ok {
		return
	}
	defer w.unlock(context.Background(), callWorkerLock)
	run := uuid.New()
	_ = audit(ctx, w.service.db, run, uuid.Nil, "worker", uuid.Nil, "call_worker_run_started", -1, -1, map[string]any{"batch": w.batch})
	w.resumePendingFiles(ctx, run)
	rows, err := w.service.db.QueryContext(ctx, `SELECT call_uuid FROM calls WHERE retention_state IN ('active','grace','deletion_failed') AND retention_expires_at<=now() AND (retention_hold_until IS NULL OR retention_hold_until<=now()) ORDER BY retention_expires_at,call_uuid LIMIT $1`, w.batch)
	if err != nil {
		return
	}
	ids := make([]uuid.UUID, 0, w.batch)
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	meta := make([]string, len(ids))
	for i, id := range ids {
		meta[i] = id.String()
	}
	_ = audit(ctx, w.service.db, run, uuid.Nil, "call", uuid.Nil, "calls_selected_for_deletion", int64(len(ids)), -1, map[string]any{"call_uuids": meta})
	completed := int64(0)
	for _, id := range ids {
		if w.deleteCall(ctx, run, id) == nil {
			completed++
		}
	}
	_ = audit(ctx, w.service.db, run, uuid.Nil, "worker", uuid.Nil, "call_worker_run_completed", completed, -1, map[string]any{"selected": len(ids)})
}

func (w *Worker) resumePendingFiles(ctx context.Context, run uuid.UUID) {
	rows, err := w.service.db.QueryContext(ctx, `SELECT deletion_uuid,call_uuid,file_manifest FROM retention_call_deletions WHERE status IN ('db_deleted','files_pending','failed') AND available_at<=now() ORDER BY available_at,created_at LIMIT $1`, w.batch)
	if err != nil {
		return
	}
	type pending struct {
		deletion, call uuid.UUID
		files          []fileRef
	}
	items := make([]pending, 0)
	for rows.Next() {
		var item pending
		var raw []byte
		if rows.Scan(&item.deletion, &item.call, &raw) == nil && json.Unmarshal(raw, &item.files) == nil {
			items = append(items, item)
		}
	}
	_ = rows.Close()
	for _, item := range items {
		var bytes int64
		failed := false
		_ = audit(ctx, w.service.db, run, item.deletion, "call", item.call, "call_files_deletion_started", int64(len(item.files)), -1, map[string]any{"resumed": true})
		for _, file := range item.files {
			var deleteErr error
			if file.Kind == "report" {
				deleteErr = w.service.reports.Delete(ctx, file.Path)
			} else {
				deleteErr = w.service.audio.Delete(ctx, file.Path)
			}
			if deleteErr != nil {
				failed = true
				_, _ = w.service.db.ExecContext(ctx, `UPDATE retention_call_deletions SET status='files_pending',attempts=attempts+1,last_error_code='storage_delete_failed',last_error_message_safe=$2,available_at=now()+LEAST(attempts+1,24)*interval '1 hour',updated_at=now() WHERE deletion_uuid=$1`, item.deletion, deleteErr.Error())
				_ = audit(ctx, w.service.db, run, item.deletion, "call", item.call, "call_deletion_failed", -1, -1, map[string]any{"stage": "files", "resumed": true})
				break
			}
			bytes += file.Size
		}
		if !failed {
			_, _ = w.service.db.ExecContext(ctx, `UPDATE retention_call_deletions SET status='completed',completed_at=now(),updated_at=now(),last_error_code=NULL,last_error_message_safe=NULL WHERE deletion_uuid=$1`, item.deletion)
			_ = audit(ctx, w.service.db, run, item.deletion, "call", item.call, "call_files_deleted", int64(len(item.files)), bytes, map[string]any{"resumed": true})
			_ = audit(ctx, w.service.db, run, item.deletion, "call", item.call, "call_deletion_completed", 1, bytes, map[string]any{"resumed": true})
		}
	}
}

func (w *Worker) deleteCall(ctx context.Context, run, callID uuid.UUID) error {
	tx, err := w.service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var audioPath, cachePath string
	var expires time.Time
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT audio_path,asr_cache_path,retention_expires_at,retention_version FROM calls WHERE call_uuid=$1 AND retention_expires_at<=now() AND retention_state IN ('active','grace','deletion_failed') FOR UPDATE`, callID).Scan(&audioPath, &cachePath, &expires, &version); err != nil {
		return err
	}
	files := []fileRef{{Kind: "audio", Path: audioPath}}
	if cachePath != "" {
		files = append(files, fileRef{Kind: "audio", Path: cachePath})
	}
	variantRows, err := tx.QueryContext(ctx, `SELECT storage_path,COALESCE(size_bytes,0) FROM call_media_variants WHERE call_uuid=$1 AND storage_path IS NOT NULL`, callID)
	if err != nil {
		return err
	}
	for variantRows.Next() {
		var p string
		var size int64
		if variantRows.Scan(&p, &size) == nil && p != "" {
			files = append(files, fileRef{Kind: "privacy_media", Path: p, Size: size})
		}
	}
	_ = variantRows.Close()
	reportRows, err := tx.QueryContext(ctx, `SELECT storage_path,size_bytes FROM call_report_exports WHERE call_uuid=$1 AND storage_path IS NOT NULL`, callID)
	if err != nil {
		return err
	}
	for reportRows.Next() {
		var p string
		var size int64
		if reportRows.Scan(&p, &size) == nil {
			files = append(files, fileRef{Kind: "report", Path: p, Size: size})
		}
	}
	_ = reportRows.Close()
	var dbManifest []byte
	err = tx.QueryRowContext(ctx, `SELECT jsonb_build_object('transcriptions',(SELECT count(*) FROM call_transcriptions WHERE call_uuid=$1),'analyses',(SELECT count(*) FROM call_analyses WHERE call_uuid=$1),'actions',(SELECT count(*) FROM call_actions WHERE call_uuid=$1),'comments',(SELECT count(*) FROM call_analysis_comments WHERE call_uuid=$1),'reports',(SELECT count(*) FROM call_report_exports WHERE call_uuid=$1))`, callID).Scan(&dbManifest)
	if err != nil {
		return err
	}
	fileJSON, _ := json.Marshal(files)
	deletion := uuid.New()
	err = tx.QueryRowContext(ctx, `INSERT INTO retention_call_deletions(deletion_uuid,call_uuid,retention_expires_at,status,attempts,db_manifest,file_manifest) VALUES($1,$2,$3,'claimed',1,$4::jsonb,$5::jsonb) ON CONFLICT(call_uuid) DO UPDATE SET status='claimed',attempts=retention_call_deletions.attempts+1,updated_at=now() RETURNING deletion_uuid`, deletion, callID, expires, dbManifest, fileJSON).Scan(&deletion)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, run, deletion, "call", callID, "call_manifest_created", int64(len(files)), -1, map[string]any{"retention_version": version}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE calls SET retention_state='deleting' WHERE call_uuid=$1`, callID); err != nil {
		return err
	}
	if err = audit(ctx, tx, run, deletion, "call", callID, "call_database_deletion_started", -1, -1, nil); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM processing_jobs WHERE entity_uuid=$1`, callID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM calls WHERE call_uuid=$1`, callID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE retention_call_deletions SET status='db_deleted',updated_at=now() WHERE call_uuid=$1`, callID); err != nil {
		return err
	}
	if err = audit(ctx, tx, run, deletion, "call", callID, "call_database_rows_deleted", 1, -1, json.RawMessage(dbManifest)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_ = audit(ctx, w.service.db, run, deletion, "call", callID, "call_files_deletion_started", int64(len(files)), -1, nil)
	var bytes int64
	for _, file := range files {
		var deleteErr error
		if file.Kind == "report" {
			deleteErr = w.service.reports.Delete(ctx, file.Path)
		} else {
			deleteErr = w.service.audio.Delete(ctx, file.Path)
		}
		if deleteErr != nil {
			_, _ = w.service.db.ExecContext(ctx, `UPDATE retention_call_deletions SET status='files_pending',last_error_code='storage_delete_failed',last_error_message_safe=$2,available_at=now()+interval '1 hour',updated_at=now() WHERE call_uuid=$1`, callID, deleteErr.Error())
			_ = audit(ctx, w.service.db, run, deletion, "call", callID, "call_deletion_failed", -1, -1, map[string]any{"stage": "files"})
			return deleteErr
		}
		bytes += file.Size
	}
	_, err = w.service.db.ExecContext(ctx, `UPDATE retention_call_deletions SET status='completed',completed_at=now(),updated_at=now(),last_error_code=NULL,last_error_message_safe=NULL WHERE call_uuid=$1`, callID)
	if err == nil {
		_ = audit(ctx, w.service.db, run, deletion, "call", callID, "call_files_deleted", int64(len(files)), bytes, nil)
		_ = audit(ctx, w.service.db, run, deletion, "call", callID, "call_deletion_completed", 1, bytes, nil)
	}
	return err
}

func (w *Worker) runInstructions(ctx context.Context) {
	ok, err := w.lock(ctx, instructionWorkerLock)
	if err != nil || !ok {
		return
	}
	defer w.unlock(context.Background(), instructionWorkerLock)
	run := uuid.New()
	rows, err := w.service.db.QueryContext(ctx, `SELECT i.instruction_uuid,i.file_path FROM analysis_instructions i WHERE i.deleted_at IS NOT NULL AND i.purge_state IN ('eligible','failed') AND i.purge_after<=now() AND NOT EXISTS(SELECT 1 FROM call_analysis_instruction_snapshots s WHERE s.instruction_uuid=i.instruction_uuid) ORDER BY i.purge_after,i.instruction_uuid LIMIT $1`, w.batch)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	type candidate struct {
		id   uuid.UUID
		path string
	}
	items := []candidate{}
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.id, &c.path) == nil {
			items = append(items, c)
		}
	}
	ids := make([]string, len(items))
	for i, c := range items {
		ids[i] = c.id.String()
	}
	_ = audit(ctx, w.service.db, run, uuid.Nil, "instruction", uuid.Nil, "instructions_selected_for_purge", int64(len(items)), -1, map[string]any{"instruction_uuids": ids})
	for _, item := range items {
		if err = w.service.instructions.Delete(ctx, item.path); err != nil {
			_, _ = w.service.db.ExecContext(ctx, `UPDATE analysis_instructions SET purge_state='failed',purge_after=now()+interval '1 day' WHERE instruction_uuid=$1`, item.id)
			continue
		}
		res, deleteErr := w.service.db.ExecContext(ctx, `DELETE FROM analysis_instructions i WHERE instruction_uuid=$1 AND deleted_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM call_analysis_instruction_snapshots s WHERE s.instruction_uuid=i.instruction_uuid)`, item.id)
		if deleteErr == nil {
			count, _ := res.RowsAffected()
			_ = audit(ctx, w.service.db, run, uuid.Nil, "instruction", item.id, "instruction_purge_completed", count, -1, nil)
		}
	}
	w.service.log.Info(ctx, "instruction retention run completed", zap.Int("selected", len(items)))
}

func (s *Service) String() string { return fmt.Sprintf("retention service %p", s) }
