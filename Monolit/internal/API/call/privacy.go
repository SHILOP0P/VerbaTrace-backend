package call

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
	privacyservice "verbatrace/monolit/internal/service/privacy"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type privacyPolicyMutation struct {
	SchemaVersion          int                        `json:"schema_version"`
	Config                 models.PrivacyPolicyConfig `json:"config"`
	PreviewHash            string                     `json:"preview_hash"`
	Reason                 string                     `json:"reason"`
	HighImpactAcknowledged bool                       `json:"high_impact_acknowledged"`
}

var privacyLabels = map[string]string{
	"person_name": "Имя", "phone_number": "Телефон", "email_address": "Электронная почта", "address": "Адрес",
	"date_of_birth": "Дата рождения", "passport_number": "Номер паспорта", "drivers_license": "Водительское удостоверение",
	"account_number": "Номер счёта", "banking_information": "Банковские данные", "credit_card_number": "Номер карты",
	"credit_card_cvv": "Код карты", "credit_card_expiration": "Срок действия карты", "password": "Секрет",
	"ip_address": "Сетевой адрес", "username": "Имя пользователя", "medical_condition": "Медицинские данные",
	"money_amount": "Денежная сумма", "organization": "Организация",
}

func privacyEntityLabel(entity string) string {
	if label := privacyLabels[entity]; label != "" {
		return label
	}
	return "Скрытые данные"
}
func (h *CallHandler) GetPersonalPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	h.getPrivacyPolicy(w, r, models.PrivacyScopePersonal, uuid.Nil)
}
func (h *CallHandler) GetCompanyPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCompanyID(w, r)
	if ok {
		h.getPrivacyPolicy(w, r, models.PrivacyScopeCompany, id)
	}
}
func (h *CallHandler) GetDepartmentPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.privacyDepartmentIDs(w, r)
	if ok {
		h.getPrivacyPolicy(w, r, models.PrivacyScopeDepartment, id)
	}
}
func (h *CallHandler) SavePersonalPrivacyDraft(w http.ResponseWriter, r *http.Request) {
	h.savePrivacyDraft(w, r, models.PrivacyScopePersonal, uuid.Nil)
}
func (h *CallHandler) SaveCompanyPrivacyDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCompanyID(w, r)
	if ok {
		h.savePrivacyDraft(w, r, models.PrivacyScopeCompany, id)
	}
}
func (h *CallHandler) SaveDepartmentPrivacyDraft(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.privacyDepartmentIDs(w, r)
	if ok {
		h.savePrivacyDraft(w, r, models.PrivacyScopeDepartment, id)
	}
}
func (h *CallHandler) PreviewPersonalPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	h.previewPrivacyPolicy(w, r, models.PrivacyScopePersonal, uuid.Nil)
}
func (h *CallHandler) PreviewCompanyPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCompanyID(w, r)
	if ok {
		h.previewPrivacyPolicy(w, r, models.PrivacyScopeCompany, id)
	}
}
func (h *CallHandler) PreviewDepartmentPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.privacyDepartmentIDs(w, r)
	if ok {
		h.previewPrivacyPolicy(w, r, models.PrivacyScopeDepartment, id)
	}
}
func (h *CallHandler) PublishPersonalPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	h.publishPrivacyPolicy(w, r, models.PrivacyScopePersonal, uuid.Nil)
}
func (h *CallHandler) PublishCompanyPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCompanyID(w, r)
	if ok {
		h.publishPrivacyPolicy(w, r, models.PrivacyScopeCompany, id)
	}
}
func (h *CallHandler) PublishDepartmentPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.privacyDepartmentIDs(w, r)
	if ok {
		h.publishPrivacyPolicy(w, r, models.PrivacyScopeDepartment, id)
	}
}
func (h *CallHandler) ListCompanyPrivacyPolicyVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCompanyID(w, r)
	if !ok {
		return
	}
	scopeID, actor, ok := h.privacyScope(r, models.PrivacyScopeCompany, id)
	if !ok {
		return
	}
	items, err := h.privacy.ListVersions(r.Context(), models.PrivacyScopeCompany, scopeID, actor)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, privacyVersionView(item))
	}
	_ = response.WriteJSON(w, 200, map[string]any{"items": result})
}
func (h *CallHandler) GetCompanyPrivacyPolicyVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCompanyID(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || version < 1 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректная версия")
		return
	}
	scopeID, actor, ok := h.privacyScope(r, models.PrivacyScopeCompany, id)
	if !ok {
		return
	}
	item, err := h.privacy.GetVersionNumber(r.Context(), models.PrivacyScopeCompany, scopeID, actor, version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.WriteError(w, 404, "privacy_policy_version_not_found", "Версия политики не найдена")
			return
		}
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, privacyVersionView(item))
}

