# Спецификация: тарифы, Integration API v2 и маршрутизация внешних звонков

Статус: product and production design, реализация не начата
Дата: 2026-08-24
Backend: `C:\projects\VerbaTrace\Monolit`
Frontend: `C:\projects\VerbaTrace-frontend`

Связанные документы:

- [Кредитный биллинг и developer platform](./credit-billing-developer-platform-rollout.md)
- [Платформа автоматического поступления звонков](./integration-ingest-platform.md)
- [Backend-контракт интеграций](./integration-ingest-platform-backend.md)
- [Frontend-контракт интеграций](./integration-ingest-platform-frontend.md)
- [Retention и история инструкций](./call-retention-and-instruction-history.md)

Этот документ фиксирует решения, принятые после реализации первой версии credit
billing и Generic Ingest API. Он дополняет связанные документы и имеет приоритет
в вопросах тарифных значений, доступности API по тарифам, unlimited business
members и выбора папки для внешнего импорта. Код и данные не меняются до отдельной
команды на реализацию.

## 1. Цели

1. Заменить механическую конвертацию старых минут в кредиты тарифной сеткой,
   учитывающей транскрипцию, диаризацию, идентификацию говорящих и анализ.
2. Дать пользователю понятный ориентир в часах, сохранив кредиты единственным
   исполняемым лимитом backend.
3. Предоставить Integration API на Personal Pro и всех business-тарифах.
4. Разрешить внешнему источнику безопасно направлять звонок в личную область,
   компанию или отдел и, при наличии полномочий, в конкретную папку.
5. Если папка не указана, атомарно использовать системную папку `Внешняя` внутри
   выбранной области.
6. Не ограничивать количество сотрудников и пользователей в business-тарифах;
   ограничивать потребление кредитами и техническими anti-abuse quotas.
7. Сделать pricing, credit usage и integration UX целостными, русскоязычными и
   визуально зрелыми на desktop и mobile.

## 2. Неподвижные продуктовые решения

### 2.1. Тарифная сетка

| Код | Цена в месяц | Allowance | Ориентир обработки | Retention | Export | API/webhooks |
|---|---:|---:|---:|---:|---|---|
| `personal_start` | 0 RUB | 250 000 | около 3 часов | 30 дней | нет | нет |
| `personal_plus` | 1 990 RUB | 1 500 000 | около 20 часов | 365 дней | да | нет |
| `personal_pro` | 4 990 RUB | 6 000 000 | около 80 часов | 365 дней | да | да |
| `business_start` | 14 900 RUB | 7 000 000 | около 100 часов | 180 дней | да | да |
| `business_plus` | 39 900 RUB | 30 000 000 | около 400 часов | 365 дней | да | да |
| `business_pro` | 99 900 RUB | 90 000 000 | около 1 200 часов | 550 дней | да | да |
| `enterprise` | по формуле | от 90 000 000 | от 1 200 часов | договорной | да | да |

Публичные названия: `Start`, `Personal Plus`, `Personal Pro`, `Business Start`,
`Business Plus`, `Business Pro`, `Enterprise`. В русскоязычной витрине допускаются
`Бесплатный`, `Персональный Плюс`, `Персональный Про`, `Бизнес Старт`,
`Бизнес Плюс`, `Бизнес Про`; backend codes и API contract не локализуются.

Часы являются маркетинговым ориентиром representative workload, а не вторым
лимитом. Backend никогда не блокирует операцию по минутам, если credit admission
успешен. UI рядом с часами показывает пояснение: расход зависит от режима
транскрипции, продолжительности и числа анализов.

`personal_start` остаётся без API, с retention 30 дней. Это ограничивает abuse и
стоимость бесплатного пользователя. `personal_pro` получает Generic API и
webhooks. Все business-тарифы получают одинаковый API surface и экспорт; они
различаются allowance, retention, priority, аналитикой, audit depth и support.

### 2.2. Representative workload

Витринные часы рассчитаны на среднем звонке 30 минут и одном анализе на звонок:

```text
personal_plus:
1 200 минут * 992 credits + 40 * 2 275 credits = 1 281 400 credits

personal_pro:
4 800 минут * 1 109 credits + 160 * 2 275 credits = 5 687 200 credits
```

