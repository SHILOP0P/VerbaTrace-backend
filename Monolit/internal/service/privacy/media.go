package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrOriginalMediaForbidden = errors.New("original media forbidden")
	ErrRedactedMediaNotReady  = errors.New("redacted media not ready")
	ErrMediaVariantNotFound   = errors.New("media variant not found")
)

type MediaFile struct {
	File           *os.File
	Name, MIMEType string
	Size           int64
}

func (s *Service) RequestSanitizedMedia(ctx context.Context, call models.Call, userID uuid.UUID) (models.MediaVariant, error) {
	capabilities, err := s.Capabilities(ctx, call, userID)
	if err != nil {
		return models.MediaVariant{}, err
	}
	if !capabilities.CanRequestSanitizedMedia {
		return models.MediaVariant{}, ErrOriginalMediaForbidden
	}
	state, err := s.GetCallState(ctx, call.ID)
	if err != nil {
		return models.MediaVariant{}, err
	}
	if state.Status != "ready" {
		return models.MediaVariant{}, ErrRedactedMediaNotReady
	}
	spans, err := s.RedactionSpans(ctx, call.ID)
	if err != nil {
		return models.MediaVariant{}, err
	}
	intervals := canonicalIntervals(spans, float64(call.DurationSeconds))
	intervalBody, _ := json.Marshal(intervals)
	intervalHash := sha256.Sum256(intervalBody)
	sourceHash, err := s.sourceFingerprint(ctx, call.AudioPath, call.SizeBytes)
	if err != nil {
		return models.MediaVariant{}, err
	}
	variant := "redacted_audio"
	if strings.HasPrefix(strings.ToLower(call.MimeType), "video/") {
		variant = "redacted_video"
	}
	id, err := uuid.NewV7()
	if err != nil {
		return models.MediaVariant{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO call_media_variants(media_variant_uuid,call_uuid,variant,transcription_revision,privacy_policy_version_uuid,status,source_fingerprint,intervals_sha256)
		VALUES($1,$2,$3,$4,$5,'pending',$6,$7) ON CONFLICT(call_uuid,variant,transcription_revision,intervals_sha256,processor_contract) DO NOTHING`,
		id, call.ID, variant, state.Revision, state.PolicyVersionID, sourceHash, intervalHash[:])
	if err != nil {
		return models.MediaVariant{}, err
	}
	media, err := s.GetSanitizedMedia(ctx, call.ID)
	if err != nil {
		return models.MediaVariant{}, err
	}
	_ = insertAudit(ctx, s.db, scopeForCall(call), scopeIDForCall(call), uuid.NullUUID{UUID: call.ID, Valid: true}, userID, "sanitized_media_requested", "call_media_variant", media.ID, map[string]any{"variant": variant})
	return media, nil
}

func (s *Service) GetSanitizedMedia(ctx context.Context, callID uuid.UUID) (models.MediaVariant, error) {
	var item models.MediaVariant
	var videoCopied sql.NullBool
	var errCode sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT v.media_variant_uuid,v.call_uuid,v.variant,v.transcription_revision,v.status,
		COALESCE(v.storage_path,''),COALESCE(v.file_name,''),COALESCE(v.mime_type,''),COALESCE(v.size_bytes,0),v.processor_contract,
		COALESCE(v.container,''),COALESCE(v.audio_codec,''),COALESCE(v.video_codec,''),v.video_stream_copied,COALESCE(v.output_duration_ms,0),v.last_error_code,v.created_at,v.updated_at
		FROM call_media_variants v JOIN call_privacy_states p ON p.call_uuid=v.call_uuid AND p.transcription_revision=v.transcription_revision
		WHERE v.call_uuid=$1 ORDER BY v.created_at DESC LIMIT 1`, callID).Scan(&item.ID, &item.CallID, &item.Variant, &item.TranscriptionRevision, &item.Status, &item.StoragePath, &item.FileName, &item.MIMEType, &item.SizeBytes, &item.ProcessorContract, &item.Container, &item.AudioCodec, &item.VideoCodec, &videoCopied, &item.OutputDurationMS, &errCode, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrMediaVariantNotFound
	}
	if err != nil {
		return item, err
	}
	if videoCopied.Valid {
		item.VideoStreamCopied = &videoCopied.Bool
	}
	if errCode.Valid {
		item.LastErrorCode = &errCode.String
	}
	return item, nil
}

// SupportMediaVariant answers what a support engineer with temporary access may
// hear. The customer's masking policy applies to them as it does to everybody
// else: where the policy is on and a redacted copy is ready, that copy is what
// they get. The admin path used to open the original file directly, which walked
// straight past the policy.
//
// It returns the storage path to serve and whether it is the redacted variant.
func (s *Service) SupportMediaVariant(ctx context.Context, call models.Call) (string, bool, error) {
	state, err := s.EnsureCallState(ctx, call)
	if err != nil {
		return "", false, err
	}
	if !state.PolicySnapshot.Enabled {
		return call.AudioPath, false, nil
	}

	variant, err := s.GetSanitizedMedia(ctx, call.ID)
	if err != nil {
		if errors.Is(err, ErrMediaVariantNotFound) {
			// The policy asks for masking and there is nothing masked to serve, so
			// there is nothing support may listen to.
			return "", false, ErrRedactedMediaNotReady
		}
		return "", false, err
	}
	if variant.Status != "ready" {
		return "", false, ErrRedactedMediaNotReady
	}

	return variant.StoragePath, true, nil
}

func (s *Service) OpenSanitizedMedia(ctx context.Context, call models.Call, userID uuid.UUID) (MediaFile, error) {
	capabilities, err := s.Capabilities(ctx, call, userID)
	if err != nil {
		return MediaFile{}, err
	}
	if !capabilities.CanRequestSanitizedMedia {
		return MediaFile{}, ErrOriginalMediaForbidden
	}
	variant, err := s.GetSanitizedMedia(ctx, call.ID)
	if err != nil {
		return MediaFile{}, err
	}
	if variant.Status != "ready" {
		return MediaFile{}, ErrRedactedMediaNotReady
	}
	path, err := s.safeAudioPath(variant.StoragePath)
	if err != nil {
		return MediaFile{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return MediaFile{}, err
	}
	return MediaFile{File: file, Name: variant.FileName, MIMEType: variant.MIMEType, Size: variant.SizeBytes}, nil
}

func (s *Service) RunMediaWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processNextMedia(ctx)
			}
		}
	}()
	return done
}