func (h *CallHandler) ListDepartmentPrivacyPolicyVersions(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.privacyDepartmentIDs(w, r)
	if !ok {
		return
	}
	scopeID, actor, ok := h.privacyScope(r, models.PrivacyScopeDepartment, id)
	if !ok {
		return
	}
	items, err := h.privacy.ListVersions(r.Context(), models.PrivacyScopeDepartment, scopeID, actor)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, privacyVersionView(item))
	}
	_ = response.WriteJSON(w, 200, map[string]any{"items": result})
}

func (h *CallHandler) GetDepartmentPrivacyPolicyVersion(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.privacyDepartmentIDs(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || version < 1 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректная версия")
		return
	}
	scopeID, actor, ok := h.privacyScope(r, models.PrivacyScopeDepartment, id)
	if !ok {
		return
	}
	item, err := h.privacy.GetVersionNumber(r.Context(), models.PrivacyScopeDepartment, scopeID, actor, version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.WriteError(w, 404, "privacy_policy_version_not_found", "Версия политики не найдена")
			return
		}
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, privacyVersionView(item))
}

func privacyCompanyID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "company_uuid"))
	if err != nil {
		response.WriteError(w, 400, "privacy_policy_invalid", "Некорректная компания")
		return uuid.Nil, false
	}
	return id, true
}
func (h *CallHandler) privacyDepartmentIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	companyID, ok := privacyCompanyID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, 400, "privacy_policy_invalid", "Некорректный отдел")
		return uuid.Nil, uuid.Nil, false
	}
	belongs, err := h.privacy.DepartmentBelongsToCompany(r.Context(), companyID, departmentID)
	if err != nil {
		writePrivacyError(w, err)
		return uuid.Nil, uuid.Nil, false
	}
	if !belongs {
		response.WriteError(w, 422, "privacy_policy_invalid", "Отдел не принадлежит компании")
		return uuid.Nil, uuid.Nil, false
	}
	return companyID, departmentID, true
}
func (h *CallHandler) privacyScope(r *http.Request, scope models.PrivacyScopeType, id uuid.UUID) (uuid.UUID, uuid.UUID, bool) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	if scope == models.PrivacyScopePersonal {
		id = actor
	}
	return id, actor, true
}

func (h *CallHandler) getPrivacyPolicy(w http.ResponseWriter, r *http.Request, scope models.PrivacyScopeType, scopeID uuid.UUID) {
	if h.privacy == nil {
		response.WriteError(w, 501, "privacy_not_configured", "Защита данных не настроена")
		return
	}
	scopeID, actor, ok := h.privacyScope(r, scope, scopeID)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "Требуется авторизация")
		return
	}
	view, err := h.privacy.GetPolicy(r.Context(), scope, scopeID, actor)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, privacyPolicyView(view))
}

func (h *CallHandler) savePrivacyDraft(w http.ResponseWriter, r *http.Request, scope models.PrivacyScopeType, scopeID uuid.UUID) {
	scopeID, actor, ok := h.privacyScope(r, scope, scopeID)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "Требуется авторизация")
		return
	}
	var request privacyPolicyMutation
	if err := decodeStrictJSON(r, &request); err != nil || request.SchemaVersion != 1 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректные настройки защиты данных")
		return
	}
	expected, valid := expectedLockVersion(r)
	if !valid {
		response.WriteError(w, 409, "privacy_policy_revision_conflict", "Настройки были изменены")
		return
	}
	draft, err := h.privacy.SaveDraft(r.Context(), scope, scopeID, actor, expected, request.Config)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", draft.LockVersion))
	_ = response.WriteJSON(w, 200, map[string]any{"draft": privacyDraftView(draft)})
}