Business allowance включает operational headroom относительно витринного объёма.
Headroom покрывает различия длины контекста и небольшое число повторных внутренних
attempts, но не бесплатный повтор пользовательского анализа. Cost telemetry должна
ежемесячно считать P50/P90/P99 фактических часов на тариф, долю исчерпавших лимит и
gross margin. Изменение витринного ориентира или allowance требует versioned plan
revision, а не silent update.

### 2.3. Оценка бесплатного тарифа

Representative usage:

```text
180 минут * 875 credits + 6 анализов * 2 275 credits = 171 150 credits
```

При reference multiplier `3.5` provider-cost эквивалент составляет около 41 RUB
при курсе 82.9977 RUB/USD. Полное использование 250 000 credits соответствует
примерно 59 RUB provider cost. Это не полная себестоимость: storage, traffic, CPU,
retry loss, payment, support и налоги считаются отдельно.

### 2.4. Business без лимита сотрудников

Поля `member_limit` и аналогичные продуктовые блокировки не применяются к новым
business plan revisions. Компания может добавлять любое число пользователей и
сотрудников в пределах технически допустимой ёмкости системы. Нельзя маскировать
лимит сотрудников через departments, invitations, API keys или service accounts.

Защита от abuse реализуется отдельно:

- credit allowance и purchased wallet;
- request rate limits на key/application/owner/IP;
- ограничения размера файла и duration;
- ограниченная concurrency processing, являющаяся scheduler policy, а не числом
  сотрудников;
- backlog/storage admission control;
- fair-use review для аномальной автоматизации с non-destructive notification.

Технические quotas должны возвращаться capability API и не рекламироваться как
лимит пользователей.

### 2.5. Enterprise formula

Enterprise начинается строго выше Business Pro по объёму. Базовая формула:

```text
volume_component = requested_monthly_credits / 90_000_000
                   * 99_900 RUB
                   * enterprise_markup

enterprise_markup default = 1.05
enterprise_markup allowed range = [1.00, 1.10]

custom_component = verified_incremental_monthly_cost * custom_markup
custom_markup default = 1.10, maximum = 1.10

monthly_price = round_up_1_000(volume_component + custom_component)
```

`verified_incremental_monthly_cost` включает только измеряемые расходы конкретного
договора: dedicated infrastructure, private storage, увеличенный egress, agreed
support capacity, SSO/SCIM vendor fees и SLA reserve. Произвольная «стоимость
сложности» без расчёта запрещена.

Пример без custom component:

```text
180 000 000 / 90 000 000 * 99 900 * 1.05 = 209 790 RUB
contract price = 210 000 RUB/month
```

Формула повышает unit revenue на 5%, максимум на 10%, но не гарантирует фактическую
прибыль без полной cost telemetry. Quote сохраняет inputs, exchange rate, cost
snapshot, markup, author, approver, validity interval и версию формулы. Изменение
формулы не меняет активный договор до renewal/amendment.

Годовая скидка применяется после формулы и не должна снижать forecast gross margin
ниже утверждённого floor. Floor устанавливается финансовым решением после пилота;
до появления полных расходов система не заявляет гарантированную маржу.

## 3. Тарифный и billing-контракт

### 3.1. Versioned plan revisions

Нельзя переписывать исторические параметры плана без версии. Требуется одна из
моделей:

1. `plan_versions` с immutable revision и effective interval; либо
2. новый immutable `plan_uuid` на каждую коммерческую revision при стабильном
   публичном `plan_code`.

Subscription закрепляет revision на billing period. На новом периоде применяется
revision, подтверждённая пользователем согласно правилам изменения договора.

Минимальные поля revision:

```text
plan_revision_uuid
plan_code
version
currency = RUB
monthly_price_minor
monthly_credit_allowance
marketing_hours_hint
history_retention_days
export_enabled
api_access_enabled
webhooks_enabled
processing_priority
team_analytics_enabled
audit_tier
support_tier
effective_from/effective_to
status draft|scheduled|active|retired
created_by/approved_by/created_at
```

`monthly_minutes_limit` после миграции не участвует в enforcement. Оно либо
удаляется после compatibility window, либо явно маркируется legacy и не отдаётся
новому frontend.

### 3.2. Миграция действующих подписок

- инвентаризировать active subscriptions и открытые allowance epochs;
- не менять allowance уже открытого периода задним числом;
- новую revision применить со следующего billing boundary;
- preview migration показывает old/new price, allowance, retention и capabilities;
- downgrade retention не удаляет данные немедленно: действует контракт retention;
- увеличение retention может продлить существующие звонки только по формально
  выбранной policy;
