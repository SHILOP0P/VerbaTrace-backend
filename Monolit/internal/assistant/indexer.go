package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"verbatrace/monolit/internal/analysistext"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type indexCandidate struct {
	callID, transcriptionID uuid.UUID
	revision                int
	hash                    []byte
	payload                 []byte
	analysisPayload         []byte
	// Who pays for embedding this call, and under which cap.
	ownerID      uuid.UUID
	companyID    uuid.UUID
	departmentID uuid.UUID
}
type revisionPayload struct {
	Text     string                        `json:"text"`
	Segments []models.TranscriptionSegment `json:"segments"`
}
type searchChunk struct {
	Text, Speaker string
	Start, End    *float64
	SourceKind    string
}

func (s *Service) RunIndexWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if err := s.IndexNext(ctx, 10); err != nil {
				log.Printf("assistant index worker: %v", err)
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

func (s *Service) IndexNext(ctx context.Context, limit int) error {
	provider, model, dimensions := "local", "lexical-v1", 1536
	if s.embedder != nil && s.embedder.Enabled() {
		provider, model, dimensions = s.embedder.Profile()
	}
	profile, err := s.ensureProfile(ctx, provider, model, dimensions)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.call_uuid,t.transcription_uuid,r.revision,ct.content_sha256,ct.payload,COALESCE(a.result_json,'{}'::jsonb),c.uploaded_by_user_uuid,c.company_uuid,c.department_uuid FROM calls c JOIN call_transcriptions t ON t.call_uuid=c.call_uuid JOIN call_transcription_revision_state rs ON rs.transcription_uuid=t.transcription_uuid JOIN call_transcription_revisions r ON r.transcription_uuid=t.transcription_uuid AND r.revision=rs.active_revision JOIN call_transcription_contents ct ON ct.transcription_content_uuid=r.transcription_content_uuid LEFT JOIN call_privacy_states ps ON ps.call_uuid=c.call_uuid LEFT JOIN LATERAL (SELECT result_json,updated_at FROM call_analyses WHERE call_uuid=c.call_uuid AND status='done' ORDER BY updated_at DESC LIMIT 1) a ON true WHERE t.status='transcribed' AND c.deleted_at IS NULL AND (ps.call_uuid IS NULL OR ps.status IN ('not_requested','ready')) AND NOT EXISTS(SELECT 1 FROM call_search_documents d WHERE d.call_uuid=c.call_uuid AND d.transcription_revision=r.revision AND d.profile_uuid=$1 AND d.status='ready' AND d.content_sha256=ct.content_sha256 AND (ps.updated_at IS NULL OR d.updated_at>=ps.updated_at) AND (a.updated_at IS NULL OR d.updated_at>=a.updated_at)) ORDER BY GREATEST(t.updated_at,COALESCE(a.updated_at,t.updated_at)) LIMIT $2`, profile, limit)
	if err != nil {
		return err
	}
	candidates := []indexCandidate{}
	for rows.Next() {
		var x indexCandidate
		var owner, company, department uuid.NullUUID
		if err = rows.Scan(&x.callID, &x.transcriptionID, &x.revision, &x.hash, &x.payload, &x.analysisPayload, &owner, &company, &department); err != nil {
			_ = rows.Close()
			return err
		}
		x.ownerID, x.companyID, x.departmentID = owner.UUID, company.UUID, department.UUID
		candidates = append(candidates, x)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err = s.indexCandidate(ctx, profile, candidate); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ensureProfile(ctx context.Context, provider, model string, dimensions int) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT search_index_profile_uuid FROM search_index_profiles WHERE provider=$1 AND model=$2 AND dimensions=$3 AND chunker_version='segments-v1' AND retrieval_version='rrf-v1'`, provider, model, dimensions).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, err
	}
	id, _ = uuid.NewV7()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE search_index_profiles SET status='retired' WHERE status='active'`); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO search_index_profiles(search_index_profile_uuid,provider,model,dimensions,chunker_version,retrieval_version,status) VALUES($1,$2,$3,$4,'segments-v1','rrf-v1','active') ON CONFLICT(provider,model,dimensions,chunker_version,retrieval_version) DO UPDATE SET status='active'`, id, provider, model, dimensions); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(); err != nil {
		return uuid.Nil, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT search_index_profile_uuid FROM search_index_profiles WHERE provider=$1 AND model=$2 AND dimensions=$3 AND chunker_version='segments-v1' AND retrieval_version='rrf-v1'`, provider, model, dimensions).Scan(&id)
	return id, err
}
func (s *Service) indexCandidate(ctx context.Context, profile uuid.UUID, c indexCandidate) error {
	var payload revisionPayload
	if err := json.Unmarshal(c.payload, &payload); err != nil {
		return err
	}
	chunks := chunkPayload(payload)
	if analysisText := analysisSearchText(c.analysisPayload); analysisText != "" {
		for _, text := range splitText("Сохранённый анализ звонка. "+analysisText, maxChunkRunes) {
			chunks = append(chunks, searchChunk{Text: text, SourceKind: "analysis"})
		}
	}
	if len(chunks) == 0 {
		return nil
	}
	var vectors [][]float32
	if s.embedder != nil && s.embedder.Enabled() {
		inputs := make([]string, len(chunks))
		tokens := int64(0)
		for i := range chunks {
			inputs[i] = chunks[i].Text
			tokens += int64((len([]byte(chunks[i].Text)) + 2) / 3)
		}
		// Indexing calls a paid provider, so it books credits like any other AI
		// operation. The reference keys the operation to this exact revision of
		// this call, which makes a repeated attempt idempotent rather than a
		// second charge.
		provider, model, _ := s.embedder.Profile()
		reference := fmt.Sprintf("index:%s:%d", c.callID, c.revision)
		var creditOperation uuid.UUID
		if s.credits != nil {
			var reserveErr error
			creditOperation, reserveErr = s.credits.ReserveEmbedding(ctx, c.ownerID, c.companyID, c.departmentID, reference, tokens, provider, model)
			if reserveErr != nil {
				return reserveErr
			}
		}
		var err error
		vectors, _, err = s.embedder.Embed(ctx, inputs, "search_document")
		if err != nil {
			if s.credits != nil && creditOperation != uuid.Nil {
				_ = s.credits.MarkCreditOperationReconciling(ctx, creditOperation, "embedding provider result unavailable")
			}
			return err
		}
		if len(vectors) != len(chunks) {
			if s.credits != nil && creditOperation != uuid.Nil {
				_ = s.credits.MarkCreditOperationReconciling(ctx, creditOperation, "embedding provider returned unexpected item count")
			}
			return errors.New("embedding provider returned unexpected item count")
		}
		if s.credits != nil && creditOperation != uuid.Nil {
			if err = s.credits.SettleEmbedding(ctx, creditOperation, tokens, provider, model); err != nil {
				return err
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, _ = tx.ExecContext(ctx, `UPDATE call_search_documents SET status='stale',updated_at=now() WHERE call_uuid=$1 AND profile_uuid=$2 AND status='ready'`, c.callID, profile)
	doc, _ := uuid.NewV7()
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_search_documents(call_search_document_uuid,call_uuid,transcription_uuid,transcription_revision,content_sha256,profile_uuid,status) VALUES($1,$2,$3,$4,$5,$6,'indexing') ON CONFLICT(call_uuid,transcription_revision,profile_uuid) DO UPDATE SET status='indexing',error_code=NULL,updated_at=now()`, doc, c.callID, c.transcriptionID, c.revision, c.hash, profile); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT call_search_document_uuid FROM call_search_documents WHERE call_uuid=$1 AND transcription_revision=$2 AND profile_uuid=$3`, c.callID, c.revision, profile).Scan(&doc); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_search_chunks WHERE call_search_document_uuid=$1`, doc); err != nil {
		return err
	}
	for i, ch := range chunks {
		id, _ := uuid.NewV7()
		var embedding any
		if len(vectors) > i {
			embedding = vectorLiteral(vectors[i])
		}
		kind := ch.SourceKind
		if kind == "" {
			kind = "transcription"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO call_search_chunks(call_search_chunk_uuid,call_search_document_uuid,ordinal,text,speaker,start_seconds,end_seconds,embedding,source_kind) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8::vector,$9)`, id, doc, i, ch.Text, ch.Speaker, ch.Start, ch.End, embedding, kind); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE call_search_documents d SET status='ready',updated_at=now() FROM call_transcription_revision_state rs WHERE d.call_search_document_uuid=$1 AND rs.transcription_uuid=d.transcription_uuid AND rs.active_revision=d.transcription_revision`, doc)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("transcription changed during indexing")
	}
	return tx.Commit()
}