func (s *Service) processNextMedia(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE call_media_variants SET status='pending',locked_at=NULL,locked_by=NULL,last_error_code='privacy_media_worker_lease_expired',last_error_message_safe='Повторная подготовка после остановки worker',available_at=now(),updated_at=now() WHERE status='processing' AND locked_at<now()-interval '30 minutes'`)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	var id uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT media_variant_uuid FROM call_media_variants WHERE status IN ('pending','failed') AND available_at<=now() AND attempts<max_attempts ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id)
	if err != nil {
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_media_variants SET status='processing',attempts=attempts+1,locked_at=now(),locked_by='privacy-media-worker',updated_at=now() WHERE media_variant_uuid=$1`, id); err != nil {
		return
	}
	if err = tx.Commit(); err != nil {
		return
	}
	if err = s.generateMedia(ctx, id); err != nil {
		_, _ = s.db.ExecContext(context.Background(), `UPDATE call_media_variants SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'pending' END,last_error_code='privacy_media_generation_failed',last_error_message_safe='Не удалось подготовить очищенную запись',available_at=now()+interval '30 seconds',locked_at=NULL,locked_by=NULL,updated_at=now() WHERE media_variant_uuid=$1`, id)
		s.log.Error(ctx, "sanitized media generation failed", zap.String("media_variant_id", id.String()), zap.Error(err))
	}
}