- migration idempotent, resumable, audited и имеет rollback до activation;
- purchased/promotional/sandbox wallet не изменяется;
- unlimited members требует отдельной проверки invitations и UI, а не только
  значения `NULL` в plans.

### 3.3. Entitlements

Backend вычисляет effective entitlements централизованно. Проверки вида
`plan_code == business_pro` в handlers/services запрещены. Capability response:

```json
{
  "export": true,
  "integration_api": true,
  "webhooks": true,
  "company_members_unlimited": true,
  "retention_days": 365,
  "processing_priority": "standard",
  "audit_tier": "extended"
}
```

Frontend показывает функции по response, но backend остаётся enforcement source.
Отсутствующая capability возвращает стабильный `403 feature_not_available` с
`required_plan_codes`; это не `404` и не credit error.

## 4. Integration API v2: границы

### 4.1. Входит в v2

- upload media и ingest by URL;
- status/list ingest items;
- чтение созданного звонка, транскрипции и последнего доступного анализа;
- запрос экспорта и получение его состояния/ссылки;
- управление webhook endpoints через API key;
- list доступных destinations и folders;
- per-request placement в personal/company/department scope;
- системная папка `Внешняя` по умолчанию;
- idempotency, pagination, rate-limit metadata, audit и signed webhooks.

### 4.2. Не входит без отдельного решения

- изменение текста транскрипции внешней системой;
- управление company memberships, ролями и invitations;
- создание/удаление отделов;
- обход Human QA;
- произвольное выполнение внутренних admin API;
- передача provider/model, не разрешённых pricing policy;
- bulk export без asynchronous job и credit/storage admission.

## 5. Identity, scopes и environments

Application принадлежит personal user либо company. Service account принадлежит
application и connection. API key наследует пересечение application capabilities
и service-account scopes.

Целевые scopes:

| Scope | Возможность |
|---|---|
| `calls:write` | создать ingest item/upload |
| `calls:read` | status/list call/transcript/analysis metadata |
| `exports:write` | создать export job |
| `exports:read` | получить status/download URL |
| `usage:read` | usage/quota/key validation |
| `destinations:read` | доступные области и папки |
| `webhooks:read` | endpoints/deliveries |
| `webhooks:write` | create/update/test/revoke endpoints |
| `ai:real` | production AI в разрешённом environment |

Legacy `webhooks:manage` мигрируется в `webhooks:read` + `webhooks:write`; на
compatibility window принимаются оба, но response возвращает canonical scopes.

Sandbox и production имеют разные keys, data namespace и wallets. Sandbox key не
может вызвать production route; ошибка — `401 key_environment_mismatch` без
раскрытия существования application. Production API недоступно без active
entitlement и active subscription/grace policy.

## 6. Placement model

### 6.1. Термины

`destination` — область владения звонком и папкой:

```text
personal:   user_uuid
company:    company_uuid
department: company_uuid + department_uuid
```

Профиль в пользовательском тексте означает личную область владельца application.
Отдельной сущности `employee_profile_uuid` в текущей модели нет. API не вводит
неподтверждённую сущность: company member остаётся участником компании, а не новым
owner scope. Если позже появятся автономные employee profiles, это новая schema
version и отдельная ACL-модель.

### 6.2. Connection boundary

Connection задаёт максимальную область делегирования:

- personal application: только personal destination владельца;
- company application без department: company и любой доступный department этой
  компании;
- company connection с fixed department: только этот department;
- fixed folder connection: только эта folder, если `allow_folder_override=false`;
- `allow_folder_override=true`: любая активная folder внутри delegated scope.

Это предотвращает ситуацию, когда ключ одного отдела передаёт UUID папки другого
отдела. Per-request placement никогда не расширяет connection boundary.

### 6.3. Request contract

В metadata JSON upload и JSON ingest добавляется:

```json
{
  "schema_version": 2,
  "external_event_id": "evt-123",
  "external_call_id": "call-456",
  "title": "Разговор с клиентом",
  "destination": {
    "scope": "department",
    "company_uuid": "...",
    "department_uuid": "...",
    "folder_uuid": "..."
  }
}
```

Правила:

1. `scope` обязателен только при override connection default.
2. `personal`: company/department запрещены.
3. `company`: company обязателен, department запрещён.
4. `department`: company и department обязательны и composite-valid.
5. `folder_uuid` optional и должен принадлежать exact destination.
6. Folder из более широкой или более узкой области не принимается автоматически.
7. Если `destination` отсутствует, используется default connection placement.
8. Если folder отсутствует, используется exact system folder `Внешняя`.
9. Arbitrary `folder_name` не принимается: это создаёт duplicates и ambiguity.
10. Смена placement при повторе того же idempotency key является conflict.

Чтобы сделать API удобным, список destination/folder UUID получается отдельным
endpoint, а не копируется пользователем из URL интерфейса.

### 6.4. Destination endpoints

```http
GET /api/{environment}/v2/destinations?cursor=&limit=
GET /api/{environment}/v2/destinations/{scope}/{owner_uuid}/folders?cursor=&limit=&q=
```

Для department compound identity передаётся явными query/path fields согласно
OpenAPI; неоднозначный polymorphic path запрещён. Ответ содержит display names,
scope, immutable UUID, `is_system`, `can_ingest`, archived state и cursor.

## 7. Системная папка `Внешняя`

### 7.1. Data model

`call_folders` расширяется:

```text
system_type nullable: external_ingest
is_system generated/derived from system_type
created_by_actor_type user|system
```

Database constraints:

- `system_type` входит в allowlist;
- partial unique index на одну active `external_ingest` folder для exact personal
  owner;
- partial unique index на одну active для exact company scope;
- partial unique index на одну active для exact department scope;
- system folder нельзя soft-delete, rename или переместить обычным endpoint;
- assignment folder/call должен иметь совместимый scope;
- department/company composite FK предотвращает cross-company placement.

Имя `Внешняя` является локализованным display label. Identity определяется
`system_type`, а не текстом имени: пользовательская папка с названием `Внешняя` не
считается системной и не перехватывает импорт.

### 7.2. Creation semantics

System folder создаётся lazy при первом успешном admission либо eager migration.
Предпочтение: lazy `INSERT ... ON CONFLICT ... RETURNING` внутри короткой
транзакции placement resolution. Два параллельных импорта получают один UUID.

Folder создаётся до durable ingest commit или в той же транзакции, чтобы accepted
item никогда не остался без placement. При rollback orphan folder допустима только
как системная пустая folder; cleanup её не удаляет.

### 7.3. Lifecycle

- archive department/company блокирует новые imports до него;
- существующая системная folder и assignments сохраняются по retention policy;
- revoke connection не удаляет folder;
- deleting ordinary folder, указанной как default connection folder, переводит
  connection в `degraded` и fallback запрещён до явного решения;
- отсутствующий optional request folder не вызывает fallback в `Внешняя`: ответ
  `404 folder_not_found` или non-disclosing `403 destination_denied`;
- fallback в `Внешняя` применяется только когда folder не указан.

## 8. Admission и идемпотентность

Порядок admission:

1. parse route/environment;
2. authenticate key и scope;
3. проверить entitlement/subscription/grace;
4. strict decode, schema и media/url limits;
5. resolve connection boundary;
6. resolve destination и folder ACL;
7. вычислить canonical payload hash, включая resolved placement;
8. проверить idempotency/external IDs;
9. reserve backlog/storage/credit budget;
10. durable commit ingest item с immutable placement snapshot;
11. вернуть `202`.

`ingest_items` сохраняет snapshot:

```text
destination_scope
user_uuid/company_uuid/department_uuid
folder_uuid
connection_settings_version
placement_source connection_default|request_override|system_external
entitlement_snapshot_uuid
pricing_snapshot_uuid
```

Worker не перечитывает mutable connection destination. Изменение connection после
`202` не меняет уже принятый item.

Idempotency:

- same key + same canonical payload/placement → прежний response;
- same key + different folder/destination/media → `409 idempotency_conflict`;
- same `(connection, external_call_id)` → один call;
- same external event + different payload → `409 external_event_conflict`;
- retry worker/manual retry не создаёт новую reservation/usage/call;
- request без `Idempotency-Key` в production отклоняется `400`.

## 9. External API surface

Canonical prefix:

```text
/api/sandbox/v2
/api/production/v2
```

### 9.1. Ingest

```http
POST /ingest/calls
POST /ingest/calls/upload
GET  /ingest/items/{ingest_item_uuid}
GET  /ingest/items?status=&cursor=&limit=
POST /ingest/items/{ingest_item_uuid}/cancel
POST /ingest/items/{ingest_item_uuid}/retry
```

