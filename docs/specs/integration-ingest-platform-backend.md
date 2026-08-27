# Backend-спецификация: Integration Ingest Platform

Статус: production design, код не реализован
Дата: 2026-08-22
Родитель: [integration-ingest-platform.md](./integration-ingest-platform.md)
Billing dependency:
[credit-billing-developer-platform-rollout.md](./credit-billing-developer-platform-rollout.md)

## 1. Архитектурное решение

Создать bounded context `integration`, не расширять HTTP handler ручной загрузки
vendor-условиями. Слои:

```text
internal/API/integration
internal/service/integration
internal/repository/integration
internal/integrationauth
internal/integrationcrypto
internal/mediafetch
internal/connector/{generic,bitrix24}
```

`CallService.CreateCall` остаётся единственным продуктовым путём создания звонка.
Для worker ему передаётся явный integration service principal, зафиксированный
placement и media reader. Если атомарность требует нового метода, он добавляется в
call domain, а не дублируется raw SQL в integration service.

`created_by_user_uuid` — только историческая атрибуция, не runtime principal:
увольнение/блокировка создателя не должно останавливать connection или давать ему
доступ после увольнения. Call domain получает явный service principal с company,
connection, scopes и delegated placement; он проходит отдельный authorization path
и не impersonate-ит user. В audit сохраняются и service actor, и original creator.

PostgreSQL остаётся очередью первой версии: текущие `SKIP LOCKED`/lease/retry
паттерны переиспользуются. Бизнес-состояние ingest хранится отдельно от
`processing_jobs`. Новый broker вводится только по результатам capacity/load test.

## 2. Модель данных

Все UUID генерируются сервером. Все mutable rows имеют `lock_version bigint`.

### 2.1. `integration_connections`

```text
connection_uuid PK
application_uuid FK developer_applications ON DELETE RESTRICT
company_uuid FK companies ON DELETE RESTRICT
department_uuid nullable composite-valid for company, ON DELETE SET NULL
folder_uuid nullable FK call_folders ON DELETE SET NULL
created_by_user_uuid FK users ON DELETE RESTRICT
name varchar(120)
provider generic_api|bitrix24|amocrm|telephony
status draft|active|degraded|disabled|revoked
skip_custom_instructions bool
selected_instruction_uuids uuid[] or normalized join table
settings_version integer
settings jsonb, provider-specific allowlisted schema
last_event_at/last_success_at/last_health_at nullable
last_error_code nullable
lock_version, created_at, updated_at, revoked_at
```

Предпочтительна join table для instructions со snapshot применения через обычный
analysis contract. JSON settings не содержит plaintext secrets.

Физический cascade от company к connection запрещён: он противоречит требованиям
сохранить provenance, usage и audit. Удаление company выполняется отдельным
идемпотентным erasure workflow: revoke/остановка, inventory зависимых данных,
legal/retention decision и контролируемая анонимизация или удаление. `settings`
хранит `schema_version`; принятый item закрепляет `connection_settings_version` и
неизменяемый placement/instruction snapshot.

Application и company должны иметь одного owner: для company application owner
равен этой company; personal application не может создавать company connection в
первой версии. Environment connection наследуется от application и отдельно не
переопределяется.

### 2.2. `developer_applications`

Application — корневая identity developer platform и владелец ключей, connections,
webhooks и usage. Она создаётся до service accounts и sandbox API.

```text
application_uuid PK
owner_type user|company
owner_uuid
billing_account_uuid FK billing_accounts ON DELETE RESTRICT
name varchar(120)
environment sandbox|production
status active|disabled|revoked
capabilities text[]
daily_credit_limit bigint CHECK >= 0
monthly_credit_limit bigint CHECK >= 0
max_credits_per_operation bigint CHECK >= 0
lock_version bigint
created_at, updated_at, revoked_at
```

Owner polymorphism обеспечивается не двумя nullable FK без проверки, а owner
registry/composite constraint либо отдельными типизированными связями. Billing
account должен принадлежать тому же owner. Изменение owner/environment запрещено;
для другого owner/environment создаётся новая application. Budget уменьшение не
отменяет уже зарезервированные операции, но блокирует новые.

Budget consumption считает `reserved + settled` атомарно с reserve. Daily window —
календарные сутки UTC, monthly — календарный месяц UTC. `NULL` означает server
policy default; unlimited разрешается только явной capability, а не магическим
нулём или отсутствующим значением.

Связи:

```text
developer_application
  -> integration_service_accounts
  -> integration_api_keys (через service account)
  -> integration_connections
  -> integration_webhook_endpoints
  -> usage_operations
```

Каждый authenticated request проверяет:

```text
application.environment = request environment
api_key/service_account.application_uuid = application_uuid
connection.application_uuid = application_uuid
billing_account owner = application owner
usage_operation.application_uuid = application_uuid
```

`vt_test_*` разрешён только sandbox endpoint, `vt_live_*` — только production.
Mismatch возвращает `key_environment_mismatch`; redirect/fallback между средами
запрещён. Application budgets проверяются атомарно с полным `maximum_charge`
reserve.

### 2.3. `integration_service_accounts`

Одна техническая identity может иметь несколько ключей:

```text
service_account_uuid PK
application_uuid FK developer_applications ON DELETE RESTRICT
connection_uuid FK
name, status active|disabled|revoked
scopes text[]
created_by_user_uuid, created_at, revoked_at
```

Service account и connection обязаны принадлежать одной application. Connection
может иметь несколько service accounts, но service account не переносится между
applications.

### 2.4. `integration_api_keys`

```text
key_uuid PK, service_account_uuid FK
name, prefix UNIQUE, secret_hash bytea, hash_version
scopes text[] (subset account scopes)
expires_at, overlap_until, last_used_at, revoked_at, created_at
```

Plaintext ключ не сохраняется. Prefix пригоден только для поиска и UI. Последнее
использование обновляется throttled (например, не чаще раза в 5 минут), чтобы auth
не создавал write amplification.

Effective scopes — пересечение key/account/application scopes и текущих server
capabilities. Расширение scopes существующего ключа запрещено; сужение применяется
немедленно. Prefix имеет достаточную случайность и collision retry. `hash_version`
поддерживает pepper rotation без plaintext. KDF и rate limit проходят benchmark,
чтобы auth не стал CPU-DoS усилителем.

Key не хранит billing account, pricing multiplier или самостоятельные credit
budgets. Эти правила берутся из application и versioned pricing policy.

### 2.5. `ingest_events`

```text
event_uuid PK, connection_uuid FK
external_event_id, event_type, schema_version
payload_encrypted or redacted jsonb
payload_sha256 bytea
accepted bool, rejection_code nullable
received_at
UNIQUE(connection_uuid, external_event_id)
```

Hash строится по канонизированному JSON. Raw payload имеет TTL и не содержит
Authorization/presigned query secrets после redaction.

### 2.6. `ingest_items`

```text
ingest_item_uuid PK, connection_uuid/event_uuid FK
external_call_id, idempotency_key, request_sha256
source_kind url|upload|connector
recording_locator_encrypted nullable
title, original_filename, occurred_at, metadata_redacted
status/stage/lock_version
attempts, max_attempts, available_at, locked_at, locked_by
call_uuid nullable FK calls ON DELETE SET NULL
media_sha256/media_size_bytes/media_duration_seconds nullable
error_code/error_message_safe nullable
created_at/updated_at/completed_at/cancelled_at
UNIQUE(connection_uuid, external_call_id)
UNIQUE(connection_uuid, idempotency_key)
```

`recording_locator` очищается после успешного fetch или по короткому TTL.

Request hash включает route, principal/application, schema version и каноническое
представление всех значимых полей. Unicode normalization, duplicate JSON keys,
числа (`1`/`1.0`), absent/null и порядок массивов имеют явную семантику; parser
отклоняет duplicate keys и trailing data.

### 2.7. `integration_webhook_endpoints`

```text
webhook_endpoint_uuid PK
application_uuid FK developer_applications ON DELETE RESTRICT
connection_uuid nullable FK integration_connections ON DELETE RESTRICT
url_ciphertext, key_version
signing_secret_ciphertext, signing_key_version
status active|disabled|revoked
event_types text[]
created_at, updated_at, revoked_at
```

Endpoint уровня application может обслуживать несколько connections; endpoint с
`connection_uuid` ограничен одной connection той же application. URL query secrets
запрещены. Изменение URL создаёт новую settings version/audit event и не меняет уже
созданные deliveries.

### 2.8. `integration_outbox` и deliveries

Outbox фиксируется в той же транзакции, что и доменное событие:

```text
outbox_uuid, connection_uuid, event_id UNIQUE, event_type, aggregate_uuid
payload jsonb, status, attempts/max_attempts, available/locked fields
created_at, delivered_at
```