func (h *CallHandler) previewPrivacyPolicy(w http.ResponseWriter, r *http.Request, scope models.PrivacyScopeType, scopeID uuid.UUID) {
	scopeID, actor, ok := h.privacyScope(r, scope, scopeID)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "Требуется авторизация")
		return
	}
	var request privacyPolicyMutation
	if err := decodeStrictJSON(r, &request); err != nil || request.SchemaVersion != 1 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректные настройки защиты данных")
		return
	}
	config, hash, err := h.privacy.Preview(r.Context(), scope, scopeID, actor, request.Config)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	warnings := []map[string]string{}
	for _, entity := range config.EntityTypes {
		if entity == "money_amount" {
			warnings = append(warnings, map[string]string{"code": "money_analysis_limited", "title": "Денежные суммы будут скрыты", "message": "Анализ не сможет проверять точные цены и лимиты."})
		}
	}
	sampleAfter := "Меня зовут Анна, телефон +7 900 000-00-00, стоимость 150 000 рублей."
	if config.Enabled {
		for _, entity := range config.EntityTypes {
			switch entity {
			case "person_name":
				sampleAfter = strings.ReplaceAll(sampleAfter, "Анна", models.PrivacyMarkersRUv1[entity])
			case "phone_number":
				sampleAfter = strings.ReplaceAll(sampleAfter, "+7 900 000-00-00", models.PrivacyMarkersRUv1[entity])
			case "money_amount":
				sampleAfter = strings.ReplaceAll(sampleAfter, "150 000 рублей", models.PrivacyMarkersRUv1[entity])
			}
		}
	}
	_ = response.WriteJSON(w, 200, map[string]any{"preview_hash": hash, "effective_config": config, "warnings": warnings, "sample": map[string]string{"before": "Меня зовут Анна, телефон +7 900 000-00-00, стоимость 150 000 рублей.", "after": sampleAfter}})
}

func (h *CallHandler) publishPrivacyPolicy(w http.ResponseWriter, r *http.Request, scope models.PrivacyScopeType, scopeID uuid.UUID) {
	scopeID, actor, ok := h.privacyScope(r, scope, scopeID)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "Требуется авторизация")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) < 16 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Требуется Idempotency-Key")
		return
	}
	var request privacyPolicyMutation
	if err := decodeStrictJSON(r, &request); err != nil || request.SchemaVersion != 1 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректный запрос публикации")
		return
	}
	if contains(request.Config.EntityTypes, "money_amount") && !request.HighImpactAcknowledged {
		response.WriteError(w, 422, "privacy_policy_invalid", "Подтвердите ограничение анализа денежных сумм")
		return
	}
	operation := "publish_privacy_policy:" + string(scope) + ":" + scopeID.String()
	replay, err := h.privacy.BeginIdempotentMutation(r.Context(), actor, operation, idempotencyKey, request)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	if replay != nil {
		_ = response.WriteJSON(w, replay.Status, replay.Body)
		return
	}
	version, err := h.privacy.Publish(r.Context(), scope, scopeID, actor, request.PreviewHash, request.Reason, request.HighImpactAcknowledged)
	if err != nil {
		h.privacy.FailIdempotentMutation(r.Context(), actor, operation, idempotencyKey)
		writePrivacyError(w, err)
		return
	}
	result := map[string]any{"active_version": privacyVersionView(version)}
	if err = h.privacy.CompleteIdempotentMutation(r.Context(), actor, operation, idempotencyKey, 201, result); err != nil {
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 201, result)
}

func privacyPolicyView(view models.PrivacyPolicyView) map[string]any {
	catalog := make([]map[string]any, 0, len(privacyLabels))
	for entity, label := range privacyLabels {
		catalog = append(catalog, map[string]any{"entity_type": entity, "label": label, "marker": models.PrivacyMarkersRUv1[entity], "default_enabled": contains(models.DefaultPrivacyEntityTypes, entity)})
	}
	sortCatalog(catalog)
	result := map[string]any{"scope_type": view.ScopeType, "scope_uuid": view.ScopeID, "can_manage": view.CanManage, "marker_contract": view.MarkerContract, "catalog": catalog}
	if view.ActiveVersion != nil {
		result["active_version"] = privacyVersionView(*view.ActiveVersion)
	} else {
		result["active_version"] = nil
	}
	if view.Draft != nil {
		result["draft"] = privacyDraftView(*view.Draft)
	} else {
		result["draft"] = nil
	}
	if view.InheritedVersion != nil {
		result["inherited_from"] = map[string]any{
			"scope_type":     view.InheritedScopeType,
			"scope_uuid":     view.InheritedScopeID,
			"active_version": privacyVersionView(*view.InheritedVersion),
		}
	} else {
		result["inherited_from"] = nil
	}
	return result
}
func privacyVersionView(value models.PrivacyPolicyVersion) map[string]any {
	return map[string]any{"id": value.ID, "version": value.Version, "config": value.Config, "publish_reason": value.PublishReason, "published_by_user_uuid": value.PublishedBy, "published_at": value.PublishedAt}
}
func privacyDraftView(value models.PrivacyPolicyDraft) map[string]any {
	return map[string]any{"config": value.Config, "lock_version": value.LockVersion, "updated_by_user_uuid": value.UpdatedBy, "updated_at": value.UpdatedAt}
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func sortCatalog(values []map[string]any) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j]["label"].(string) < values[i]["label"].(string) {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
func expectedLockVersion(r *http.Request) (*int64, bool) {
	if r.Header.Get("If-None-Match") == "*" {
		return nil, true
	}
	raw := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\"")
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return nil, false
	}
	return &value, true
}
func decodeStrictJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple json values")
	}
	return nil
}

func writePrivacyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, privacyservice.ErrPolicyForbidden), errors.Is(err, privacyservice.ErrOriginalMediaForbidden):
		response.WriteError(w, 403, "privacy_policy_forbidden", "Недостаточно прав")
	case errors.Is(err, privacyservice.ErrPolicyConflict):
		response.WriteError(w, 409, "privacy_policy_revision_conflict", "Настройки были изменены")
	case errors.Is(err, privacyservice.ErrPreviewStale):
		response.WriteError(w, 409, "privacy_preview_stale", "Предпросмотр устарел")
	case errors.Is(err, privacyservice.ErrPolicyInvalid):
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректные настройки защиты данных")
	case errors.Is(err, privacyservice.ErrCorrectionConflict):
		response.WriteError(w, 409, "privacy_policy_revision_conflict", "Транскрипция была изменена")
	case errors.Is(err, privacyservice.ErrCorrectionInvalid):
		response.WriteError(w, 422, "privacy_correction_invalid", "Некорректное исправление маски")
	case errors.Is(err, privacyservice.ErrIdempotencyKeyReused):
		response.WriteError(w, 409, "idempotency_key_reused", "Ключ повторно использован для другого запроса")
	case errors.Is(err, privacyservice.ErrIdempotencyInProgress):
		response.WriteError(w, 409, "idempotency_operation_in_progress", "Операция с этим ключом уже выполняется")
	case errors.Is(err, privacyservice.ErrRedactedMediaNotReady):
		response.WriteError(w, 409, "redacted_media_not_ready", "Очищенная запись ещё не готова")
	case errors.Is(err, privacyservice.ErrMediaVariantNotFound):
		response.WriteError(w, 404, "redacted_media_not_found", "Очищенная запись не запрошена")
	default:
		response.WriteError(w, 500, "privacy_internal_error", "Не удалось выполнить операцию защиты данных")
	}
}

func (h *CallHandler) GetCallPrivacy(w http.ResponseWriter, r *http.Request) {
	call, actor, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	callResponse, err := converter.CallModelToAPI(call)
	if err != nil {
		response.WriteError(w, 500, "privacy_internal_error", "Не удалось получить состояние защиты")
		return
	}
	h.enrichCallPrivacy(r, call, actor, &callResponse)
	if callResponse.Privacy == nil {
		response.WriteError(w, 500, "privacy_internal_error", "Не удалось получить состояние защиты")
		return
	}
	_ = response.WriteJSON(w, 200, callResponse.Privacy)
}

type privacyCorrectionRequest struct {
	SchemaVersion    int        `json:"schema_version"`
	Operation        string     `json:"operation"`
	ExpectedRevision int        `json:"expected_revision"`
	Reason           string     `json:"reason"`
	WordStartIndex   *int       `json:"word_start_index"`
	WordEndIndex     *int       `json:"word_end_index"`
	SpanUUID         *uuid.UUID `json:"span_uuid"`
	EntityType       string     `json:"entity_type"`
	ReplacementText  string     `json:"replacement_text"`
	RemoveConfirmed  bool       `json:"remove_confirmed"`
}

func (request privacyCorrectionRequest) serviceInput() privacyservice.CorrectionInput {
	return privacyservice.CorrectionInput{Operation: request.Operation, ExpectedRevision: request.ExpectedRevision, Reason: request.Reason, WordStartIndex: request.WordStartIndex, WordEndIndex: request.WordEndIndex, SpanID: request.SpanUUID, EntityType: request.EntityType, ReplacementText: request.ReplacementText, RemoveConfirmed: request.RemoveConfirmed}
}

func (h *CallHandler) PreviewPrivacyCorrection(w http.ResponseWriter, r *http.Request) {
	call, actor, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	var request privacyCorrectionRequest
	if decodeStrictJSON(r, &request) != nil || request.SchemaVersion != 1 {
		response.WriteError(w, 422, "privacy_correction_invalid", "Некорректное исправление маски")
		return
	}
	preview, err := h.privacy.PreviewCorrection(r.Context(), call, actor, request.serviceInput())
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, preview)
}

