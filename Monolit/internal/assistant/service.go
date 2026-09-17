package assistant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/call"

	"github.com/google/uuid"
)

var (
	ErrForbidden           = errors.New("assistant access forbidden")
	ErrNotFound            = errors.New("assistant resource not found")
	ErrInvalidInput        = errors.New("invalid assistant input")
	ErrRunInProgress       = errors.New("assistant run in progress")
	ErrProviderUnavailable = errors.New("assistant provider unavailable")
	ErrVersionConflict     = errors.New("assistant version conflict")
)

var technicalUUIDPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)

const maxSearchLimit = 50

type Service struct {
	db        *sql.DB
	embedder  Embedder
	generator Generator
	credits   CreditMeter
	now       func() time.Time
}

type CreditMeter interface {
	ReserveAssistantGeneration(ctx context.Context, userID, companyID, departmentID, runID uuid.UUID, inputTokens, maxOutputTokens int64, provider, model string) (uuid.UUID, error)
	SettleAssistantGeneration(context.Context, uuid.UUID, *models.ProviderUsage) error
	ReserveEmbedding(ctx context.Context, userID, companyID, departmentID uuid.UUID, reference string, inputTokens int64, provider, model string) (uuid.UUID, error)
	SettleEmbedding(ctx context.Context, operationID uuid.UUID, inputTokens int64, provider, model string) error
	MarkCreditOperationReconciling(context.Context, uuid.UUID, string) error
}