Каждая HTTP-попытка сохраняется отдельно в `integration_webhook_deliveries` с
status, latency, response size/hash, safe error; response body по умолчанию не
хранится.

### 2.9. Audit

Append-only `integration_audit_events`: actor type/UUID, connection, entity,
event type, safe metadata, request/correlation ID, timestamp. UPDATE/DELETE обычным
application role запрещаются DB privileges или trigger-ом.

Owner/migration role, restore и DBA всё ещё могут менять строки, поэтому append-only
не называется tamper-proof. Для такого свойства нужен WORM/export или hash-chain с
внешним checkpoint; иначе документируется доверие к DB-администраторам.

### 2.10. Индексы и constraints

- partial worker index `(status, available_at, created_at, uuid)`;
- connection history `(connection_uuid, created_at DESC, uuid DESC)`;
- outbox worker partial index;
- CHECK enums/counts/lengths;
- composite FK/trigger подтверждает department-company и folder scope;
- index health/metrics queries проверяется `EXPLAIN` на объёме pilot × 10;
- migrations forward-only совместимы со старым binary в rolling deploy.

## 3. HTTP API

### 3.1. Management API (user JWT)

```http
POST   /api/v1/developer-applications
GET    /api/v1/developer-applications?owner_type=&owner_uuid=&cursor=&limit=
GET    /api/v1/developer-applications/{application_uuid}
PATCH  /api/v1/developer-applications/{application_uuid}
POST   /api/v1/developer-applications/{application_uuid}/disable
POST   /api/v1/developer-applications/{application_uuid}/revoke

POST   /api/v1/companies/{company_uuid}/integrations
GET    /api/v1/companies/{company_uuid}/integrations?cursor=&limit=
GET    /api/v1/integrations/{connection_uuid}
PATCH  /api/v1/integrations/{connection_uuid}
POST   /api/v1/integrations/{connection_uuid}/disable
POST   /api/v1/integrations/{connection_uuid}/enable
DELETE /api/v1/integrations/{connection_uuid}

POST   /api/v1/integrations/{connection_uuid}/service-accounts
POST   /api/v1/service-accounts/{uuid}/keys
POST   /api/v1/integration-keys/{uuid}/rotate
DELETE /api/v1/integration-keys/{uuid}

GET    /api/v1/integrations/{connection_uuid}/ingest-items
GET    /api/v1/ingest-items/{ingest_item_uuid}
POST   /api/v1/ingest-items/{ingest_item_uuid}/retry
POST   /api/v1/ingest-items/{ingest_item_uuid}/cancel
GET    /api/v1/integrations/{connection_uuid}/audit-events
POST   /api/v1/integrations/{connection_uuid}/webhook/test
```

Create/PATCH/commands принимают `Idempotency-Key`; PATCH также `If-Match` или
`expected_lock_version`. Lists — cursor pagination с limit 1..100.

### 3.2. Public ingest API (service account)

```http
POST /api/v1/ingest/calls
POST /api/v1/ingest/calls/upload
GET  /api/v1/ingest/items/{ingest_item_uuid}
```

Upload contract: JSON metadata part плюс ровно один media part; порядок частей не
важен, per-part/header/count limits действуют до allocation. Filename не становится
filesystem path. Первая версия либо явно не поддерживает resumable upload, либо
имеет upload sessions с expiry, checksums, idempotent finalize и orphan cleanup.

Authorization: `Bearer vt_live_<prefix>.<secret>`. JWT пользователя на этих routes
не принимается как service account. Reverse proxy не должен логировать header.

JSON request:

```json
{
  "schema_version": 1,
  "external_event_id": "evt-123",
  "external_call_id": "call-456",
  "title": "Звонок клиенту",
  "recording_url": "https://example.test/recording.mp3",
  "original_filename": "recording.mp3",
  "occurred_at": "2026-08-21T09:30:00Z",
  "participants": [{"external_id":"42","display_name":"Иван","role":"manager"}],
  "metadata": {"deal_id":"1234","source_url":"https://example.test/deal/1234"}
}
```

Unknown top-level fields отклоняются для текущей schema version. Provider metadata
имеет allowlisted size/depth/key count. `participants` не превращаются в VerbaTrace
users без явного mapping.

Success: `202` + stable item/status URL. Duplicate: тот же `202/200` и
`deduplicated:true`. Conflict: `409` stable code. Не возвращать внутренние ошибки.

### 3.3. Stable error envelope

