package integration

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"verbatrace/monolit/internal/integrationcrypto"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/mediafetch"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type WorkerRepository interface {
	ClaimIngest(context.Context, string, time.Duration) (models.ClaimedIngestItem, error)
	CompleteIngest(context.Context, uuid.UUID, uuid.UUID, int64, int, []byte) error
	FailIngest(context.Context, uuid.UUID, string, string, bool, time.Duration) error
	HeartbeatIngest(context.Context, uuid.UUID, string, time.Duration) error
	ClearExpiredIngestLocators(context.Context) (int64, error)
}
type CallCreator interface {
	CreateCall(context.Context, models.CreateCallInput) (models.Call, error)
}
type Fetcher interface {
	Fetch(context.Context, string) (io.ReadCloser, int64, string, error)
}
type Worker struct {
	repo        WorkerRepository
	calls       CallCreator
	fetcher     Fetcher
	cipher      *integrationcrypto.Cipher
	log         logger.Logger
	id          string
	poll, lease time.Duration
	stagingDir  string
}

func NewWorker(repo WorkerRepository, calls CallCreator, cipher *integrationcrypto.Cipher, log logger.Logger, stagingDir ...string) *Worker {
	if log == nil {
		log = logger.NewNop()
	}
	dir := ""
	if len(stagingDir) > 0 {
		dir = stagingDir[0]
	}
	return &Worker{repo: repo, calls: calls, fetcher: mediafetch.New(500 << 20), cipher: cipher, log: log, id: "ingest-" + uuid.NewString(), poll: 2 * time.Second, lease: 15 * time.Minute, stagingDir: dir}
}
func (w *Worker) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.poll)
		defer ticker.Stop()
		lastCleanup := time.Time{}
		for {
			if time.Since(lastCleanup) >= time.Hour {
				w.cleanupStaging(time.Now().UTC())
				if _, err := w.repo.ClearExpiredIngestLocators(ctx); err != nil && ctx.Err() == nil {
					w.log.Error(ctx, "expired ingest locator cleanup failed", zap.Error(err))
				}
				lastCleanup = time.Now()
			}
			if err := w.runOne(ctx); err != nil && !errors.Is(err, models.ErrIngestNotFound) && ctx.Err() == nil {
				w.log.Error(ctx, "ingest worker iteration failed", zap.Error(err))
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

func (w *Worker) cleanupStaging(now time.Time) {
	if w.stagingDir == "" {
		return
	}
	entries, err := os.ReadDir(w.stagingDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			w.log.Error(context.Background(), "integration staging cleanup failed", zap.Error(err))
		}
		return
	}
	for _, entry := range entries {
		extension := filepath.Ext(entry.Name())
		if entry.IsDir() || (extension != ".part" && extension != ".media") {
			continue
		}
		info, infoErr := entry.Info()
		maxAge := 25 * time.Hour
		if extension == ".media" {
			maxAge = 7 * 24 * time.Hour
		}
		if infoErr == nil && now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(w.stagingDir, entry.Name()))
		}
	}
}
func (w *Worker) runOne(ctx context.Context) error {
	item, err := w.repo.ClaimIngest(ctx, w.id, w.lease)
	if err != nil {
		return err
	}
	if item.LocatorKeyVersion != w.cipher.Version() {
		_ = w.repo.FailIngest(ctx, item.ID, "encryption_key_unavailable", "Recording key is unavailable", true, time.Minute)
		return integrationcrypto.ErrUnavailable
	}
	raw, err := w.cipher.Decrypt(item.LocatorCiphertext, "ingest_items/application/"+item.ApplicationID.String()+"/recording_locator")
	if err != nil {
		_ = w.repo.FailIngest(ctx, item.ID, "locator_decrypt_failed", "Recording locator cannot be decrypted", false, 0)
		return err
	}
	body, declared, mimeType, stagedPath, err := w.openRecording(ctx, item, string(raw))
	if err != nil {
		_ = w.repo.FailIngest(ctx, item.ID, "recording_fetch_failed", "Recording is temporarily unavailable", true, retryDelay(item.Attempts))
		return err
	}
	defer func() { _ = body.Close() }()
	tmp, err := os.CreateTemp("", "verbatrace-ingest-*")
	if err != nil {
		_ = w.repo.FailIngest(ctx, item.ID, "temporary_storage_failed", "Temporary storage is unavailable", true, retryDelay(item.Attempts))
		return err
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }()
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, (500<<20)+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil || size <= 0 || size > 500<<20 {
		_ = w.repo.FailIngest(ctx, item.ID, "invalid_recording_size", "Recording size is invalid", false, 0)
		removeStaged(stagedPath)
		if copyErr != nil {
			return copyErr
		}
		return errors.New("invalid recording size")
	}
	if declared > 0 && declared != size {
		_ = w.repo.FailIngest(ctx, item.ID, "recording_size_mismatch", "Recording size changed during download", true, retryDelay(item.Attempts))
		return errors.New("recording size mismatch")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if strings.TrimSpace(mimeType) == "" || strings.HasPrefix(strings.ToLower(mimeType), "application/octet-stream") {
		head := make([]byte, 512)
		n, _ := file.Read(head)
		mimeType = http.DetectContentType(head[:n])
		_, _ = file.Seek(0, io.SeekStart)
	}
	filename := "recording"
	if item.OriginalFilename != nil {
		filename = filepath.Base(*item.OriginalFilename)
	}
	if filepath.Ext(filename) == "" {
		if exts, _ := mime.ExtensionsByType(strings.Split(mimeType, ";")[0]); len(exts) > 0 {
			filename += exts[0]
		}
	}
	visibility := models.CallVisibilityScopePersonal
	if item.CompanyID.Valid {
		visibility = models.CallVisibilityScopeCompany
	}
	if item.DepartmentID.Valid {
		visibility = models.CallVisibilityScopeDepartment
	}
	created, err := w.calls.CreateCall(ctx, models.CreateCallInput{Title: item.Title, OriginalFilename: filename, MimeType: mimeType, SizeBytes: size, Content: file, UploadedByUserUUID: item.CreatedByUserID, CompanyUUID: item.CompanyID, DepartmentUUID: item.DepartmentID, VisibilityScope: visibility, FolderUUID: item.FolderID, IntegrationPrincipalUUID: uuid.NullUUID{UUID: item.ConnectionID, Valid: true}, SkipCustomInstructions: !item.InheritScopeInstructions})
	if err != nil {
		retry := !errors.Is(err, models.ErrUnsupportedAudioType) && !errors.Is(err, models.ErrInvalidCallPlacement)
		_ = w.repo.FailIngest(ctx, item.ID, "call_creation_failed", "Recording could not be processed", retry, retryDelay(item.Attempts))
		if !retry {
			removeStaged(stagedPath)
		}
		return err
	}
	if err = w.repo.CompleteIngest(ctx, item.ID, created.ID, size, created.DurationSeconds, hash.Sum(nil)); err != nil {
		return err
	}
	removeStaged(stagedPath)
	return nil
}

func removeStaged(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

func (w *Worker) openRecording(ctx context.Context, item models.ClaimedIngestItem, locator string) (io.ReadCloser, int64, string, string, error) {
	if item.SourceKind != "upload" {
		body, size, contentType, err := w.fetcher.Fetch(ctx, locator)
		return body, size, contentType, "", err
	}
	if w.stagingDir == "" || filepath.Base(locator) != locator {
		return nil, 0, "", "", errors.New("invalid staged recording locator")
	}
	base, err := filepath.Abs(w.stagingDir)
	if err != nil {
		return nil, 0, "", "", err
	}
	path := filepath.Join(base, locator)
	if filepath.Dir(path) != base {
		return nil, 0, "", "", errors.New("staged recording escaped storage root")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, "", "", err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, "", "", err
	}
	return file, info.Size(), "", path, nil
}
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Minute * time.Duration(1<<(attempt-1))
}
