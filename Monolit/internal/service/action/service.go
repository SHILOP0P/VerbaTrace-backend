package action

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }

func (s *Service) Create(ctx context.Context, in CreateInput) (Item, error) {
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	if in.ActorUserUUID == uuid.Nil || in.CallUUID == uuid.Nil || in.AnalysisUUID == uuid.Nil || in.SourceDepartment == uuid.Nil || in.TargetDepartment == uuid.Nil || in.Title == "" || len([]rune(in.Title)) > 200 || len([]rune(in.Description)) > 10000 || !in.DueAt.After(s.now()) || len(in.Evidence) > 20 || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 200 {
		return Item{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var company uuid.UUID
	var uploadedBy uuid.UUID
	var revision int
	err = tx.QueryRowContext(ctx, `SELECT c.company_uuid,c.uploaded_by_user_uuid,COALESCE(rs.active_revision,1) FROM calls c JOIN call_analyses a ON a.call_uuid=c.call_uuid LEFT JOIN call_transcriptions t ON t.call_uuid=c.call_uuid LEFT JOIN call_transcription_revision_state rs ON rs.transcription_uuid=t.transcription_uuid WHERE c.call_uuid=$1 AND a.analysis_uuid=$2 AND a.status='done' AND c.company_uuid IS NOT NULL AND c.uploaded_by_user_uuid IS NOT NULL`, in.CallUUID, in.AnalysisUUID).Scan(&company, &uploadedBy, &revision)
	if err == sql.ErrNoRows {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	if in.AssigneeUserUUID == uuid.Nil {
		in.AssigneeUserUUID = uploadedBy
	}
	if ok, err := canCreate(ctx, tx, in.ActorUserUUID, company, in.SourceDepartment, in.CallUUID); err != nil {
		return Item{}, err
	} else if !ok {
		return Item{}, ErrForbidden
	}
	if ok, err := validAssignment(ctx, tx, company, in.TargetDepartment, in.AssigneeUserUUID); err != nil {
		return Item{}, err
	} else if !ok {
		return Item{}, ErrInvalidInput
	}
	var deptsOK bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM departments s JOIN departments t ON t.department_uuid=$2 WHERE s.department_uuid=$1 AND s.company_uuid=$3 AND t.company_uuid=$3 AND s.deleted_at IS NULL AND t.deleted_at IS NULL)`, in.SourceDepartment, in.TargetDepartment, company).Scan(&deptsOK); err != nil {
		return Item{}, err
	}
	if !deptsOK {
		return Item{}, ErrInvalidInput
	}
	actionID := uuid.New()
	now := s.now().UTC()
	insertResult, err := tx.ExecContext(ctx, `INSERT INTO call_actions(action_uuid,company_uuid,source_department_uuid,target_department_uuid,call_uuid,analysis_uuid,transcription_revision,title,description,assignee_user_uuid,due_at,grace_expires_at,created_by_user_uuid,client_request_key,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$15) ON CONFLICT(created_by_user_uuid,client_request_key) DO NOTHING`, actionID, company, in.SourceDepartment, in.TargetDepartment, in.CallUUID, in.AnalysisUUID, revision, in.Title, in.Description, in.AssigneeUserUUID, in.DueAt.UTC(), in.DueAt.UTC().Add(24*time.Hour), in.ActorUserUUID, in.IdempotencyKey, now)
	if err != nil {
		return Item{}, err
	}
	inserted, _ := insertResult.RowsAffected()
	if inserted == 0 {
		var existing uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT action_uuid FROM call_actions WHERE created_by_user_uuid=$1 AND client_request_key=$2`, in.ActorUserUUID, in.IdempotencyKey).Scan(&existing); err != nil {
			return Item{}, err
		}
		_ = tx.Rollback()
		return s.Get(ctx, existing, in.ActorUserUUID, false)
	}
	if err = s.insertEvidence(ctx, tx, actionID, in.CallUUID, revision, in.Evidence); err != nil {
		return Item{}, err
	}
	if err = insertEvent(ctx, tx, actionID, "created", in.ActorUserUUID, "", nil, map[string]any{"status": "open"}); err != nil {
		return Item{}, err
	}
	_, _ = tx.ExecContext(ctx, `UPDATE call_action_dispositions SET superseded_at=$2 WHERE analysis_uuid=$1 AND superseded_at IS NULL`, in.AnalysisUUID, now)
	_, err = tx.ExecContext(ctx, `INSERT INTO call_action_dispositions(disposition_uuid,company_uuid,call_uuid,analysis_uuid,transcription_revision,kind,created_by_user_uuid,created_at) VALUES($1,$2,$3,$4,$5,'action_created',$6,$7)`, uuid.New(), company, in.CallUUID, in.AnalysisUUID, revision, in.ActorUserUUID, now)
	if err != nil {
		return Item{}, err
	}
	if err = createNotification(ctx, tx, actionID, in.AssigneeUserUUID, "action_assigned", "Новое действие", in.Title, 1); err != nil {
		return Item{}, err
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, actionID, in.ActorUserUUID, false)
}