Cancel/retry через API key требуют `calls:write`, ownership и state-machine check.
Production manual retry не обнуляет attempts и не списывает повторно успешные
usage operations.

### 9.2. Read model

```http
GET /calls/{call_uuid}
GET /calls/{call_uuid}/transcription
GET /calls/{call_uuid}/analysis
```

Ответы содержат только данные, разрешённые application owner/connection boundary.
Audio download не входит по умолчанию; если понадобится, вводится отдельный
`media:read` scope и short-lived signed URL. Analysis endpoint возвращает явное
состояние `not_started|processing|ready|failed`, version и evidence availability.

### 9.3. Exports

```http
POST /exports
GET  /exports/{export_uuid}
GET  /exports/{export_uuid}/download
```

Export asynchronous, idempotent и scope-bound. Download URL short-lived,
single-purpose, не логируется и не отдаётся после expiry/revoke. Форматы имеют
allowlist. CSV защищён от formula injection, архивы — от path traversal.

### 9.4. Webhooks

```http
POST   /webhooks
GET    /webhooks
PATCH  /webhooks/{webhook_uuid}
POST   /webhooks/{webhook_uuid}/test
GET    /webhooks/{webhook_uuid}/deliveries?cursor=&limit=
DELETE /webhooks/{webhook_uuid}
```

События первой версии:

```text
ingest.accepted
ingest.completed
ingest.failed
call.transcription.ready
call.analysis.ready
export.ready
export.failed
```

Webhook payload содержит stable event UUID, schema version, occurred_at,
application/connection IDs, aggregate ID и safe payload. Подпись HMAC включает
timestamp + raw body; receiver tolerance документируется. Secret показывается один
раз. Deliveries имеют exponential backoff+jitter, maximum attempts, DLQ-like
terminal state и manual redelivery с новым delivery UUID, но тем же event UUID.

SSRF-защита применяется к webhook URL и recording URL: DNS resolution/rebinding,
redirect chain, private/link-local/metadata ranges, protocol allowlist, size/time
limits и egress proxy policy.

### 9.5. Errors, pagination и quotas

Единый error envelope:

```json
{
  "error": {
    "code": "folder_scope_mismatch",
    "message": "Папка не относится к выбранной области",
    "request_id": "...",
    "retryable": false,
    "field": "destination.folder_uuid"
  }
}
```

Стабильные codes включают auth/scope/environment/entitlement, invalid schema,
destination denied, archived destination, folder not found/scope mismatch,
idempotency conflicts, insufficient credits, backlog full, rate limited, media
invalid/too large, retryable upstream failure и internal error.

List API используют cursor pagination с deterministic `(created_at, uuid)` order.
Offset pagination не используется во внешнем v2. `429` возвращает `Retry-After` и
rate-limit headers. Quotas считаются на key, application, billing owner и global;
IP является дополнительным сигналом, но не единственным ключом ограничения.

## 10. ACL и безопасность

1. Personal application видит только данные владельца.
2. Company application не получает доступ по факту знания UUID; ownership и
   delegated boundary проверяются в каждом repository query.
3. Department должен быть active и принадлежать exact company.
4. Folder должна быть active и exact-scope compatible.
5. Calls/read/export/webhook используют тот же principal, не user impersonation.
6. Создатель application не становится вечным runtime actor; membership revoke
   закрывает management access, service account продолжает по company policy.
7. API secrets хранятся hash-only; webhook secrets и URL — encrypted at rest.
8. Secrets, recording URLs, Authorization и raw metadata не попадают в logs/audit.
9. Metadata имеет size/depth/key allowlist, redaction и запрещённые prototype-like
   keys для downstream JS consumers.
10. Media проходит quarantine, format validation и resource-bounded processing.
11. OpenAPI examples не содержат реальных secrets/UUID/PII.
12. Key rotation поддерживает bounded overlap; revoke вступает в силу для новых
    admission немедленно.

## 11. Credits и processing

- admission проверяет available allowance/wallet и operation maximum;
- URL ingest до фактической duration использует conservative reservation;
- после probing reservation корректируется, затем settle по фактической цене;
- недостаток кредитов до provider call переводит item в `blocked_credits`, не
  создавая платный внешний запрос;
