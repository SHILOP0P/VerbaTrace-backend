package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"verbatrace/monolit/internal/models"
)

func (r *Repository) BeginPipeline(ctx context.Context, id uuid.UUID, runKey string, revision int) error {
	result, err := r.db.ExecContext(ctx, `UPDATE call_analyses a SET pipeline_run_key=$2, transcription_revision=$3,
 result_json=CASE WHEN pipeline_run_key=$2 THEN result_json ELSE NULL END,
 result_text=CASE WHEN pipeline_run_key=$2 THEN result_text ELSE NULL END
 WHERE analysis_uuid=$1 AND $3=(SELECT COALESCE(s.active_revision,(SELECT max(rr.revision) FROM call_transcription_revisions rr WHERE rr.transcription_uuid=t.transcription_uuid),1)
 FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=a.call_uuid)`, id, runKey, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return models.ErrAnalysisSuperseded
	}
	return err
}

func (r *Repository) SaveProgress(ctx context.Context, id uuid.UUID, runKey string, revision int, raw json.RawMessage) error {
	result, err := r.db.ExecContext(ctx, `UPDATE call_analyses a SET result_json=CASE WHEN
	(COALESCE((result_json#>>'{progress,stage_rank}')::int,0),COALESCE((result_json#>>'{progress,items_done}')::int,0),COALESCE((result_json#>>'{progress,windows_done}')::int,0)) >
	(COALESCE(($4::jsonb#>>'{progress,stage_rank}')::int,0),COALESCE(($4::jsonb#>>'{progress,items_done}')::int,0),COALESCE(($4::jsonb#>>'{progress,windows_done}')::int,0))
	THEN result_json ELSE $4::jsonb END,updated_at=now()
 WHERE analysis_uuid=$1 AND pipeline_run_key=$2 AND status='processing'
 AND $3=(SELECT COALESCE(s.active_revision,(SELECT max(rr.revision) FROM call_transcription_revisions rr WHERE rr.transcription_uuid=t.transcription_uuid),1)
 FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=a.call_uuid)`, id, runKey, revision, []byte(raw))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return models.ErrAnalysisSuperseded
	}
	return err
}

// ClaimAnalysisTask prevents concurrent/redelivered workers from repeating an
// external generation. An uncertain response needs reconciliation, not a new charge.
func (r *Repository) ClaimAnalysisTask(ctx context.Context, id, analysisID uuid.UUID) (*models.AnalysisResult, error) {
	var inserted uuid.UUID
	err := r.db.QueryRowContext(ctx, `INSERT INTO call_analysis_tasks(task_uuid,analysis_uuid) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING task_uuid`, id, analysisID).Scan(&inserted)
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var raw []byte
	if err = r.db.QueryRowContext(ctx, `SELECT result FROM call_analysis_tasks WHERE task_uuid=$1 AND analysis_uuid=$2`, id, analysisID).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("analysis step %s has an uncertain provider outcome; retry analysis as a new run", id)
	}
	var result models.AnalysisResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) SaveAnalysisTask(ctx context.Context, id uuid.UUID, result models.AnalysisResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE call_analysis_tasks SET result=$2::jsonb,updated_at=now() WHERE task_uuid=$1`, id, raw)
	return err
}

// Only release a claim before any request was sent to the provider.
func (r *Repository) ReleaseAnalysisTask(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM call_analysis_tasks WHERE task_uuid=$1 AND result IS NULL`, id)
	return err
}