```json
{"error":{"code":"recording_url_forbidden","message":"...","request_id":"...","retryable":false}}
```

Коды документируются и не переиспользуются с другой семантикой. HTTP mapping:
400 schema, 401 invalid key, 403 scope/ACL, 404 non-disclosing, 409 conflict,
413 limit, 422 media/semantic validation, 429 quota, 503 temporary admission.

## 4. Auth, ACL и secrets

### 4.1. Management

- active `company_manager`: полный management;
- `department_leader`: read health/items только для своего department, если продукт
  явно включает это право; default deny для секретов и settings;
- employee: нет integration management;
- platform admin: только существующие explicit permissions, с audit; роль сама по
  себе не даёт plaintext secret (его всё равно нет).

### 4.2. Service account authentication

1. Parse с жёстким length/charset limit.
2. Найти active key по prefix.
3. Constant-time проверить hash с pepper/version.
4. Проверить expiry/revoke/account/connection/scopes.
5. Применить rate limit и request body limit.
6. Положить immutable principal в context.

Нельзя fallback-ить на другой тип auth при невалидном integration key.

Одинаковый внешний ответ и близкая стоимость проверки используются для unknown
prefix, wrong secret, expired/revoked key и disabled account. После auth каждая
repository query всё равно содержит company/connection boundary; UUID из URL не
является ownership proof. Revoke, membership и scopes повторно проверяются перед
привилегированной командой и первым платным вызовом.

### 4.3. Encryption

OAuth refresh/access tokens, source locator и webhook secret шифруются AEAD с
уникальным nonce, associated data `(table,row,column,key_version)` и внешним master
key/KMS. Есть key rotation job, audit и метрика записей на старой версии. Если KMS
недоступен, connection становится degraded; plaintext fallback запрещён.

## 5. Ingest transaction и идемпотентность

В короткой транзакции:

1. lock/validate connection;
2. INSERT event `ON CONFLICT`;
3. сравнить payload hash при конфликте;
4. INSERT item по external call/idempotency;
5. сравнить request hash при конфликте;
6. INSERT audit/outbox `ingest.accepted`;
7. COMMIT;
8. вернуть stable item.

Ответ до commit запрещён. Client disconnect не отменяет уже committed item.

Idempotency сохраняется не меньше максимального retry/reconciliation окна. Удалять
keys идемпотентности раньше созданного call запрещено.

До `202` admission атомарно резервирует per-key/connection/company лимиты backlog,
temp storage и незавершённых items. Освобождение reservation идемпотентно связано с
terminal state. Disable/revoke имеет server-side cut-off timestamp и policy
`continue|pause|cancel` для уже committed items, а не зависит от race с worker.

## 6. Worker lifecycle

### 6.1. Claim

Atomic claim: `FOR UPDATE SKIP LOCKED`, `attempts+1`, lease и worker ID. Stale lease
reclaim допустим только если attempts < max. Heartbeat нужен для загрузок длиннее
lease. Context cancellation освобождает временные ресурсы, но не врёт о completed.

### 6.2. Fetch

- URL decrypt только в worker memory;
- DNS resolve через injectable resolver;
- проверить все IP; connect pinned к проверенному IP с TLS ServerName;
- каждый redirect повторяет полную проверку;
- timeout connect/header/idle/total;
- max redirects, response bytes и decompression ratio;
- отклонить auth URL, userinfo и неизвестные scheme;
- stream одновременно в temp storage и SHA-256;
- не держать весь media в RAM;
- cleanup idempotent.

Redirect не переносит Authorization/cookie/source credentials на другой origin;
proxy environment variables и resolver-ы вне policy запрещены. Проверка адреса
применяется к каждому socket connect, включая IPv4-mapped IPv6 и все A/AAAA. TLS
certificate проверяется для исходного host при pinned IP. Temp object остаётся в
quarantine и недоступен downstream до validation.

### 6.3. Validation/CreateCall

Определить media по содержимому/ffprobe, duration и quota. Placement повторно
валидируется непосредственно перед CreateCall, потому что membership/folder могли
измениться после приёма. При удалённом department — blocked, без fallback.

Unique reservation связывает ingest item и call. Crash после CreateCall, но до
обновления item восстанавливается по reservation; повторный CreateCall запрещён.
Usage accounting должен иметь уникальный source reference.

`ffprobe`/decoder/antivirus считаются обработчиками враждебных файлов: отдельный
непривилегированный sandbox/container, no network, read-only root, CPU/RAM/process/
file/time limits и pinned patched versions. Archive, playlist, external-reference
и recursive formats запрещены, если не нужны. Malware policy закрывается до pilot.