- top-up автоматически разблокирует только items, разрешённые policy;
- повтор provider attempt учитывается как internal retry loss, если пользователь
  не инициировал новую billable operation;
- user-triggered reanalysis — новая operation и новый idempotency contract;
- sandbox mock не расходует production credits;
- sandbox real расходует реальные credits и явно помечается до подтверждения.

Priority scheduler не должен допускать starvation Start/Plus. Используются
weighted queues, per-owner concurrency и global capacity. Business Pro/Enterprise
получают меньшую ожидаемую queue latency, но не обещание без измеримого SLA.

## 12. Frontend: pricing и subscription UX

### 12.1. Визуальный принцип

Интерфейс сохраняет текущую тёмную/стеклянную visual language, но не строится как
набор вложенных рамок. Иерархия достигается spacing, typography, surface contrast и
одним accent action. Все тексты русские, кроме API, API key, URL, webhook и
названий протоколов/форматов.

### 12.2. Pricing cards

Каждая карточка показывает:

- название и цену `/ месяц`;
- ориентир часов крупнее технических credits;
- 4–6 отличий без внутренних версий и служебных полей;
- retention человеческим текстом;
- API/export availability;
- основной CTA;
- tooltip о зависимости расхода от режима обработки.

Credits не скрываются полностью: detail drawer показывает allowance, пример
расхода и ссылку на методику. Не показывать «безлимит», если есть credit limit.

Business cards не показывают число участников. Вместо этого: `Без ограничения по
сотрудникам и пользователям`. Рядом пояснение, что объём обработки общий для
компании.

Enterprise calculator принимает monthly hours/credits и optional features,
показывает ориентировочную цену, диапазон и состав расчёта. Это estimate, не
оферта. Пользователь не может выставить markup; он видит только итог и допущения.

### 12.3. Usage display

Sidebar показывает компактно:

```text
Обработка в этом месяце
62% использовано
Сброс через 12 дней
```

Detail раскрывает credits, estimated remaining hours range, операции и wallet.
Нельзя показывать точное число оставшихся часов как гарантию. На 70/90/100%
показываются последовательно informational/warning/blocked states без layout jump.

## 13. Frontend: integration UX

### 13.1. Information architecture

```text
Настройки → Интеграции
  Приложения
  Подключения
  API-ключи
  Webhooks
  Импорты
  Аудит
```

На mobile это tabs/sections без горизонтального overflow. На desktop master-detail
layout сохраняет название выбранного API/connection в заголовке правой области.

### 13.2. Connection and destination setup

Форма подключения:

1. понятное название;
2. personal/company owner;
3. default destination: личная область/компания/отдел;
4. default folder: `Внешняя` либо пользовательская;
5. разрешить ли request folder override;
6. processing mode/instructions;
7. webhooks optional.

При смене company/department несовместимая folder очищается с объяснением. Preview
показывает точный путь: `Компания → Продажи → Внешняя`. Системная папка имеет badge
`По умолчанию для API`, а не технический `v1`.

### 13.3. Key UX

- полный secret показывается один раз;
- вся область secret и кнопка `Скопировать` кликабельны с keyboard focus;
- успешное копирование вызывает disappearing toast `API-ключ скопирован`;
- ошибка clipboard имеет fallback selection и понятное сообщение;
- закрытие one-time modal требует подтверждения сохранения;
- secret не сохраняется в URL, localStorage, analytics или crash reports;
- для всех copy actions используется единый компонент и единый toast pattern.

### 13.4. Imports

На overview показываются последние 10 импортов и кнопка `Все импорты`. Полная
страница использует cursor pagination, server filters и URL state. Строка показывает
status, безопасное название, источник, destination path, папку, время, credits и
created call link. Failed/blocked state показывает безопасную причину и допустимое
действие. Retry/cancel недоступны, если state machine запрещает команду.

Desktop table и mobile cards проходят состояния loading, empty, stale, partial,
permission denied, retryable error и terminal error. Нижняя карточка/аудит имеют
safe-area/padding и никогда не прижимаются к viewport edge.

### 13.5. Webhooks и API docs

Webhook UI показывает русские event labels с техническим code вторичным текстом.
URL остаётся `HTTPS URL`. Доступны deliveries, response status/latency, test и
redelivery. Signing secret имеет тот же one-time UX.

Setup page содержит curl/Go/JavaScript examples, environment switch, base URL,
scopes, destination picker и OpenAPI download. Copy feedback единообразен. В
пример не подставляется настоящий secret после закрытия modal.