func analysisSearchText(raw []byte) string {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	parts := make([]string, 0)
	appendString := func(label string, value string) {
		if text := strings.TrimSpace(value); text != "" {
			parts = append(parts, label+": "+text)
		}
	}
	items, universal := payload["items"].([]any)
	readable := func(value any) string { text, _ := value.(string); return text }
	var titles map[string]string
	if universal {
		// A chunk is quoted to people and to the assistant as it is stored, so it
		// carries none of the model's reference IDs. Speakers are «Спикер A», not
		// names: a name may change later without the call being indexed again.
		titles = analysistext.ResultTitles(raw)
		readable = func(value any) string {
			text, _ := value.(string)
			return analysistext.ResolveSpeakerMarkers(analysistext.StripReferenceIDs(text, titles), nil)
		}
	}
	appendString("Общий вывод", readable(payload["summary"]))
	appendString("Результат", readable(payload["outcome"]))
	if universal {
		for _, rawItem := range items {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["id"].(string)
			title := titles[id]
			if title == "" {
				title = readable(item["title"])
			}
			text := strings.TrimSpace(strings.Join([]string{title, readable(item["answer_summary"]), readable(item["explanation"]), readable(item["improvement"])}, ". "))
			if text != "" {
				parts = append(parts, "Пункт анализа: "+text)
			}
		}
	} else if criteria, ok := payload["criteria_results"].([]any); ok {
		for _, rawItem := range criteria {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			title, _ := item["title"].(string)
			explanation, _ := item["explanation"].(string)
			recommendation, _ := item["recommendation"].(string)
			text := strings.TrimSpace(strings.Join([]string{title, explanation, recommendation}, ". "))
			if text != "" {
				parts = append(parts, "Пункт анализа: "+text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// readableChunk cleans an analysis chunk indexed before chunks were written
// without reference IDs; no re-index is needed for it. A transcript chunk is
// what was said and stays as it is.
func readableChunk(kind, text string) string {
	if kind != "analysis" {
		return text
	}
	return analysistext.ResolveSpeakerMarkers(analysistext.StripReferenceIDs(text, nil), nil)
}

const maxChunkRunes = 1800

func chunkPayload(p revisionPayload) []searchChunk {
	out := []searchChunk{}
	if len(p.Segments) == 0 {
		for _, text := range splitText(p.Text, maxChunkRunes) {
			out = append(out, searchChunk{Text: text})
		}
		return out
	}
	for _, seg := range p.Segments {
		for _, text := range splitText(seg.Text, maxChunkRunes) {
			// Segment timestamps bound the quotation; no invented word-level timing.
			out = append(out, searchChunk{Text: text, Speaker: seg.Speaker, Start: seg.StartSeconds, End: seg.EndSeconds})
		}
	}
	return out
}
func splitText(text string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	remaining := []rune(strings.TrimSpace(text))
	out := []string{}
	for len(remaining) > 0 {
		end := min(limit, len(remaining))
		if end < len(remaining) {
			for i := end; i > limit/2; i-- {
				if remaining[i-1] == ' ' || remaining[i-1] == '\n' {
					end = i
					break
				}
			}
		}
		chunk := strings.TrimSpace(string(remaining[:end]))
		if chunk != "" {
			out = append(out, chunk)
		}
		remaining = remaining[end:]
	}
	return out
}