### 6.4. Processing observation

Не polling tight loop. Предпочтительно доменное outbox событие при переходе call в
terminal state. До его появления допускается low-frequency reconciler с индексом.

### 6.5. Retry policy

Backoff: configurable exponential + full jitter, `Retry-After` учитывается с cap.
Error taxonomy:

- retryable: timeout, DNS temporary, 408/425/429/5xx, storage unavailable;
- permanent: schema, unsupported media, hard size, SSRF, 401/403 source;
- blocked: billing entitlement, revoked placement, configuration/secrets;
- internal invariant: quarantined + alert, не бесконечный retry.

Manual retry создаёт command event, меняет `available_at`, но не обнуляет историю.
Override max attempts разрешён только manager/admin policy с причиной.

### 6.6. Credit billing contract

Ingest не изменяет баланс напрямую и не использует legacy minute counter как
финансовый источник истины. До первого платного provider call worker создаёт
`usage_operation` с application, connection, ingest item, call, billing account,
environment и pricing snapshot, затем вызывает общий billing service reserve.

- `(usage_operation_uuid, operation_type)` уникален;
- до provider call полностью резервируется рассчитанный `maximum_charge`; при
  нехватке операция не запускается;
- дополнительный reserve после запуска запрещён;
- actual меньше/equal cap: settle actual и release остатка; actual выше cap: settle
  только cap, разница `internal_pricing_loss` + alert;
- обычный available/reserved balance неотрицателен; postpaid/debt не входит в
  первую версию;
- повтор claim/retry использует ту же operation, reserve и settlement;
- insufficient credits переводит конкретный этап в
  `blocked_insufficient_credits`, не удаляя media/transcript;
- funding event будит blocked item через outbox/reconciler без повторения уже
  оплаченного этапа;
- sandbox mock не имеет маршрута к real ledger/provider;
- sandbox real требует `ai:real`, budgets и явный processing mode, но применяет ту
  же pricing version и reserve/settle семантику;
- actual provider usage/cost сохраняется до settle; неизвестный outcome остаётся
  `reconciling`;
- финансовая транзакция и ingest state координируются durable outbox/saga, а не
  распределённой DB-транзакцией;
- employee API не раскрывает company wallet balance; backend capability является
  единственным источником разрешения.

Fault-injection tests обязательны для crash до/после reserve, provider response,
call/transcript commit, settle и outbox publish. Сто параллельных одинаковых ingest
requests должны создать один billable result и одно customer settlement.

## 7. Outgoing webhooks

Envelope:

```json
{"id":"evt_uuid","type":"call.analysis.ready","version":1,
 "occurred_at":"...","data":{"ingest_item_uuid":"...","call_uuid":"...","status":"completed"}}
```

Headers:

```text
X-VerbaTrace-Event-Id
X-VerbaTrace-Timestamp
X-VerbaTrace-Signature: v1=<hex HMAC-SHA256(timestamp + "." + raw_body)>
```

Timestamp replay window документируется. Один canonical raw body используется и
для подписи, и для отправки. 2xx — success; 408/425/429/5xx — retry; прочие 4xx —
permanent после ограниченного числа диагностических повторов. URL проходит ту же
SSRF policy. Secret rotation поддерживает overlap двух подписей/versions.

Delivery ordering сетью не гарантируется: envelope содержит aggregate sequence, а
consumer contract требует дедупликацию. Retry имеет deadline/TTL и durable
dead-letter/operator state. SSRF проверяется при каждой попытке. Test delivery имеет
отдельный event type. Signing secret выдаётся/ротируется one-time с `no-store` и не
возвращается management GET.

## 8. Bitrix24 adapter contract

- OAuth state подписан, одноразовый, TTL и привязан к user/company/connection;
- Authorization Code использует PKCE S256, если vendor поддерживает; иначе риск
  фиксируется и компенсируется строгим redirect URI и server-side code exchange;
- callback повторно проверяет manager membership и исходную browser session; consume
  state и token save атомарны, повтор callback не перезаписывает connection;
- callback URL HTTPS, redirect allowlist;
- `member_id` — стабильная identity портала, domain не используется как PK;
- refresh под advisory/distributed lock, чтобы не было token-refresh stampede;
- invalid_grant переводит connection в degraded/reconnect_required;
- vendor event ack следует требованиям vendor, но durable event commit обязателен;
- adapter сохраняет vendor event ID и version before normalization;
- reconciliation использует watermark + overlap window и ту же дедупликацию;
- rate limits и Retry-After учитываются;
- permissions проверяются health test и отображаются как missing scopes;
- redacted contract fixtures версионируются.

