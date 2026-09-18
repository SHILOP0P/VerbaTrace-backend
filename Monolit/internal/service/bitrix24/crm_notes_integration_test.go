//go:build integration

package bitrix24

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// portalStub answers the CRM methods of a Bitrix24 portal from memory.
type portalStub struct {
	comments      map[string]string
	next          int
	adds, updates int
}

func (p *portalStub) call(_ context.Context, _, method, _ string, params any, target any) error {
	fields, _ := params.(map[string]any)
	var result any
	switch method {
	case "crm.activity.get":
		result = map[string]any{"OWNER_TYPE_ID": "2", "OWNER_ID": "501"}
	case "crm.timeline.comment.add":
		p.adds++
		p.next++
		p.comments[strconv.Itoa(p.next)] = fields["fields"].(map[string]any)["COMMENT"].(string)
		result = p.next
	case "crm.timeline.comment.update":
		id := fmt.Sprint(fields["id"])
		if _, ok := p.comments[id]; !ok {
			return errors.New("bitrix api error: NOT_FOUND")
		}
		p.updates++
		p.comments[id] = fields["fields"].(map[string]any)["COMMENT"].(string)
		result = true
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, _ := json.Marshal(result)
	return json.Unmarshal(raw, target)
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestCRMNoteIsWrittenOnceAndUpdatedAfterwards(t *testing.T) {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	ctx := context.Background()

	owner := repositorytest.CreateUser(t, db)
	company, billing, application, connection := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(t, db, `INSERT INTO companies(company_uuid,name,tag,manager_user_uuid,member_limit) VALUES($1,'CRM Company','@crm-company',$2,10)`, company, owner)
	exec(t, db, `INSERT INTO company_members(company_uuid,user_uuid,role,status) VALUES($1,$2,'company_manager','active')`, company, owner)
	exec(t, db, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid) VALUES($1,'company',$2)`, billing, company)
	exec(t, db, `INSERT INTO developer_applications(application_uuid,owner_type,company_uuid,billing_account_uuid,name,environment,status) VALUES($1,'company',$2,$3,'CRM App','production','active')`, application, company, billing)
	exec(t, db, `INSERT INTO integration_connections(connection_uuid,application_uuid,company_uuid,created_by_user_uuid,name,provider,status,settings)
		VALUES($1,$2,$3,$4,'Bitrix24','bitrix24','active','{"crm_note_mode":"auto"}')`, connection, application, company, owner)
	event, item, call := uuid.New(), uuid.New(), uuid.New()
	exec(t, db, `INSERT INTO ingest_events(event_uuid,connection_uuid,external_event_id,event_type,schema_version,payload_sha256,accepted) VALUES($1,$2,'e1','call',2,'\x00',true)`, event, connection)
	exec(t, db, `INSERT INTO ingest_items(ingest_item_uuid,connection_uuid,event_uuid,external_call_id,idempotency_key,request_sha256,source_kind,title,status,stage,application_uuid,billing_account_uuid,destination_scope,destination_company_uuid,source_ref,metadata_redacted)
		VALUES($1,$2,$3,'c1','k1','\x00','connector','Звонок','completed','completed',$4,$5,'company',$6,'ref-c1','{"crm_activity_id":"77"}')`, item, connection, event, application, billing, company)
	exec(t, db, `INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, uploaded_by_user_uuid, company_uuid, visibility_scope, ingest_item_uuid, created_at)
		VALUES ($1,'Звонок','analyzed','a.mp3','a.mp3','audio/mpeg',1,$2,$3,'company',$4,'2026-09-15T10:30:00Z')`, call, owner, company, item)
	exec(t, db, `INSERT INTO call_analyses (analysis_uuid, call_uuid, status, provider, result_json, created_at, updated_at)
		VALUES ($1,$2,'done','mock_staged',$3::jsonb,now(),now())`, uuid.New(), call,
		`{"schema_version":3,"outcome":"{{speaker:B}} согласился на демонстрацию","work_on":["Назвать срок","Уточнить бюджет","Лишнее"]}`)
	exec(t, db, `INSERT INTO call_transcription_speaker_assignments (call_uuid, speaker_key, display_name, role, updated_by_user_uuid) VALUES ($1,'B','Клиент Пётр','client',$2)`, call, owner)
	exec(t, db, `INSERT INTO analytics_call_facts (call_uuid, analysis_uuid, occurred_at, schema_version, scorecard_mode, ai_overall_score, criteria_score, coverage_status) VALUES ($1,$2,'2026-09-15T10:30:00Z',3,'fixed',64,58,'complete')`, call, uuid.New())

	portal := &portalStub{comments: map[string]string{}}
	service := NewService(db, nil, Config{})
	service.SetAppURL("https://app.example")
	service.portalCall = portal.call
	service.tokenFor = func(context.Context, uuid.UUID) (connectionInfo, string, error) {
		return connectionInfo{Domain: "portal.example"}, "token", nil
	}

	service.QueueCRMNote(ctx, call)
	service.processCRMNotes(ctx)
	require.Equal(t, 1, portal.adds)
	comment := portal.comments["1"]
	require.Contains(t, comment, "VerbaTrace: разбор звонка от 15.09.2026 13:30")
	require.Contains(t, comment, "Оценка: 64 из 100, по критериям: 58")
	require.Contains(t, comment, "Итог: Клиент Пётр согласился на демонстрацию", "speakers are named, not marked")
	require.Contains(t, comment, "Над чем поработать: Назвать срок; Уточнить бюджет")
	require.NotContains(t, comment, "Лишнее")
	require.NotContains(t, comment, "Оценку проверил человек")
	require.Contains(t, comment, "https://app.example/app/calls?call="+call.String())

	// A QA revision or a new analysis updates the same comment.
	exec(t, db, `UPDATE analytics_call_facts SET human_overall_score = 70 WHERE call_uuid = $1`, call)
	service.QueueCRMNote(ctx, call)
	service.processCRMNotes(ctx)
	require.Equal(t, 1, portal.adds, "a repeat creates no second comment")
	require.Equal(t, 1, portal.updates)
	require.True(t, strings.Contains(portal.comments["1"], "Оценка: 70 из 100"))
	require.Contains(t, portal.comments["1"], "Оценку проверил человек.")

	// A comment deleted in the card is written again.
	delete(portal.comments, "1")
	service.QueueCRMNote(ctx, call)
	service.processCRMNotes(ctx)
	require.Equal(t, 2, portal.adds)

	// Switched off, nothing is queued.
	exec(t, db, `UPDATE integration_connections SET settings = '{"crm_note_mode":"off"}' WHERE connection_uuid = $1`, connection)
	service.QueueCRMNote(ctx, call)
	service.processCRMNotes(ctx)
	require.Equal(t, 2, portal.adds)
	require.Equal(t, 1, portal.updates)
}