type mediaJob struct {
	ID, CallID                                      uuid.UUID
	Variant, SourcePath, OriginalFilename, MIMEType string
	Duration                                        int
	Intervals                                       []interval
}
type interval struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func (s *Service) generateMedia(ctx context.Context, id uuid.UUID) error {
	var job mediaJob
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT v.media_variant_uuid,v.call_uuid,v.variant,c.audio_path,c.original_filename,c.mime_type,c.duration_seconds,
		COALESCE(jsonb_agg(jsonb_build_object('start',r.start_seconds,'end',r.end_seconds) ORDER BY r.start_seconds) FILTER(WHERE r.redaction_span_uuid IS NOT NULL),'[]'::jsonb)
		FROM call_media_variants v JOIN calls c ON c.call_uuid=v.call_uuid LEFT JOIN call_transcriptions t ON t.call_uuid=c.call_uuid
		LEFT JOIN call_transcription_redaction_spans r ON r.transcription_uuid=t.transcription_uuid AND r.revision=v.transcription_revision
		WHERE v.media_variant_uuid=$1 GROUP BY v.media_variant_uuid,c.call_uuid`, id).Scan(&job.ID, &job.CallID, &job.Variant, &job.SourcePath, &job.OriginalFilename, &job.MIMEType, &job.Duration, &raw)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &job.Intervals); err != nil {
		return err
	}
	source, err := s.safeAudioPath(job.SourcePath)
	if err != nil {
		return err
	}
	probe, err := s.probe(ctx, source)
	if err != nil {
		return err
	}
	ext, mimeType, container, audioCodec, videoCodec, copyVideo := outputProfile(job.Variant, probe.VideoCodec)
	dir := filepath.Join(s.config.AudioBaseDir, "privacy", job.CallID.String())
	if err = os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	name := job.ID.String() + ext
	partial := filepath.Join(dir, job.ID.String()+".partial"+ext)
	target := filepath.Join(dir, name)
	_ = os.Remove(partial)
	filter := audioFilter(job.Intervals, math.Max(probe.Duration, float64(job.Duration)))
	args := []string{"-hide_banner", "-nostdin", "-y", "-i", source, "-filter_complex", filter}
	if job.Variant == "redacted_video" {
		args = append(args, "-map", "0:v:0", "-map", "[aout]")
		if copyVideo {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "21")
		}
		if container == "webm" {
			args = append(args, "-c:a", "libopus", "-b:a", "128k")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart")
		}
	} else {
		args = append(args, "-map", "[aout]", "-c:a", "libmp3lame", "-b:a", "128k")
	}
	args = append(args, partial)
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandCtx, s.config.FFmpegPath, args...)
	output := &boundedBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = output, output
	err = command.Run()
	if err != nil {
		return fmt.Errorf("ffmpeg sanitized media: %w: %s", err, strings.TrimSpace(output.String()))
	}
	validated, err := s.probe(ctx, partial)
	if err != nil {
		return err
	}
	if math.Abs(validated.Duration-probe.Duration) > .1 {
		return fmt.Errorf("sanitized media duration drift")
	}
	info, err := os.Stat(partial)
	if err != nil || info.Size() <= 0 {
		return fmt.Errorf("sanitized media is empty")
	}
	if err = os.Rename(partial, target); err != nil {
		return err
	}
	rel, err := filepath.Rel(s.config.AudioBaseDir, target)
	if err != nil {
		return err
	}
	displayName := strings.TrimSuffix(filepath.Base(job.OriginalFilename), filepath.Ext(job.OriginalFilename)) + "-очищено" + ext
	_, err = s.db.ExecContext(ctx, `UPDATE call_media_variants SET status='ready',storage_path=$2,file_name=$3,mime_type=$4,size_bytes=$5,container=$6,audio_codec=$7,video_codec=NULLIF($8,''),video_stream_copied=$9,output_duration_ms=$10,last_error_code=NULL,last_error_message_safe=NULL,locked_at=NULL,locked_by=NULL,completed_at=now(),updated_at=now() WHERE media_variant_uuid=$1`, id, filepath.ToSlash(rel), displayName, mimeType, info.Size(), container, audioCodec, videoCodec, nullableBool(job.Variant == "redacted_video", copyVideo), int64(math.Round(validated.Duration*1000)))
	return err
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buffer.Write(value)
	}
	return original, nil
}
func (b *boundedBuffer) String() string { return b.buffer.String() }

type probeResult struct {
	Duration   float64
	VideoCodec string
}

func (s *Service) probe(ctx context.Context, path string) (probeResult, error) {
	cmd := exec.CommandContext(ctx, s.config.FFProbePath, "-v", "error", "-show_entries", "format=duration:stream=codec_type,codec_name", "-of", "json", path)
	body, err := cmd.Output()
	if err != nil {
		return probeResult{}, err
	}
	var payload struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type  string `json:"codec_type"`
			Codec string `json:"codec_name"`
		} `json:"streams"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return probeResult{}, err
	}
	duration, err := strconv.ParseFloat(payload.Format.Duration, 64)
	if err != nil {
		return probeResult{}, err
	}
	result := probeResult{Duration: duration}
	for _, stream := range payload.Streams {
		if stream.Type == "video" {
			result.VideoCodec = stream.Codec
			break
		}
	}
	return result, nil
}