До подтверждения реального API media на тестовом портале adapter остаётся
`experimental/not_verified`, даже если unit tests зелёные.

## 9. Concurrency и bottlenecks

- bounded pools отдельно для fetch, ffprobe и webhook; один медленный host не
  занимает весь pool;
- per-host/per-connection concurrency и circuit breaker;
- admission limits предотвращают исчерпание disk/temp/DB connections;
- DB claim batch малый, network работает после commit;
- large JSON/raw payload не читается без limit;
- last_used/health writes coalesced;
- indexes предотвращают full scan очередей;
- connection-level noisy neighbor ограничивается quota/fair scheduling;
- graceful shutdown прекращает claim, завершает/освобождает leases;
- disk/object storage pressure имеет readiness degradation и alert;
- webhook receiver может быть медленным бесконечно — delivery отделён от ingest.

## 10. Retention и deletion inventory

При удалении call существующий retention lifecycle удаляет media/analysis и связь с
ingest, но сохраняет минимальный технический event без title/participants/source
URL. Connection deletion:

- revoke keys/tokens немедленно;
- stop workers/deliveries;
- сохранить audit по утверждённому сроку;
- очистить encrypted raw payload/locators;
- не удалять calls и usage;
- не оставлять OAuth tokens или webhook secret.

Новые integration tables добавляются в retention inventory и тест FK graph.

До pilot закрываются сроки для raw/redacted event, idempotency, audit, delivery и
locator. Legal/incident hold останавливает erasure только для перечисленных
сущностей и аудируется. DSAR/erasure inventory охватывает object versions,
multipart parts, caches, backup expiry и vendor tokens. Повтор vendor event не
должен тихо воскресить удалённые данные без новой policy decision.

## 11. Observability и operations

Logs structured/redacted. Error code — bounded label; UUID/external ID не labels
метрик. Traces не содержат payload/media URL.

Runbook охватывает: queue growth, stuck lease, source outage, KMS failure, webhook
failure, token expiry, object storage pressure, duplicate alarm, pause/resume,
reconciliation и rollback deploy.

До pilot утверждаются численные SLO, capacity envelope, RPO/RTO и maximum queue
age. Restore проверяет согласованную пару DB/object storage, KMS keys, ledger links
и outbox; PITR сохраняет stable event ID. Метрики tenant-safe и bounded-cardinality;
external ID ищется по keyed hash, не через plaintext labels/logs.

Health API различает liveness/readiness/provider health. Сбой одной CRM не делает
весь API unready; исчерпание DB/storage capacity — делает.

## 12. Тестовая стратегия

### Unit/property/fuzz

- key parse/hash/scope/rotation;
- canonical JSON/idempotency conflicts;
- state machine и error classification;
- URL/IP/redirect/DNS rebinding SSRF matrix;
- HMAC canonical signature;
- retry/backoff bounds;
- payload parser fuzz и metadata limits.

### Repository integration

- unique races двумя транзакциями;
- SKIP LOCKED/lease reclaim;
- outbox atomicity;
- optimistic lock;
- company/department/folder constraints;
- deletion/retention inventory;
- migrations up/down на disposable DB и forward compatibility.

### Service/API

- 100 параллельных одинаковых requests → один item/call;
- same key/different payload → conflict;
- crash points до/после commit, fetch, storage save, call creation, outbox;
- billing exactly once;
- revoked key/connection during processing;
- authorization/non-disclosure;
- body/media limits и cancellation.

### Contract/E2E/load

- local source server: redirects, slowloris, 429, truncated/chunked, corrupt media;
- webhook receiver: signature, duplicate, Retry-After, timeout;
- connector emulator fixtures;
- real Bitrix24 trial/NFR checklist отдельно от CI;
- sustained load, burst, DB pool, disk/temp, queue age и graceful restart.

## 13. Delivery gates

Обязательны: `go test ./...`, integration tests с PostgreSQL, race для новых
packages, lint, migration validation, Docker build, security tests и API contract
snapshot. Для publication отдельно проверяются CI exact SHA.

Нельзя заявлять Bitrix24-ready по mock-only тестам, production-ready по backend
компиляции или reliable ingest без fault-injection/idempotency evidence.