func (s *Service) SetNoActionRequired(ctx context.Context, actor, callID, analysisID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if actor == uuid.Nil || callID == uuid.Nil || analysisID == uuid.Nil {
		return ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var company uuid.UUID
	var revision int
	err = tx.QueryRowContext(ctx, `SELECT c.company_uuid,COALESCE(rs.active_revision,1) FROM calls c JOIN call_analyses a ON a.call_uuid=c.call_uuid LEFT JOIN call_transcriptions t ON t.call_uuid=c.call_uuid LEFT JOIN call_transcription_revision_state rs ON rs.transcription_uuid=t.transcription_uuid WHERE c.call_uuid=$1 AND a.analysis_uuid=$2 AND a.status='done' AND c.company_uuid IS NOT NULL`, callID, analysisID).Scan(&company, &revision)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var ok bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, company, actor).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	now := s.now().UTC()
	_, _ = tx.ExecContext(ctx, `UPDATE call_action_dispositions SET superseded_at=$2 WHERE analysis_uuid=$1 AND superseded_at IS NULL`, analysisID, now)
	_, err = tx.ExecContext(ctx, `INSERT INTO call_action_dispositions(disposition_uuid,company_uuid,call_uuid,analysis_uuid,transcription_revision,kind,reason,created_by_user_uuid,created_at) VALUES($1,$2,$3,$4,$5,'no_action_required',NULLIF($6,''),$7,$8)`, uuid.New(), company, callID, analysisID, revision, reason, actor, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) insertEvidence(ctx context.Context, tx *sql.Tx, actionID, callID uuid.UUID, revision int, inputs []EvidenceInput) error {
	if len(inputs) == 0 {
		return nil
	}
	var payload []byte
	err := tx.QueryRowContext(ctx, `SELECT cc.payload FROM call_transcriptions t JOIN call_transcription_revisions r ON r.transcription_uuid=t.transcription_uuid AND r.revision=$2 JOIN call_transcription_contents cc ON cc.transcription_content_uuid=r.transcription_content_uuid WHERE t.call_uuid=$1`, callID, revision).Scan(&payload)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT jsonb_build_object('words',COALESCE(words,'[]'::jsonb)) FROM call_transcriptions WHERE call_uuid=$1`, callID).Scan(&payload)
	}
	if err != nil {
		return ErrInvalidInput
	}
	var doc struct {
		Words []Word `json:"words"`
	}
	if json.Unmarshal(payload, &doc) != nil {
		return ErrInvalidInput
	}
	for i, input := range inputs {
		if input.WordStartIndex == nil || input.WordEndIndex == nil {
			q := strings.TrimSpace(input.LegacyQuote)
			if q == "" {
				return ErrInvalidInput
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO call_action_evidence(evidence_uuid,action_uuid,position,kind,quote_snapshot) VALUES($1,$2,$3,'legacy_text',$4)`, uuid.New(), actionID, i, q)
			if err != nil {
				return err
			}
			continue
		}
		start, end := *input.WordStartIndex, *input.WordEndIndex
		if start < 0 || end < start || end >= len(doc.Words) {
			return ErrInvalidInput
		}
		parts := make([]string, 0, end-start+1)
		for _, w := range doc.Words[start : end+1] {
			parts = append(parts, w.Text)
		}
		speaker := doc.Words[start].Speaker
		_, err = tx.ExecContext(ctx, `INSERT INTO call_action_evidence(evidence_uuid,action_uuid,position,kind,word_start_index,word_end_index,quote_snapshot,speaker_snapshot,start_seconds,end_seconds) VALUES($1,$2,$3,'word_range',$4,$5,$6,NULLIF($7,''),$8,$9)`, uuid.New(), actionID, i, start, end, strings.Join(parts, " "), speaker, doc.Words[start].Start, doc.Words[end].End)
		if err != nil {
			return err
		}
	}
	return nil
}