func (h *CallHandler) ApplyPrivacyCorrection(w http.ResponseWriter, r *http.Request) {
	call, actor, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) < 16 {
		response.WriteError(w, 422, "privacy_correction_invalid", "Требуется Idempotency-Key")
		return
	}
	var request privacyCorrectionRequest
	if decodeStrictJSON(r, &request) != nil || request.SchemaVersion != 1 {
		response.WriteError(w, 422, "privacy_correction_invalid", "Некорректное исправление маски")
		return
	}
	operation := "privacy_correction:" + call.ID.String()
	replay, err := h.privacy.BeginIdempotentMutation(r.Context(), actor, operation, idempotencyKey, request)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	if replay != nil {
		_ = response.WriteJSON(w, replay.Status, replay.Body)
		return
	}
	revision, err := h.privacy.ApplyCorrection(r.Context(), call, actor, request.serviceInput())
	if err != nil {
		h.privacy.FailIdempotentMutation(r.Context(), actor, operation, idempotencyKey)
		writePrivacyError(w, err)
		return
	}
	result := map[string]any{"revision": revision, "reanalysis_required": true}
	if err = h.privacy.CompleteIdempotentMutation(r.Context(), actor, operation, idempotencyKey, 200, result); err != nil {
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, result)
}

func (h *CallHandler) ListPrivacyAudit(w http.ResponseWriter, r *http.Request) {
	call, actor, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	capabilities, err := h.privacy.Capabilities(r.Context(), call, actor)
	if err != nil || !capabilities.CanReviewRedactions {
		response.WriteError(w, 403, "privacy_policy_forbidden", "Недостаточно прав")
		return
	}
	items, err := h.privacy.ListAudit(r.Context(), call.ID, 100)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"items": items})
}
func (h *CallHandler) RequestRedactedMedia(w http.ResponseWriter, r *http.Request) {
	call, actor, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	if len(strings.TrimSpace(r.Header.Get("Idempotency-Key"))) < 16 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Требуется Idempotency-Key")
		return
	}
	media, err := h.privacy.RequestSanitizedMedia(r.Context(), call, actor)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	status := 202
	if media.Status == "ready" {
		status = 200
	}
	_ = response.WriteJSON(w, status, mediaVariantView(media))
}
func (h *CallHandler) GetRedactedMedia(w http.ResponseWriter, r *http.Request) {
	_, _, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	media, err := h.privacy.GetSanitizedMedia(r.Context(), mustCallID(r))
	if err != nil {
		if errors.Is(err, privacyservice.ErrMediaVariantNotFound) {
			_ = response.WriteJSON(w, 200, map[string]any{"status": "not_requested", "variant": "redacted"})
			return
		}
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, mediaVariantView(media))
}
func mediaVariantView(media models.MediaVariant) map[string]any {
	return map[string]any{"id": media.ID, "status": media.Status, "variant": media.Variant, "mime_type": media.MIMEType, "size_bytes": media.SizeBytes, "file_name": media.FileName, "video_stream_copied": media.VideoStreamCopied, "last_error_code": media.LastErrorCode, "updated_at": media.UpdatedAt}
}
func (h *CallHandler) CreateMediaAccessSession(w http.ResponseWriter, r *http.Request) {
	call, actor, ok := h.authorizedPrivacyCall(w, r)
	if !ok {
		return
	}
	var request struct {
		SchemaVersion int    `json:"schema_version"`
		Variant       string `json:"variant"`
	}
	if err := decodeStrictJSON(r, &request); err != nil || request.SchemaVersion != 1 {
		response.WriteError(w, 422, "privacy_policy_invalid", "Некорректный вариант записи")
		return
	}
	id, expires, err := h.privacy.CreateMediaAccessSession(r.Context(), call, actor, request.Variant)
	if err != nil {
		writePrivacyError(w, err)
		return
	}
	_ = response.WriteJSON(w, 201, map[string]any{"media_access_session_uuid": id, "variant": request.Variant, "expires_at": expires})
}
func (h *CallHandler) authorizedPrivacyCall(w http.ResponseWriter, r *http.Request) (models.Call, uuid.UUID, bool) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "Требуется авторизация")
		return models.Call{}, uuid.Nil, false
	}
	id := mustCallID(r)
	if id == uuid.Nil {
		response.WriteError(w, 400, response.CodeInvalidCallUUID, "Некорректный звонок")
		return models.Call{}, uuid.Nil, false
	}
	call, err := h.service.GetByUUID(r.Context(), id, actor)
	if err != nil {
		response.WriteError(w, 404, response.CodeCallNotFound, "Звонок не найден")
		return models.Call{}, uuid.Nil, false
	}
	return call, actor, true
}
func mustCallID(r *http.Request) uuid.UUID { id, _ := uuid.Parse(chi.URLParam(r, "uuid")); return id }