### 13.6. Layout quality gates

- widths: 360, 390, 768, 1024, 1280, 1440 px;
- dark/light theme;
- 200% zoom и keyboard-only navigation;
- Russian long text, UUID, URL, key prefix и 6-digit prices не создают overflow;
- cards не пересекаются, sticky footer не закрывает content;
- modal/drawer имеет safe viewport height и внутренний scroll;
- toast не перекрывает CTA и доступен screen reader;
- focus возвращается к вызвавшему элементу;
- status передаётся текстом/иконкой, не только цветом;
- reduced motion отключает необязательные анимации.

## 14. Observability и operations

Метрики:

- admission rate/latency по environment/plan/status;
- auth/scope/entitlement/rate-limit rejects;
- placement resolution failures по safe code;
- system folder create conflict/retry;
- ingest queue depth/age, per-stage latency, retry/blocked/failed;
- credits reserved/settled/released и reconciliation drift;
- provider cost and retry loss per plan;
- webhook success/latency/attempt/DLQ;
- export queue/storage/egress;
- actual hours and gross margin distribution per plan;
- free-to-paid conversion и abusive free usage.

Логи структурированы по request/event/ingest/call UUID без PII/secrets. Traces не
содержат raw metadata/media URL. Alerts имеют owner и runbook:

- queue age SLO breach;
- credit reservation leak;
- no system folder uniqueness violation;
- repeated destination ACL denials anomaly;
- webhook permanent failure spike;
- provider/reconciliation drift;
- Enterprise margin forecast below floor.

Workers имеют lease, heartbeat, bounded retries, jitter, poison-item isolation и
graceful shutdown. Cleanup inventory покрывает multipart temp, staging media,
expired exports, webhook payload TTL и revoked secrets metadata.

## 15. Bottlenecks и capacity

Перед production rollout обязательны load tests:

1. burst admission одного крупного Enterprise owner;
2. fairness между 1 000 small owners и одним large owner;
3. параллельное lazy creation `Внешняя`;
4. 500 MB uploads с медленными клиентами и оборванными соединениями;
5. URL fetch с redirect/DNS rebinding/slowloris;
6. queue backlog при деградации ASR/LLM;
7. webhook endpoint с timeout/429/5xx;
8. export множества длинных звонков;
9. list imports с миллионами rows и cursor indexes;
10. retention cleanup при одновременном чтении/download.

Вероятные bottlenecks:

- локальный filesystem несовместим с несколькими replicas — production требует
  shared object storage;
- PostgreSQL queue требует индексы claim path, autovacuum tuning и capacity gate;
- media probing/transcoding требует CPU/memory isolation;
- folder ACL joins и polymorphic scope требуют composite indexes;
- unlimited members увеличивают notification/search/list load даже без роста
  credits;
- exports создают storage/egress burst;
- per-item polling frontend создаёт N+1 — нужен aggregate list refresh/SSE;
- webhook retries могут усилить outage — нужны per-host circuit breaker и budgets.

Capacity decision documented до launch: maximum sustained admission, concurrent
uploads, processing throughput, maximum queue age, storage headroom и RTO/RPO.

## 16. Миграции

Последовательность:

1. добавить versioned plan revisions и enterprise quote storage;
2. seed новые revisions без activation;
3. добавить entitlements response и shadow comparison со старой логикой;
4. добавить system folder fields/constraints/indexes;
5. backfill/validate existing folders и connection placements;
6. добавить immutable placement snapshot в ingest items;
7. добавить v2 scopes и compatibility mapping;
8. реализовать v2 read/export/webhook/destination APIs за flags;
9. frontend pricing/integration UI читает новые contracts за flags;
10. shadow/load/security/e2e gates;
11. activate new plan revisions на billing boundary;
12. постепенно включить v2 production admission;
13. удалить legacy enforcement только после telemetry window.

Каждая migration имеет preflight counts, batches, progress, resume marker,
post-validation и rollback/forward-fix plan. Concurrent index creation используется
там, где применимо. Constraint сначала `NOT VALID`, затем validate, если таблица
крупная. Нельзя одновременно менять tariff, credit enforcement и API routing без
раздельных flags.

## 17. Feature flags и rollout

Независимые flags:

```text
plan_revisions_v2_read
plan_revisions_v2_enforce
unlimited_business_members
integration_v2_destinations
integration_v2_folder_override
integration_v2_read_api
integration_v2_exports
integration_v2_webhook_management
system_external_folder
frontend_pricing_v2
frontend_integrations_v2
```

Порядок: local/integration tests → internal sandbox → selected personal Pro pilot →
selected Business pilot → percentage rollout → general availability. Rollback
admission не удаляет уже принятые items; workers продолжают по immutable snapshot.
Rollback plan revisions не уменьшает текущий open allowance epoch.

## 18. Тестовая матрица

### 18.1. Unit/property

- plan price/allowance/enterprise formula/rounding/overflow;
- entitlement resolution и отсутствие plan-code branching;
- destination validation truth table;
- folder exact-scope compatibility;
- idempotency canonical hash включает placement;
- HMAC signing/verification/time tolerance;
- rate and credit arithmetic без float;
- state-machine commands;
- redaction и safe errors.

### 18.2. PostgreSQL integration

- unique system folder при 100 concurrent transactions;
- cross-company/department/folder ACL denial;
- archived/deleted destination races;
- accepted item сохраняет immutable placement после connection update;
- retry/restart не дублирует call, usage, assignment или webhook event;
- allowance migration не меняет open epoch;
- unlimited member entitlement проходит все repository limits;
- indexes используются на production-shaped data через EXPLAIN.

### 18.3. HTTP contract

- sandbox/production key mismatch;
- каждый scope allow/deny;
- strict JSON unknown/duplicate/depth/size;
- URL and multipart happy path;
- missing folder → system `Внешняя`;
- invalid specified folder → error, без fallback;
- cursor pagination stable under concurrent inserts;
- webhook CRUD/test/delivery;
- export lifecycle and expired link;
- `Idempotency-Key` replay/conflict;
- 401/403/404 non-disclosure.

### 18.4. Security

- SSRF redirect/rebinding/private networks/cloud metadata;
- malicious media/container/zip/path traversal;
- CSV injection;
- secret leakage through logs, traces, errors, analytics, browser persistence;
- UUID enumeration and horizontal privilege escalation;
- replayed webhook and stale timestamp;
- rate-limit bypass across keys/IPs;
- revoked/rotated key race;
- oversized metadata and resource exhaustion.

### 18.5. Frontend/e2e/visual

- pricing values/capabilities from real API, no duplicated constants;
- Personal Pro and business API availability;
- unlimited business member text and no hidden UI cap;
- destination/folder cascading selection and preview;
- one-time key copy/toast/close safeguards;
- recent 10 imports → full paginated page;
- error/empty/loading/stale states;
- responsive geometry/theme/zoom/accessibility matrix;
- browser → API → DB → worker → call/folder → transcript/analysis → webhook;
- screenshots at agreed breakpoints compared without overlap/clipping.

## 19. Definition of Done

Функция не считается готовой только по build/unit tests. Требуется:

- approved product revision и OpenAPI v2;
- migrations applied and validated on production-shaped copy;
- backend unit/integration/security/load gates green;
- frontend typecheck/build/e2e/accessibility/visual gates green;
- authenticated end-to-end sandbox и production-like pilot;
- provider billing/credit reconciliation within approved tolerance;
- no duplicate calls/charges/folders under retries and concurrency;
- ACL tests prove personal/company/department isolation;
- all business exports and Personal Pro API verified;
- monitoring dashboards, alerts, runbooks and on-call ownership ready;
- rollback drill completed;
- documentation/examples match shipped payloads;
- exact deployed SHAs recorded for both repositories.

## 20. Решения, требующие отдельного утверждения до реализации

1. Personal Plus retention: в обсуждении выбран 365 дней; подтвердить, что прежние
   180 дней действительно повышаются.
2. Business Pro retention: сохранить уже реализованные 550 дней либо выбрать другое
   договорное значение; бесконечное хранение не подразумевается.
3. Разрешать ли company-level connection направлять запросом в любой department или
   создавать отдельный connection на отдел. Безопасный default — отдельный
   connection/fixed department, override включается явно.
4. Нужен ли `media:read`; по умолчанию audio download через API отсутствует.
5. Конкретные rate/concurrency/media-duration quotas определяются capacity tests,
   а не предположением в продуктовой таблице.
6. Gross-margin floor и annual discount Enterprise утверждаются после полной cost
   telemetry; текущая формула ограничивает markup, но не заменяет учёт расходов.