func canCreate(ctx context.Context, tx *sql.Tx, actor, company, department, callID uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM calls c WHERE c.call_uuid=$4 AND c.company_uuid=$2 AND (EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=$2 AND cm.user_uuid=$1 AND cm.status='active' AND cm.role='company_manager') OR EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=$3 AND dm.user_uuid=$1 AND dm.status='active')))`, actor, company, department, callID).Scan(&ok)
	return ok, err
}
func validAssignment(ctx context.Context, tx *sql.Tx, company, department, user uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members cm JOIN departments d ON d.company_uuid=cm.company_uuid JOIN department_members dm ON dm.department_uuid=d.department_uuid AND dm.user_uuid=cm.user_uuid WHERE cm.company_uuid=$1 AND cm.user_uuid=$3 AND cm.status='active' AND d.department_uuid=$2 AND d.deleted_at IS NULL AND dm.status='active')`, company, department, user).Scan(&ok)
	return ok, err
}

func insertEvent(ctx context.Context, tx *sql.Tx, actionID uuid.UUID, kind string, actor uuid.UUID, reason string, oldData, newData any) error {
	var oldJSON, newJSON any
	if oldData != nil {
		b, _ := json.Marshal(oldData)
		oldJSON = b
	}
	if newData != nil {
		b, _ := json.Marshal(newData)
		newJSON = b
	}
	var actorArg any
	if actor != uuid.Nil {
		actorArg = actor
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO call_action_events(event_uuid,action_uuid,event_type,actor_user_uuid,reason,old_data,new_data) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7)`, uuid.New(), actionID, kind, actorArg, reason, oldJSON, newJSON)
	return err
}

func createNotification(ctx context.Context, tx *sql.Tx, actionID, userID uuid.UUID, kind, title, body string, schedule int64) error {
	deliveryID, notificationID := uuid.New(), uuid.New()
	res, err := tx.ExecContext(ctx, `INSERT INTO call_action_notification_deliveries(delivery_uuid,action_uuid,recipient_uuid,kind,schedule_version,notification_uuid) VALUES($1,$2,$3,$4,$5,NULL) ON CONFLICT DO NOTHING`, deliveryID, actionID, userID, kind, schedule)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	entity := "call_action"
	if _, err = tx.ExecContext(ctx, `INSERT INTO notifications(notification_uuid,user_uuid,type,title,body,entity_type,entity_uuid,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,now())`, notificationID, userID, kind, title, body, entity, actionID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_action_notification_deliveries SET notification_uuid=$1 WHERE delivery_uuid=$2`, notificationID, deliveryID)
	return err
}

func nullableUUID(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := id.UUID
	return &v
}
func ptrString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}
func ptrTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}