func outputProfile(variant, video string) (ext, mime, container, audio, videoOut string, copyVideo bool) {
	if variant == "redacted_audio" {
		return ".mp3", "audio/mpeg", "mp3", "mp3", "", false
	}
	switch video {
	case "h264":
		return ".mp4", "video/mp4", "mp4", "aac", "h264", true
	case "vp8", "vp9", "av1":
		return ".webm", "video/webm", "webm", "opus", video, true
	default:
		return ".mp4", "video/mp4", "mp4", "aac", "h264", false
	}
}
func nullableBool(enabled, value bool) any {
	if !enabled {
		return nil
	}
	return value
}
func audioFilter(intervals []interval, duration float64) string {
	condition := "0"
	parts := make([]string, 0, len(intervals))
	for _, item := range intervals {
		parts = append(parts, fmt.Sprintf("between(t\\,%0.3f\\,%0.3f)", item.Start, item.End))
	}
	if len(parts) > 0 {
		condition = strings.Join(parts, "+")
	}
	return fmt.Sprintf("[0:a]volume='if(%s,0,1)':eval=frame[base];sine=frequency=880:sample_rate=48000:duration=%0.3f,volume='if(%s,0.12,0)':eval=frame[tone];[base][tone]amix=inputs=2:normalize=0[aout]", condition, duration, condition)
}
func canonicalIntervals(spans []models.RedactionSpan, duration float64) []interval {
	items := make([]interval, 0, len(spans))
	for _, span := range spans {
		start := math.Max(0, span.StartSeconds-.08)
		end := math.Min(duration, span.EndSeconds+.08)
		if end >= start {
			items = append(items, interval{start, end})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Start < items[j].Start })
	merged := make([]interval, 0, len(items))
	for _, item := range items {
		if len(merged) > 0 && item.Start <= merged[len(merged)-1].End {
			if item.End > merged[len(merged)-1].End {
				merged[len(merged)-1].End = item.End
			}
		} else {
			merged = append(merged, item)
		}
	}
	return merged
}
func (s *Service) sourceFingerprint(ctx context.Context, path string, size int64) ([]byte, error) {
	content, err := s.audio.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = content.Close() }()
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%d:", size)
	if _, err = io.Copy(hash, content); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}
func (s *Service) safeAudioPath(relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute media path")
	}
	base, err := filepath.Abs(s.config.AudioBaseDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("media path escapes storage")
	}
	return target, nil
}
func scopeForCall(call models.Call) models.PrivacyScopeType {
	if call.CompanyUUID.Valid {
		return models.PrivacyScopeCompany
	}
	return models.PrivacyScopePersonal
}
func scopeIDForCall(call models.Call) uuid.UUID {
	if call.CompanyUUID.Valid {
		return call.CompanyUUID.UUID
	}
	return call.UploadedByUserUUID.UUID
}