func NewService(db *sql.DB, embedder Embedder, generator Generator) *Service {
	return &Service{db: db, embedder: embedder, generator: generator, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetCreditMeter(meter CreditMeter) { s.credits = meter }

// RunRecoveryWorker resumes durable runs left active by an application restart.
// The assistant message and run completion share one transaction, so replay
// cannot create a second completed answer for the same run.
func (s *Service) RunRecoveryWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			_ = s.recoverRuns(ctx, 5)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func (s *Service) recoverRuns(ctx context.Context, limit int) error {
	rows, err := s.db.QueryContext(ctx, `SELECT ar.assistant_run_uuid,ar.assistant_chat_uuid,ar.actor_user_uuid,COALESCE(ar.company_uuid,'00000000-0000-0000-0000-000000000000'::uuid),ar.response_detail,ar.request_received_at,ar.filter_json,um.content_json
		FROM assistant_runs ar JOIN assistant_messages um ON um.assistant_message_uuid=ar.user_message_uuid
		WHERE ar.state='queued' OR (ar.state IN ('preparing','retrieving','generating','validating') AND ar.request_received_at < now()-interval '2 minutes')
		ORDER BY ar.request_received_at LIMIT $1`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type pending struct {
		run             models.AssistantRun
		user, company   uuid.UUID
		filter, content []byte
	}
	items := []pending{}
	for rows.Next() {
		var item pending
		if err = rows.Scan(&item.run.ID, &item.run.ChatUUID, &item.user, &item.company, &item.run.ResponseDetail, &item.run.RequestReceivedAt, &item.filter, &item.content); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		claimed, claimErr := s.db.ExecContext(ctx, `UPDATE assistant_runs SET state='retrieving' WHERE assistant_run_uuid=$1 AND (state='queued' OR (state IN ('preparing','retrieving','generating','validating') AND request_received_at < now()-interval '2 minutes'))`, item.run.ID)
		if claimErr != nil {
			continue
		}
		affected, _ := claimed.RowsAffected()
		if affected == 0 {
			continue
		}
		var content struct {
			Text string `json:"text"`
		}
		var filter struct {
			CallIDs       []uuid.UUID `json:"call_uuids"`
			DepartmentIDs []uuid.UUID `json:"department_uuids"`
			FolderIDs     []uuid.UUID `json:"folder_uuids"`
			From          *time.Time  `json:"from"`
			To            *time.Time  `json:"to"`
		}
		if json.Unmarshal(item.content, &content) != nil || json.Unmarshal(item.filter, &filter) != nil {
			_ = s.failRun(ctx, item.run.ID, "recovery_payload_invalid")
			continue
		}
		in := models.CreateAssistantMessageInput{UserUUID: item.user, CompanyUUID: item.company, ChatUUID: item.run.ChatUUID, Text: content.Text, ResponseDetail: item.run.ResponseDetail, CallIDs: filter.CallIDs, DepartmentIDs: filter.DepartmentIDs, FolderIDs: filter.FolderIDs, From: filter.From, To: filter.To}
		_, _ = s.completeRun(ctx, item.run, in)
	}
	return nil
}

func (s *Service) Capabilities(ctx context.Context, user, company uuid.UUID) (models.AssistantCapabilities, error) {
	c := models.AssistantCapabilities{CompanyUUID: company, DepartmentUUIDs: []uuid.UUID{}}
	if user == uuid.Nil {
		return c, ErrInvalidInput
	}
	eligible := company == uuid.Nil
	c.Role = "personal"
	if company != uuid.Nil {
		err := s.db.QueryRowContext(ctx, `SELECT cm.role FROM company_members cm JOIN companies co USING(company_uuid) WHERE cm.company_uuid=$1 AND cm.user_uuid=$2 AND cm.status='active' AND co.deleted_at IS NULL`, company, user).Scan(&c.Role)
		if errors.Is(err, sql.ErrNoRows) {
			c.ReasonCode = "membership_required"
			return c, nil
		}
		if err != nil {
			return c, err
		}
		eligible = managesCompany(c.Role)
		rows, err := s.db.QueryContext(ctx, `SELECT dm.department_uuid,dm.role FROM department_members dm JOIN departments d USING(department_uuid) WHERE d.company_uuid=$1 AND d.deleted_at IS NULL AND dm.user_uuid=$2 AND dm.status='active'`, company, user)
		if err != nil {
			return c, err
		}
		for rows.Next() {
			var id uuid.UUID
			var role string
			if err = rows.Scan(&id, &role); err != nil {
				_ = rows.Close()
				return c, err
			}
			c.DepartmentUUIDs = append(c.DepartmentUUIDs, id)
			if role == "department_leader" {
				eligible = true
				if !managesCompany(c.Role) {
					c.Role = role
				}
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return c, err
		}
	}
	var enabled, research, export bool
	// A business plan belongs to the owner of the company, not to the company, so
	// the company branch resolves the owner first. Reading it any other way makes
	// the assistant disagree with the credit meter about who is covered.
	err := s.db.QueryRowContext(ctx, `SELECT p.assistant_enabled,p.assistant_research_enabled,p.export_enabled FROM subscriptions sub JOIN plans p USING(plan_uuid)
		WHERE sub.status='active' AND sub.starts_at<=now() AND (sub.ends_at IS NULL OR sub.ends_at>now())
		  AND (
		      ($2::uuid IS NULL AND sub.type='personal' AND sub.user_uuid=$1)
		      OR ($2::uuid IS NOT NULL AND sub.type='business' AND sub.user_uuid IN (
		          SELECT manager_user_uuid FROM companies WHERE company_uuid=$2 AND deleted_at IS NULL AND lifecycle_state='active'
		      ))
		  )
		ORDER BY sub.starts_at DESC LIMIT 1`, user, optionalCompany(company)).Scan(&enabled, &research, &export)
	if errors.Is(err, sql.ErrNoRows) {
		c.ReasonCode = "subscription_required"
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.SearchEnabled = true
	c.ChatEnabled = eligible && enabled
	c.AggregateEnabled = c.ChatEnabled && research
	c.ExportEnabled = c.ChatEnabled && export
	if !enabled {
		c.ReasonCode = "assistant_plan_required"
	} else if !eligible {
		c.ReasonCode = "assistant_role_required"
	}
	return c, nil
}
func optionalCompany(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// managesCompany mirrors models.CompanyMemberRole.ManagesCompany: the deputy has
// the manager's reach, and the assistant must not be the one place that forgets
// it.
func managesCompany(role string) bool {
	return models.CompanyMemberRole(role).ManagesCompany()
}

// departmentForUser answers which department's credit limit an operation should
// count against. One person has at most one active department inside a company,
// so there is nothing to choose between; an owner or deputy without a department
// spends against the company itself.
func (s *Service) departmentForUser(ctx context.Context, companyID, userID uuid.UUID) uuid.UUID {
	if companyID == uuid.Nil || userID == uuid.Nil {
		return uuid.Nil
	}
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT dm.department_uuid FROM department_members dm JOIN departments d USING(department_uuid)
		WHERE d.company_uuid=$1 AND d.deleted_at IS NULL AND dm.user_uuid=$2 AND dm.status='active' LIMIT 1`, companyID, userID).Scan(&id)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// reserveQueryEmbedding books the cost of vectorising one search query. It
// reports whether the search may go ahead in vector mode; without a credit meter
// wired in it simply says yes.
func (s *Service) reserveQueryEmbedding(ctx context.Context, userID, companyID uuid.UUID, query string) (uuid.UUID, int64, bool) {
	tokens := int64((len([]byte(query)) + 2) / 3)
	if s.credits == nil {
		return uuid.Nil, tokens, true
	}
	provider, model, _ := s.embedder.Profile()
	reference := fmt.Sprintf("search:%s:%s:%x", userID, companyID, sha256.Sum256([]byte(query)))
	operationID, err := s.credits.ReserveEmbedding(ctx, userID, companyID, s.departmentForUser(ctx, companyID, userID), reference, tokens, provider, model)
	if err != nil {
		return uuid.Nil, tokens, false
	}

	return operationID, tokens, true
}

func scopeSQL() string {
	return "c.company_uuid IS NOT DISTINCT FROM NULLIF($2::uuid,'00000000-0000-0000-0000-000000000000'::uuid)"
}

func (s *Service) ContentSearch(ctx context.Context, in models.ContentSearchInput) (models.ContentSearchResult, error) {
	in.Query = strings.TrimSpace(in.Query)
	if in.UserUUID == uuid.Nil || utf8.RuneCountInString(in.Query) < 2 || utf8.RuneCountInString(in.Query) > 12000 || len(in.CallIDs) > 100 || len(in.DepartmentIDs) > 100 || len(in.FolderIDs) > 100 || (in.From != nil && in.To != nil && !in.From.Before(*in.To)) {
		return models.ContentSearchResult{}, ErrInvalidInput
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > maxSearchLimit {
		return models.ContentSearchResult{}, ErrInvalidInput
	}
	cap, err := s.Capabilities(ctx, in.UserUUID, in.CompanyUUID)
	if err != nil {
		return models.ContentSearchResult{}, err
	}
	if !cap.SearchEnabled {
		return models.ContentSearchResult{}, ErrForbidden
	}
	if err = validateDepartments(in.DepartmentIDs, cap); err != nil {
		return models.ContentSearchResult{}, err
	}
	vectorMode := false
	var vector string
	if s.embedder != nil && s.embedder.Enabled() {
		semanticQuery := strings.TrimSpace(in.SemanticQuery)
		if semanticQuery == "" {
			semanticQuery = in.Query
		}
		// Turning the question into a vector costs money too. When the credit
		// limit refuses it the search still answers, lexically: a cap on spending
		// should degrade the result, not withhold it.
		if creditOperation, tokens, ok := s.reserveQueryEmbedding(ctx, in.UserUUID, in.CompanyUUID, semanticQuery); ok {
			vectors, _, embedErr := s.embedder.Embed(ctx, []string{semanticQuery}, "search_query")
			provider, model, _ := s.embedder.Profile()
			switch {
			case embedErr == nil && len(vectors) == 1:
				vectorMode = true
				vector = vectorLiteral(vectors[0])
				if s.credits != nil && creditOperation != uuid.Nil {
					_ = s.credits.SettleEmbedding(ctx, creditOperation, tokens, provider, model)
				}
			default:
				if s.credits != nil && creditOperation != uuid.Nil {
					_ = s.credits.MarkCreditOperationReconciling(ctx, creditOperation, "search embedding unavailable")
				}
			}
		}
	}
	args := []any{in.UserUUID, in.CompanyUUID, lexicalQuery(in.Query), in.Limit}
	where, extra := filterSQL(in, 5)
	args = append(args, extra...)
	score := `ts_rank_cd(ch.text_search || to_tsvector('russian',c.title),websearch_to_tsquery('russian',$3))`
	order := score + ` DESC`
	mode := "lexical"
	if vectorMode {
		args = append(args, vector)
		vp := len(args)
		// Keep lexical matches useful without letting one common keyword outrank
		// a much closer semantic match. The previous +1 lexical bonus dominated
		// cosine similarity and surfaced unrelated fragments.
		score = fmt.Sprintf(`(0.82*GREATEST(0,1-(ch.embedding <=> $%d::vector)) + 0.18*LEAST(1,ts_rank_cd(ch.text_search || to_tsvector('russian',c.title),websearch_to_tsquery('russian',$3))*4))`, vp)
		order = score + ` DESC`
		mode = "hybrid"
	}
	query := `SELECT ch.call_search_chunk_uuid,c.call_uuid,c.title,ch.text,ch.speaker,ch.start_seconds,ch.end_seconds,c.created_at,GREATEST(c.created_at,t.updated_at),d.transcription_revision,` + score + `,ch.source_kind FROM call_search_chunks ch JOIN call_search_documents d ON d.call_search_document_uuid=ch.call_search_document_uuid AND d.status='ready' JOIN search_index_profiles p ON p.search_index_profile_uuid=d.profile_uuid AND p.status='active' JOIN calls c ON c.call_uuid=d.call_uuid JOIN call_transcriptions t ON t.transcription_uuid=d.transcription_uuid WHERE ` + scopeSQL() + ` AND (` + callAccessSQL() + `) ` + where + ` AND ` + currentDocumentSQL() + ` AND ((ch.text_search || to_tsvector('russian',c.title)) @@ websearch_to_tsquery('russian',$3)`
	if vectorMode {
		query += ` OR ch.embedding IS NOT NULL`
	}
	query += `)`
	if vectorMode {
		args = append(args, 0.35)
		query += fmt.Sprintf(` AND %s >= $%d`, score, len(args))
	}
	query += ` ORDER BY ` + order + `,c.call_uuid,ch.ordinal LIMIT $4`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.ContentSearchResult{}, err
	}
	defer func() { _ = rows.Close() }()
	items := []models.ContentSearchItem{}
	for rows.Next() {
		var x models.ContentSearchItem
		if err = rows.Scan(&x.ChunkUUID, &x.CallUUID, &x.Title, &x.Quote, &x.Speaker, &x.StartSeconds, &x.EndSeconds, &x.CreatedAt, &x.UpdatedAt, &x.Revision, &x.Score, &x.SourceKind); err != nil {
			return models.ContentSearchResult{}, err
		}
		x.RetrievalMode = mode
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		return models.ContentSearchResult{}, err
	}
	var evaluated, indexed int
	countWhere, countExtra := filterSQL(in, 3)
	countQ := `SELECT count(DISTINCT c.call_uuid),count(DISTINCT d.call_uuid) FROM calls c LEFT JOIN call_search_documents d ON d.call_uuid=c.call_uuid AND d.status='ready' AND ` + currentDocumentSQL() + ` WHERE ` + scopeSQL() + ` AND (` + callAccessSQL() + `) ` + countWhere
	countArgs := []any{in.UserUUID, in.CompanyUUID}
	countArgs = append(countArgs, countExtra...)
	if err = s.db.QueryRowContext(ctx, countQ, countArgs...).Scan(&evaluated, &indexed); err != nil {
		return models.ContentSearchResult{}, err
	}
	warnings := []string{}
	if indexed < evaluated {
		warnings = append(warnings, "Часть доступных звонков ещё индексируется")
	}
	if mode == "lexical" {
		warnings = append(warnings, "Семантический провайдер недоступен: выполнен полнотекстовый поиск")
	}
	return models.ContentSearchResult{Items: items, RetrievalMode: mode, EvaluatedCalls: evaluated, IndexReadyCalls: indexed, Warnings: warnings}, nil
}

func (s *Service) fullSelectedContent(ctx context.Context, in models.ContentSearchInput) (models.ContentSearchResult, error) {
	if in.UserUUID == uuid.Nil || len(in.CallIDs) == 0 || len(in.CallIDs) > 100 {
		return models.ContentSearchResult{}, ErrInvalidInput
	}
	cap, err := s.Capabilities(ctx, in.UserUUID, in.CompanyUUID)
	if err != nil {
		return models.ContentSearchResult{}, err
	}
	if !cap.SearchEnabled {
		return models.ContentSearchResult{}, ErrForbidden
	}
	if err = validateDepartments(in.DepartmentIDs, cap); err != nil {
		return models.ContentSearchResult{}, err
	}
	args := []any{in.UserUUID, in.CompanyUUID}
	where, extra := filterSQL(in, 3)
	args = append(args, extra...)
	query := `SELECT ch.call_search_chunk_uuid,c.call_uuid,c.title,ch.text,ch.speaker,ch.start_seconds,ch.end_seconds,c.created_at,GREATEST(c.created_at,t.updated_at),d.transcription_revision,1::double precision,ch.source_kind
	FROM call_search_chunks ch
	JOIN call_search_documents d ON d.call_search_document_uuid=ch.call_search_document_uuid AND d.status='ready'
	JOIN search_index_profiles p ON p.search_index_profile_uuid=d.profile_uuid AND p.status='active'
	JOIN calls c ON c.call_uuid=d.call_uuid
	JOIN call_transcriptions t ON t.transcription_uuid=d.transcription_uuid
	WHERE ` + scopeSQL() + ` AND (` + callAccessSQL() + `) ` + where + ` AND ` + currentDocumentSQL() + `
	ORDER BY c.created_at,c.call_uuid,ch.ordinal`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.ContentSearchResult{}, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.ContentSearchItem, 0)
	indexed := map[uuid.UUID]bool{}
	for rows.Next() {
		var item models.ContentSearchItem
		if err = rows.Scan(&item.ChunkUUID, &item.CallUUID, &item.Title, &item.Quote, &item.Speaker, &item.StartSeconds, &item.EndSeconds, &item.CreatedAt, &item.UpdatedAt, &item.Revision, &item.Score, &item.SourceKind); err != nil {
			return models.ContentSearchResult{}, err
		}
		item.RetrievalMode = "full_selected_calls"
		items = append(items, item)
		indexed[item.CallUUID] = true
	}
	if err = rows.Err(); err != nil {
		return models.ContentSearchResult{}, err
	}
	warnings := []string{}
	if len(indexed) < len(in.CallIDs) {
		warnings = append(warnings, "Часть выбранных звонков ещё индексируется или недоступна")
	}
	return models.ContentSearchResult{Items: items, RetrievalMode: "full_selected_calls", EvaluatedCalls: len(in.CallIDs), IndexReadyCalls: len(indexed), Warnings: warnings}, nil
}

func requestsFullConversationReview(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	markers := []string{"полный анализ", "полностью проанализ", "разбери весь", "проанализируй весь", "все вопросы", "каждый вопрос", "весь разговор", "весь звонок", "всю беседу", "все нарушения", "каждое нарушение", "все требования", "каждое требование", "all questions", "full analysis", "entire conversation", "every requirement"}
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	// Cover natural variations without coupling the route to a closed list of
	// conversation domains. A completeness quantifier must be paired with an
	// object that can only be checked by reading the selected material in full.
	quantified := containsAny(value, "весь", "всю", "всё", "все ", "всех ", "кажд", "полност", "целиком", "all ", "every ", "entire ", "complete ")
	material := containsAny(value, "вопрос", "ответ", "реплик", "эпизод", "разговор", "диалог", "звонок", "бесед", "требован", "инструкц", "нарушен", "conversation", "call", "question", "answer", "requirement", "violation")
	return quantified && material
}

func containsAny(value string, values ...string) bool {
	for _, candidate := range values {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

// callAccessSQL reuses the one predicate that decides who sees a call, so the
// assistant can never surface a call the calls list itself hides.
func callAccessSQL() string {
	return call.VisibleToUserCondition("c", "$1")
}
func validateDepartments(ids []uuid.UUID, c models.AssistantCapabilities) error {
	if managesCompany(c.Role) {
		return nil
	}
	allowed := map[uuid.UUID]bool{}
	for _, id := range c.DepartmentUUIDs {
		allowed[id] = true
	}
	for _, id := range ids {
		if !allowed[id] {
			return ErrForbidden
		}
	}
	return nil
}
func filterSQL(in models.ContentSearchInput, start int) (string, []any) {
	parts := []string{}
	args := []any{}
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", start+len(args)-1) }
	if len(in.CallIDs) > 0 {
		p := []string{}
		for _, id := range in.CallIDs {
			p = append(p, add(id))
		}
		parts = append(parts, "c.call_uuid IN ("+strings.Join(p, ",")+")")
	}
	if len(in.DepartmentIDs) > 0 {
		p := []string{}
		for _, id := range in.DepartmentIDs {
			p = append(p, add(id))
		}
		parts = append(parts, "c.department_uuid IN ("+strings.Join(p, ",")+")")
	}
	if len(in.FolderIDs) > 0 {
		p := []string{}
		for _, id := range in.FolderIDs {
			p = append(p, add(id))
		}
		parts = append(parts, "EXISTS(SELECT 1 FROM call_folder_assignments fa WHERE fa.call_uuid=c.call_uuid AND fa.folder_uuid IN ("+strings.Join(p, ",")+"))")
	}
	if in.From != nil {
		parts = append(parts, "c.created_at >= "+add(in.From.UTC()))
	}
	if in.To != nil {
		parts = append(parts, "c.created_at < "+add(in.To.UTC()))
	}
	if len(parts) == 0 {
		return "", args
	}
	return " AND " + strings.Join(parts, " AND "), args
}

func (s *Service) ListChats(ctx context.Context, user, company uuid.UUID) ([]models.AssistantChat, error) {
	if _, err := s.requireChat(ctx, user, company); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT assistant_chat_uuid,COALESCE(company_uuid,'00000000-0000-0000-0000-000000000000'::uuid),title,response_detail,archived_at,lock_version,created_at,updated_at FROM assistant_chats WHERE owner_user_uuid=$1 AND company_uuid IS NOT DISTINCT FROM NULLIF($2::uuid,'00000000-0000-0000-0000-000000000000'::uuid) AND deleted_at IS NULL ORDER BY updated_at DESC LIMIT 100`, user, company)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []models.AssistantChat{}
	for rows.Next() {
		var x models.AssistantChat
		if err = rows.Scan(&x.ID, &x.CompanyUUID, &x.Title, &x.ResponseDetail, &x.ArchivedAt, &x.LockVersion, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) CreateChat(ctx context.Context, user, company uuid.UUID, title, detail string) (models.AssistantChat, error) {
	if _, err := s.requireChat(ctx, user, company); err != nil {
		return models.AssistantChat{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Новое исследование"
	}
	title = truncateRunes(title, 120)
	if !validDetail(detail) {
		detail = "auto"
	}
	id, _ := uuid.NewV7()
	var x models.AssistantChat
	err := s.db.QueryRowContext(ctx, `INSERT INTO assistant_chats(assistant_chat_uuid,company_uuid,owner_user_uuid,title,response_detail) VALUES($1,$2,$3,$4,$5) RETURNING assistant_chat_uuid,COALESCE(company_uuid,'00000000-0000-0000-0000-000000000000'::uuid),title,response_detail,archived_at,lock_version,created_at,updated_at`, id, optionalCompany(company), user, title, detail).Scan(&x.ID, &x.CompanyUUID, &x.Title, &x.ResponseDetail, &x.ArchivedAt, &x.LockVersion, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}
func (s *Service) ListMessages(ctx context.Context, user, chat uuid.UUID) ([]models.AssistantMessage, error) {
	company, _, err := s.ownedChat(ctx, user, chat)
	if err != nil {
		return nil, err
	}
	if _, err = s.requireChat(ctx, user, company); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT assistant_message_uuid,assistant_chat_uuid,sequence,role,status,content_json,created_at,completed_at FROM assistant_messages WHERE assistant_chat_uuid=$1 ORDER BY sequence`, chat)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []models.AssistantMessage{}
	for rows.Next() {
		var x models.AssistantMessage
		var raw []byte
		if err = rows.Scan(&x.ID, &x.ChatUUID, &x.Sequence, &x.Role, &x.Status, &raw, &x.CreatedAt, &x.CompletedAt); err != nil {
			return nil, err
		}
		var content struct {
			Text   string                  `json:"text"`
			Blocks []models.AssistantBlock `json:"blocks"`
		}
		if err = json.Unmarshal(raw, &content); err != nil {
			return nil, err
		}
		x.Text = content.Text
		x.Blocks = content.Blocks
		if x.Role == "assistant" {
			available, checkErr := s.messageSourcesAvailable(ctx, user, company, x)
			if checkErr != nil {
				return nil, checkErr
			}
			if !available {
				x.Status = "unavailable"
				x.Text = "Источники ответа изменились или больше недоступны. Выполните новый запрос."
				x.Blocks = []models.AssistantBlock{}
				x.Sources = []models.AssistantSource{}
				out = append(out, x)
				continue
			}
		}
		if err = s.loadSources(ctx, &x); err != nil {
			return nil, err
		}
		if err = s.loadArtifacts(ctx, &x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) ExportChatMarkdown(ctx context.Context, user, chat uuid.UUID) ([]byte, string, error) {
	company, _, err := s.ownedChat(ctx, user, chat)
	if err != nil {
		return nil, "", err
	}
	capabilities, err := s.requireChat(ctx, user, company)
	if err != nil {
		return nil, "", err
	}
	if !capabilities.ExportEnabled {
		return nil, "", ErrForbidden
	}
	var title string
	if err = s.db.QueryRowContext(ctx, `SELECT title FROM assistant_chats WHERE assistant_chat_uuid=$1`, chat).Scan(&title); err != nil {
		return nil, "", err
	}
	messages, err := s.ListMessages(ctx, user, chat)
	if err != nil {
		return nil, "", err
	}
	var out strings.Builder
	out.WriteString("# " + title + "\n\n")
	for _, message := range messages {
		if message.Status == "unavailable" {
			continue
		}
		if message.Role == "user" {
			out.WriteString("## Вопрос\n\n")
		} else {
			out.WriteString("## Ответ\n\n")
		}
		out.WriteString(message.Text + "\n\n")
		if len(message.Sources) > 0 {
			out.WriteString("### Источники\n\n")
			for _, source := range message.Sources {
				_, _ = fmt.Fprintf(&out, "- **%s**, версия %d: %s\n", source.CallTitle, source.Revision, source.Quote)
			}
			out.WriteString("\n")
		}
		for _, artifact := range message.Artifacts {
			out.WriteString("### " + artifact.Title + "\n\n")
			var data struct {
				Basis     string `json:"basis"`
				Total     int    `json:"total"`
				Evaluated int    `json:"evaluated_calls"`
				Indexed   int    `json:"indexed_calls"`
				Items     []struct {
					Label      string  `json:"label"`
					Count      int     `json:"count"`
					Percentage float64 `json:"percentage"`
				} `json:"items"`
			}
			if json.Unmarshal(artifact.Data, &data) == nil {
				_, _ = fmt.Fprintf(&out, "Основание: %s (%d). Проиндексировано %d из %d звонков.\n\n| Звонок | Фрагменты | Доля |\n|---|---:|---:|\n", data.Basis, data.Total, data.Indexed, data.Evaluated)
				for _, row := range data.Items {
					_, _ = fmt.Fprintf(&out, "| %s | %d | %.1f%% |\n", strings.ReplaceAll(row.Label, "|", "\\|"), row.Count, row.Percentage)
				}
				out.WriteString("\n")
			}
		}
	}
	return []byte(out.String()), "исследование.md", nil
}

func (s *Service) GetDraft(ctx context.Context, user, company, chat uuid.UUID) (models.AssistantDraft, error) {
	if _, err := s.requireChat(ctx, user, company); err != nil {
		return models.AssistantDraft{}, err
	}
	if chat != uuid.Nil {
		owned, _, err := s.ownedChat(ctx, user, chat)
		if err != nil || owned != company {
			return models.AssistantDraft{}, ErrNotFound
		}
	}
	var draft models.AssistantDraft
	err := s.db.QueryRowContext(ctx, `SELECT text,context_json,lock_version,updated_at FROM assistant_scope_drafts WHERE owner_user_uuid=$1 AND company_uuid IS NOT DISTINCT FROM $2::uuid AND assistant_chat_uuid IS NOT DISTINCT FROM $3::uuid`, user, optionalCompany(company), optionalCompany(chat)).Scan(&draft.Text, &draft.Context, &draft.LockVersion, &draft.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		draft.Context = json.RawMessage(`{}`)
		return draft, nil
	}
	return draft, err
}

func (s *Service) SaveDraft(ctx context.Context, user, company, chat uuid.UUID, text string, contextJSON json.RawMessage, expectedVersion int64) (models.AssistantDraft, error) {
	if utf8.RuneCountInString(text) > 12000 || !json.Valid(contextJSON) || len(contextJSON) > 64<<10 || expectedVersion < 0 {
		return models.AssistantDraft{}, ErrInvalidInput
	}
	if _, err := s.requireChat(ctx, user, company); err != nil {
		return models.AssistantDraft{}, err
	}
	if chat != uuid.Nil {
		owned, _, err := s.ownedChat(ctx, user, chat)
		if err != nil || owned != company {
			return models.AssistantDraft{}, ErrNotFound
		}
	}
	id, _ := uuid.NewV7()
	var draft models.AssistantDraft
	err := s.db.QueryRowContext(ctx, `INSERT INTO assistant_scope_drafts(assistant_scope_draft_uuid,owner_user_uuid,company_uuid,assistant_chat_uuid,text,context_json)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(owner_user_uuid,company_uuid,assistant_chat_uuid) DO UPDATE SET text=EXCLUDED.text,context_json=EXCLUDED.context_json,lock_version=assistant_scope_drafts.lock_version+1,updated_at=now()
		WHERE assistant_scope_drafts.lock_version=$7
		RETURNING text,context_json,lock_version,updated_at`, id, user, optionalCompany(company), optionalCompany(chat), text, contextJSON, expectedVersion).Scan(&draft.Text, &draft.Context, &draft.LockVersion, &draft.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AssistantDraft{}, ErrVersionConflict
	}
	return draft, err
}

func (s *Service) DeleteDraft(ctx context.Context, user, company, chat uuid.UUID) error {
	if _, err := s.requireChat(ctx, user, company); err != nil {
		return err
	}
	if chat != uuid.Nil {
		owned, _, err := s.ownedChat(ctx, user, chat)
		if err != nil || owned != company {
			return ErrNotFound
		}
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM assistant_scope_drafts WHERE owner_user_uuid=$1 AND company_uuid IS NOT DISTINCT FROM $2::uuid AND assistant_chat_uuid IS NOT DISTINCT FROM $3::uuid`, user, optionalCompany(company), optionalCompany(chat))
	return err
}

func (s *Service) CreateMessage(ctx context.Context, in models.CreateAssistantMessageInput) (models.AssistantRun, error) {
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || utf8.RuneCountInString(in.Text) > 12000 || len(in.ContextLabels) > 100 || in.ClientMessageID == "" || in.IdempotencyKey == "" {
		return models.AssistantRun{}, ErrInvalidInput
	}
	for _, label := range in.ContextLabels {
		if utf8.RuneCountInString(label) > 180 {
			return models.AssistantRun{}, ErrInvalidInput
		}
	}
	company, detail, err := s.ownedChat(ctx, in.UserUUID, in.ChatUUID)
	if err != nil {
		return models.AssistantRun{}, err
	}
	if company != in.CompanyUUID {
		return models.AssistantRun{}, ErrNotFound
	}
	if !validDetail(in.ResponseDetail) {
		in.ResponseDetail = detail
	}
	if _, err = s.requireChat(ctx, in.UserUUID, company); err != nil {
		return models.AssistantRun{}, err
	}
	hash := messageRequestHash(in)
	var existing models.AssistantRun
	var existingHash []byte
	err = s.db.QueryRowContext(ctx, `SELECT assistant_run_uuid,assistant_chat_uuid,state,response_detail,data_snapshot_at,request_received_at,completed_at,COALESCE(error_code,''),request_hash FROM assistant_runs WHERE company_uuid IS NOT DISTINCT FROM $1::uuid AND actor_user_uuid=$2 AND idempotency_key=$3`, optionalCompany(company), in.UserUUID, in.IdempotencyKey).Scan(&existing.ID, &existing.ChatUUID, &existing.State, &existing.ResponseDetail, &existing.DataSnapshotAt, &existing.RequestReceivedAt, &existing.CompletedAt, &existing.ErrorCode, &existingHash)
	if err == nil {
		if !bytes.Equal(existingHash, hash[:]) {
			return models.AssistantRun{}, ErrInvalidInput
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.AssistantRun{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.AssistantRun{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var locked uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT assistant_chat_uuid FROM assistant_chats WHERE assistant_chat_uuid=$1 AND owner_user_uuid=$2 AND deleted_at IS NULL FOR UPDATE`, in.ChatUUID, in.UserUUID).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.AssistantRun{}, ErrNotFound
		}
		return models.AssistantRun{}, err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM assistant_runs WHERE assistant_chat_uuid=$1 AND state IN ('queued','preparing','retrieving','generating','validating')`, in.ChatUUID).Scan(&active); err != nil {
		return models.AssistantRun{}, err
	}
	if active > 0 {
		return models.AssistantRun{}, ErrRunInProgress
	}
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM assistant_messages WHERE assistant_chat_uuid=$1`, in.ChatUUID).Scan(&seq); err != nil {
		return models.AssistantRun{}, err
	}
	userMsg, _ := uuid.NewV7()
	runID, _ := uuid.NewV7()
	blocks := []models.AssistantBlock{{Type: "text", Text: in.Text}}
	if len(in.ContextLabels) > 0 {
		labels, _ := json.Marshal(map[string]any{"labels": in.ContextLabels})
		blocks = append(blocks, models.AssistantBlock{Type: "context", Data: labels})
	}
	content, _ := json.Marshal(models.AssistantMessage{Text: in.Text, Blocks: blocks})
	if _, err = tx.ExecContext(ctx, `INSERT INTO assistant_messages(assistant_message_uuid,assistant_chat_uuid,sequence,role,content_json,client_message_id,status,completed_at) VALUES($1,$2,$3,'user',$4,$5,'completed',now())`, userMsg, in.ChatUUID, seq, content, in.ClientMessageID); err != nil {
		return models.AssistantRun{}, err
	}
	filter, _ := json.Marshal(map[string]any{"call_uuids": in.CallIDs, "department_uuids": in.DepartmentIDs, "folder_uuids": in.FolderIDs, "from": in.From, "to": in.To})
	if _, err = tx.ExecContext(ctx, `INSERT INTO assistant_runs(assistant_run_uuid,assistant_chat_uuid,user_message_uuid,actor_user_uuid,company_uuid,idempotency_key,request_hash,filter_json,response_detail,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'queued')`, runID, in.ChatUUID, userMsg, in.UserUUID, optionalCompany(company), in.IdempotencyKey, hash[:], filter, in.ResponseDetail); err != nil {
		return models.AssistantRun{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE assistant_chats SET updated_at=now(),lock_version=lock_version+1,title=CASE WHEN $3=1 AND title='Новое исследование' THEN $2 ELSE title END WHERE assistant_chat_uuid=$1`, in.ChatUUID, chatTitle(in.Text), seq); err != nil {
		return models.AssistantRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.AssistantRun{}, err
	}
	return models.AssistantRun{ID: runID, ChatUUID: in.ChatUUID, State: "queued", ResponseDetail: in.ResponseDetail, RequestReceivedAt: s.now()}, nil
}

func (s *Service) GetRun(ctx context.Context, user, runID uuid.UUID) (models.AssistantRun, error) {
	var run models.AssistantRun
	err := s.db.QueryRowContext(ctx, `SELECT ar.assistant_run_uuid,ar.assistant_chat_uuid,ar.state,ar.response_detail,ar.data_snapshot_at,ar.request_received_at,ar.completed_at,COALESCE(ar.error_code,'') FROM assistant_runs ar JOIN assistant_chats ac USING(assistant_chat_uuid) WHERE ar.assistant_run_uuid=$1 AND ac.owner_user_uuid=$2 AND ac.deleted_at IS NULL`, runID, user).Scan(&run.ID, &run.ChatUUID, &run.State, &run.ResponseDetail, &run.DataSnapshotAt, &run.RequestReceivedAt, &run.CompletedAt, &run.ErrorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AssistantRun{}, ErrNotFound
	}
	return run, err
}

func (s *Service) completeRun(ctx context.Context, run models.AssistantRun, in models.CreateAssistantMessageInput) (models.AssistantRun, error) {
	contextualQuestion, err := s.questionWithHistory(ctx, run.ChatUUID, in.Text)
	if err != nil {
		_ = s.failRun(ctx, run.ID, "history_failed")
		return models.AssistantRun{}, err
	}
	searchInput := models.ContentSearchInput{UserUUID: in.UserUUID, CompanyUUID: in.CompanyUUID, Query: in.Text, SemanticQuery: contextualQuestion, CallIDs: in.CallIDs, DepartmentIDs: in.DepartmentIDs, FolderIDs: in.FolderIDs, From: in.From, To: in.To, Limit: 20}
	var search models.ContentSearchResult
	if requestsFullConversationReview(in.Text) && len(in.CallIDs) > 0 {
		search, err = s.fullSelectedContent(ctx, searchInput)
	} else {
		search, err = s.ContentSearch(ctx, searchInput)
	}
	if err != nil {
		_ = s.failRun(ctx, run.ID, "search_failed")
		return models.AssistantRun{}, err
	}
	now := s.now()
	manifest, _ := json.Marshal(search.Items)
	_, _ = s.db.ExecContext(ctx, `UPDATE assistant_runs SET source_manifest_json=$2,data_snapshot_at=$3,state='generating' WHERE assistant_run_uuid=$1`, run.ID, manifest, now)
	sources := make([]sourcePrompt, 0, len(search.Items))
	sourceMap := map[string]models.ContentSearchItem{}
	for i, item := range search.Items {
		id := fmt.Sprintf("S%d", i+1)
		speaker := ""
		if item.Speaker != nil {
			speaker = *item.Speaker
		}
		sources = append(sources, sourcePrompt{ID: id, CallTitle: item.Title, Text: item.Quote, Speaker: speaker, Revision: item.Revision, StartSeconds: item.StartSeconds, EndSeconds: item.EndSeconds, SourceKind: item.SourceKind})
		sourceMap[id] = item
	}
	answer := generatedAnswer{}
	if len(sources) == 0 {
		switch {
		case search.EvaluatedCalls == 0:
			answer.Text = "В выбранном контексте нет доступных звонков. Проверьте личный или корпоративный режим и фильтры."
		case search.IndexReadyCalls == 0:
			answer.Text = fmt.Sprintf("В выбранном контексте %d звонков, но их поисковый индекс ещё не готов. Содержимое записей не проверено. Повторите поиск после подготовки индекса.", search.EvaluatedCalls)
		default:
			answer.Text = fmt.Sprintf("В проиндексированных звонках совпадений не найдено. Проверено %d из %d доступных звонков. Попробуйте другие слова или измените фильтры.", search.IndexReadyCalls, search.EvaluatedCalls)
		}
	} else if s.generator == nil || !s.generator.Enabled() {
		_ = s.failRun(ctx, run.ID, "assistant_unavailable")
		return models.AssistantRun{}, ErrProviderUnavailable
	} else {
		_, maxTokens := narrativeBudget(in.ResponseDetail, len(sources), utf8.RuneCountInString(in.Text))
		provider, model := s.generator.Profile()
		var creditOperation uuid.UUID
		if s.credits != nil {
			inputTokens := int64((len([]byte(in.Text)) + len(manifest) + 2) / 3)
			creditOperation, err = s.credits.ReserveAssistantGeneration(ctx, in.UserUUID, in.CompanyUUID, s.departmentForUser(ctx, in.CompanyUUID, in.UserUUID), run.ID, inputTokens, int64(maxTokens), provider, model)
			if err != nil {
				_ = s.failRun(ctx, run.ID, "insufficient_credits")
				return models.AssistantRun{}, err
			}
			_, _ = s.db.ExecContext(ctx, `UPDATE assistant_runs SET usage_operation_uuid=$2,provider=$3,model=$4 WHERE assistant_run_uuid=$1`, run.ID, creditOperation, provider, model)
		}
		var usage ProviderUsage
		answer, usage, err = s.generator.Generate(ctx, contextualQuestion, sources, maxTokens)
		if err != nil {
			if s.credits != nil && creditOperation != uuid.Nil {
				_ = s.credits.MarkCreditOperationReconciling(ctx, creditOperation, "assistant provider result unavailable")
			}
			_ = s.failRun(ctx, run.ID, "provider_failed")
			return models.AssistantRun{}, err
		}
		if s.credits != nil && creditOperation != uuid.Nil {
			billingUsage := &models.ProviderUsage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens, CostNanoUSD: usage.CostNanoUSD}
			if err = s.credits.SettleAssistantGeneration(ctx, creditOperation, billingUsage); err != nil {
				_ = s.credits.MarkCreditOperationReconciling(ctx, creditOperation, "assistant usage settlement failed")
				_ = s.failRun(ctx, run.ID, "billing_reconciliation_required")
				return models.AssistantRun{}, err
			}
		}
	}
	answer.Text = stripTechnicalIDs(answer.Text)
	maxRunes, _ := narrativeBudget(in.ResponseDetail, len(sources), utf8.RuneCountInString(in.Text))
	if utf8.RuneCountInString(answer.Text) > maxRunes {
		_ = s.failRun(ctx, run.ID, "answer_too_long")
		return models.AssistantRun{}, errors.New("assistant narrative exceeded response budget")
	}
	validIDs := []string{}
	seen := map[string]bool{}
	for _, id := range answer.CitationIDs {
		if _, ok := sourceMap[id]; ok && !seen[id] {
			validIDs = append(validIDs, id)
			seen[id] = true
		}
	}
	if len(sources) > 0 && len(validIDs) == 0 {
		_ = s.failRun(ctx, run.ID, "citation_validation_failed")
		return models.AssistantRun{}, errors.New("assistant answer has no valid citations")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AssistantRun{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM assistant_messages WHERE assistant_chat_uuid=$1`, run.ChatUUID).Scan(&seq); err != nil {
		return models.AssistantRun{}, err
	}
	msgID, _ := uuid.NewV7()
	msg := models.AssistantMessage{ID: msgID, ChatUUID: run.ChatUUID, Sequence: seq, Role: "assistant", Status: "completed", Text: answer.Text, Blocks: []models.AssistantBlock{{Type: "text", Text: answer.Text}}, CreatedAt: now, CompletedAt: &now}
	raw, _ := json.Marshal(msg)
	if _, err = tx.ExecContext(ctx, `INSERT INTO assistant_messages(assistant_message_uuid,assistant_chat_uuid,sequence,role,content_json,status,completed_at) VALUES($1,$2,$3,'assistant',$4,'completed',$5)`, msgID, run.ChatUUID, seq, raw, now); err != nil {
		return models.AssistantRun{}, err
	}
	msg.Sources = []models.AssistantSource{}
	for i, id := range validIDs {
		item := sourceMap[id]
		cid, _ := uuid.NewV7()
		if _, err = tx.ExecContext(ctx, `INSERT INTO assistant_citations(assistant_citation_uuid,assistant_message_uuid,call_uuid,transcription_revision,chunk_uuid,quote,start_seconds,end_seconds,ordinal,source_kind) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, cid, msgID, item.CallUUID, item.Revision, item.ChunkUUID, item.Quote, item.StartSeconds, item.EndSeconds, i, item.SourceKind); err != nil {
			return models.AssistantRun{}, err
		}
		msg.Sources = append(msg.Sources, models.AssistantSource{ID: cid, CallUUID: item.CallUUID, CallTitle: item.Title, Quote: item.Quote, StartSeconds: item.StartSeconds, EndSeconds: item.EndSeconds, Revision: item.Revision, SourceKind: item.SourceKind})
	}
	if len(search.Items) > 0 && isRetrievalDistributionRequest(in.Text) {
		artifact, artifactErr := storeRetrievalArtifact(ctx, tx, msgID, search)
		if artifactErr != nil {
			return models.AssistantRun{}, artifactErr
		}
		msg.Artifacts = append(msg.Artifacts, artifact)
		msg.Blocks = append(msg.Blocks, models.AssistantBlock{Type: artifact.Type, ArtifactID: &artifact.ID})
		raw, _ = json.Marshal(msg)
		if _, err = tx.ExecContext(ctx, `UPDATE assistant_messages SET content_json=$2 WHERE assistant_message_uuid=$1`, msgID, raw); err != nil {
			return models.AssistantRun{}, err
		}
	}
	if answer.Title != "" {
		var userMessages int
		if countErr := tx.QueryRowContext(ctx, `SELECT count(*) FROM assistant_messages WHERE assistant_chat_uuid=$1 AND role='user'`, run.ChatUUID).Scan(&userMessages); countErr != nil {
			return models.AssistantRun{}, countErr
		}
		if userMessages == 1 {
			_, err = tx.ExecContext(ctx, `UPDATE assistant_chats SET title=$2,updated_at=now() WHERE assistant_chat_uuid=$1`, run.ChatUUID, truncateRunes(answer.Title, 50))
			if err != nil {
				return models.AssistantRun{}, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE assistant_runs SET state='completed',completed_at=$2 WHERE assistant_run_uuid=$1`, run.ID, now); err != nil {
		return models.AssistantRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.AssistantRun{}, err
	}
	run.State = "completed"
	run.DataSnapshotAt = &now
	run.CompletedAt = &now
	run.AssistantMessage = &msg
	return run, nil
}

func (s *Service) questionWithHistory(ctx context.Context, chatID uuid.UUID, current string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role,content_json FROM assistant_messages WHERE assistant_chat_uuid=$1 ORDER BY sequence DESC LIMIT 9`, chatID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	type entry struct{ role, text string }
	entries := make([]entry, 0, 8)
	for rows.Next() {
		var role string
		var raw []byte
		if err = rows.Scan(&role, &raw); err != nil {
			return "", err
		}
		var message models.AssistantMessage
		if json.Unmarshal(raw, &message) != nil || strings.TrimSpace(message.Text) == "" {
			continue
		}
		entries = append(entries, entry{role: role, text: truncateRunes(strings.TrimSpace(message.Text), 2000)})
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(entries) > 0 && entries[0].role == "user" && entries[0].text == current {
		entries = entries[1:]
	}
	if len(entries) == 0 {
		return current, nil
	}
	var history strings.Builder
	for i := len(entries) - 1; i >= 0; i-- {
		label := "Пользователь"
		if entries[i].role == "assistant" {
			label = "Помощник"
		}
		_, _ = fmt.Fprintf(&history, "%s: %s\n", label, entries[i].text)
	}
	return "Предыдущая переписка нужна только для понимания текущего вопроса; утверждения помощника не являются доказательствами.\n" + history.String() + "\nТекущий вопрос: " + current, nil
}

func (s *Service) failRun(ctx context.Context, id uuid.UUID, code string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE assistant_runs SET state='failed',error_code=$2,completed_at=now() WHERE assistant_run_uuid=$1 AND state NOT IN ('completed','failed','cancelled','access_revoked')`, id, code)
	return err
}
func (s *Service) requireChat(ctx context.Context, user, company uuid.UUID) (models.AssistantCapabilities, error) {
	c, err := s.Capabilities(ctx, user, company)
	if err != nil {
		return c, err
	}
	if !c.ChatEnabled {
		return c, ErrForbidden
	}
	return c, nil
}
func (s *Service) ownedChat(ctx context.Context, user, chat uuid.UUID) (uuid.UUID, string, error) {
	var company uuid.UUID
	var detail string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(company_uuid,'00000000-0000-0000-0000-000000000000'::uuid),response_detail FROM assistant_chats WHERE assistant_chat_uuid=$1 AND owner_user_uuid=$2 AND deleted_at IS NULL`, chat, user).Scan(&company, &detail)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", ErrNotFound
	}
	return company, detail, err
}
func (s *Service) loadSources(ctx context.Context, msg *models.AssistantMessage) error {
	rows, err := s.db.QueryContext(ctx, `SELECT ac.assistant_citation_uuid,ac.call_uuid,c.title,ac.quote,ac.start_seconds,ac.end_seconds,ac.transcription_revision,ac.source_kind FROM assistant_citations ac JOIN calls c ON c.call_uuid=ac.call_uuid WHERE ac.assistant_message_uuid=$1 ORDER BY ac.ordinal`, msg.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	msg.Sources = []models.AssistantSource{}
	for rows.Next() {
		var x models.AssistantSource
		if err = rows.Scan(&x.ID, &x.CallUUID, &x.CallTitle, &x.Quote, &x.StartSeconds, &x.EndSeconds, &x.Revision, &x.SourceKind); err != nil {
			return err
		}
		msg.Sources = append(msg.Sources, x)
	}
	return rows.Err()
}

func (s *Service) loadArtifacts(ctx context.Context, msg *models.AssistantMessage) error {
	rows, err := s.db.QueryContext(ctx, `SELECT assistant_artifact_uuid,assistant_message_uuid,artifact_type,title,schema_version,data_json,created_at FROM assistant_artifacts WHERE assistant_message_uuid=$1 ORDER BY created_at,assistant_artifact_uuid`, msg.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	msg.Artifacts = []models.AssistantArtifact{}
	for rows.Next() {
		var item models.AssistantArtifact
		if err = rows.Scan(&item.ID, &item.MessageUUID, &item.Type, &item.Title, &item.SchemaVersion, &item.Data, &item.CreatedAt); err != nil {
			return err
		}
		msg.Artifacts = append(msg.Artifacts, item)
	}
	return rows.Err()
}

func storeRetrievalArtifact(ctx context.Context, tx *sql.Tx, messageID uuid.UUID, search models.ContentSearchResult) (models.AssistantArtifact, error) {
	type row struct {
		CallUUID   string  `json:"call_uuid"`
		Label      string  `json:"label"`
		Count      int     `json:"count"`
		Percentage float64 `json:"percentage"`
	}
	counts := map[uuid.UUID]*row{}
	order := []uuid.UUID{}
	for _, item := range search.Items {
		entry := counts[item.CallUUID]
		if entry == nil {
			entry = &row{CallUUID: item.CallUUID.String(), Label: truncateRunes(item.Title, 120)}
			counts[item.CallUUID] = entry
			order = append(order, item.CallUUID)
		}
		entry.Count++
	}
	rows := make([]row, 0, len(order))
	for _, id := range order {
		entry := *counts[id]
		entry.Percentage = float64(entry.Count) * 100 / float64(len(search.Items))
		rows = append(rows, entry)
	}
	data, _ := json.Marshal(map[string]any{
		"kind": "bar", "basis": "Найденные фрагменты", "total": len(search.Items),
		"evaluated_calls": search.EvaluatedCalls, "indexed_calls": search.IndexReadyCalls, "items": rows,
	})
	id, _ := uuid.NewV7()
	artifact := models.AssistantArtifact{ID: id, MessageUUID: messageID, Type: "chart", Title: "Распределение найденных фрагментов по звонкам", SchemaVersion: 1, Data: data, CreatedAt: time.Now().UTC()}
	_, err := tx.ExecContext(ctx, `INSERT INTO assistant_artifacts(assistant_artifact_uuid,assistant_message_uuid,artifact_type,title,schema_version,data_json,created_at) VALUES($1,$2,'chart',$3,1,$4,$5)`, artifact.ID, artifact.MessageUUID, artifact.Title, artifact.Data, artifact.CreatedAt)
	return artifact, err
}
func validDetail(v string) bool { return v == "auto" || v == "brief" || v == "detailed" }

// Provider-facing source labels are deliberately short and opaque, but a
// defensive pass also removes UUID-shaped values if a model echoes metadata.
func stripTechnicalIDs(value string) string {
	return strings.TrimSpace(technicalUUIDPattern.ReplaceAllString(value, ""))
}

func isRetrievalDistributionRequest(value string) bool {
	v := strings.ToLower(value)
	return strings.Contains(v, "распределение найденных фрагментов") || strings.Contains(v, "сколько найденных фрагментов")
}
func truncateRunes(v string, n int) string {
	r := []rune(v)
	if len(r) <= n {
		return v
	}
	return string(r[:n])
}
func vectorLiteral(values []float32) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%.9g", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Hash every semantic input. Set-like filters and UTC timestamps normalize retries.
func messageRequestHash(in models.CreateAssistantMessageInput) [32]byte {
	in.IdempotencyKey = ""
	in.Text = strings.TrimSpace(in.Text)
	in.CallIDs = canonicalIDs(in.CallIDs)
	in.DepartmentIDs = canonicalIDs(in.DepartmentIDs)
	in.FolderIDs = canonicalIDs(in.FolderIDs)
	if in.From != nil {
		t := in.From.UTC()
		in.From = &t
	}
	if in.To != nil {
		t := in.To.UTC()
		in.To = &t
	}
	raw, _ := json.Marshal(in)
	return sha256.Sum256(raw)
}
func canonicalIDs(ids []uuid.UUID) []uuid.UUID {
	result := append([]uuid.UUID{}, ids...)
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	out := result[:0]
	for _, id := range result {
		if len(out) == 0 || out[len(out)-1] != id {
			out = append(out, id)
		}
	}
	return out
}

// Validate the full retrieval manifest, not only displayed citations: uncited
// source text may still have influenced generated statements.
func (s *Service) messageSourcesAvailable(ctx context.Context, user, company uuid.UUID, msg models.AssistantMessage) (bool, error) {
	var available bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assistant_runs ar
 JOIN assistant_messages um ON um.assistant_message_uuid=ar.user_message_uuid
 WHERE ar.assistant_chat_uuid=$3 AND um.sequence=$4-1 AND ar.state='completed'
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(ar.source_manifest_json) item
 WHERE NOT EXISTS(SELECT 1 FROM calls c
 JOIN call_transcriptions t ON t.call_uuid=c.call_uuid
 JOIN call_transcription_revision_state rs ON rs.transcription_uuid=t.transcription_uuid
 WHERE c.call_uuid=(item->>'call_uuid')::uuid AND `+scopeSQL()+`
 AND rs.active_revision=(item->>'transcription_revision')::int
 AND (`+callAccessSQL()+`)
 AND NOT EXISTS(SELECT 1 FROM call_privacy_states ps WHERE ps.call_uuid=c.call_uuid
 AND (ps.status NOT IN ('not_requested','ready') OR ps.updated_at>ar.data_snapshot_at)))))`, user, company, msg.ChatUUID, msg.Sequence).Scan(&available)
	return available, err
}

// This budget applies only to narrative; artifact rows never enter the formula.
func narrativeBudget(detail string, sources, questionRunes int) (int, int) {
	if detail == "brief" {
		return 1600, 900
	}
	if detail == "detailed" {
		return 12000, 6000
	}
	sources = max(0, min(sources, 50))
	questionRunes = max(0, min(questionRunes, 12000))
	characters := min(9000, 2000+sources*180+min(questionRunes, 2000))
	return characters, (characters+1)/2 + 200
}

func currentDocumentSQL() string {
	return `EXISTS(SELECT 1 FROM call_transcription_revision_state rs
 JOIN call_transcription_revisions r ON r.transcription_uuid=rs.transcription_uuid AND r.revision=rs.active_revision
 JOIN call_transcription_contents ct ON ct.transcription_content_uuid=r.transcription_content_uuid
 WHERE rs.transcription_uuid=d.transcription_uuid AND rs.active_revision=d.transcription_revision AND ct.content_sha256=d.content_sha256)
 AND EXISTS(SELECT 1 FROM search_index_profiles ip WHERE ip.search_index_profile_uuid=d.profile_uuid AND ip.status='active')
 AND NOT EXISTS(SELECT 1 FROM call_privacy_states ps WHERE ps.call_uuid=d.call_uuid
 AND (ps.status NOT IN ('not_requested','ready') OR ps.updated_at>d.updated_at))`
}

func chatTitle(text string) string {
	title := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(title) <= 72 {
		return title
	}
	runes := []rune(title)
	cut := 72
	for i := 72; i >= 40; i-- {
		if runes[i] == ' ' {
			cut = i
			break
		}
	}
	return string(runes[:cut]) + "…"
}
func (s *Service) DeleteChat(ctx context.Context, user, chat uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT assistant_chat_uuid FROM assistant_chats WHERE assistant_chat_uuid=$1 AND owner_user_uuid=$2 FOR UPDATE`, chat, user).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assistant_runs WHERE assistant_chat_uuid=$1 AND state IN ('queued','preparing','retrieving','generating','validating'))`, chat).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return ErrRunInProgress
	}
	_, err = tx.ExecContext(ctx, `UPDATE assistant_chats SET deleted_at=COALESCE(deleted_at,now()),updated_at=now() WHERE assistant_chat_uuid=$1`, chat)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func isSearchRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, prefix := range []string{"найди", "найти", "покажи звонки", "покажи записи", "ищу "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
func lexicalQuery(text string) string {
	if !isSearchRequest(text) {
		return text
	}
	skip := map[string]bool{"найди": true, "найти": true, "мне": true, "пожалуйста": true, "звонки": true, "записи": true, "звонок": true, "покажи": true, "ищу": true, "с": true, "на": true, "по": true, "о": true, "про": true}
	terms := []string{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = strings.Trim(word, ".,!?;:")
		if skip[word] {
			continue
		}
		if word == "го" || word == "golang" {
			word = "go"
		}
		terms = append(terms, word)
	}
	if len(terms) == 0 {
		return text
	}
	return strings.Join(terms, " ")
}
