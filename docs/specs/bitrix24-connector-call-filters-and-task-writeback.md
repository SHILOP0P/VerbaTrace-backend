# Спецификация: Bitrix24-коннектор, фильтры звонков и подтверждаемая запись задач

Статус: репозиторная реализация завершена; production DoD требует внешней приёмки
Дата: 2026-08-29
Владелец решения: VerbaTrace

## 0. Состояние реализации на 2026-08-31

- [x] Миграции connection/OAuth credentials, checkpoints, call candidates,
  backfill, action write-back и support-access с аудитом.
- [x] Company-only Bitrix24 connection, server-side OAuth state, шифрование
  токенов, refresh lease, health/capability test, pause/resume.
- [x] Event-assisted reconciliation, пагинация `voximplant.statistic.get`,
  delayed recording recovery, dedup и безопасная обработка recording URL.
- [x] Backfill preview/create/status worker с `Idempotency-Key`, lease, retry и
  сохранением связи операции с найденными кандидатами.
- [x] Фильтры звонков по реальному времени, длительности, сотруднику, отделу,
  источнику и состояниям; inline range validation, chips, light/dark/mobile UI.
- [x] Preview и approval создания задачи, запрет self-approval, durable states,
  ambiguous-outcome state без слепого повтора и UI рядом с action.
- [x] Support-access request/approve/deny/revoke/expiry, отдельное уведомление и
  понятный consent dialog с точным allowlist ресурсов и команд.
- [x] Bulk mapping preview/diff и одна audited bulk command.
- [x] Cursor pagination и URL persistence для нового UI фильтров, включая
  конкретную компанию, connection и folder (offset оставлен только как
  переходный механизм). Connection options строятся только по ACL-видимым calls.
- [x] Автоматическая сверка внешнего статуса/конфликтов Bitrix24 task и ручное
  разрешение `needs_review`.
- [x] Обязательная проверка support grant на старых admin endpoints чтения
  business-данных: списки ограничены grant subjects, object reads проверяют
  точный resource/subject, каждое обращение аудируется.
- [x] Локальный release gate: backend format/lint/unit/PostgreSQL integration/vet
  и Docker build, race для затронутых пакетов в Linux-контейнере, frontend
  TypeScript/Vite build. Фильтры проверены с real local backend в light/dark и
  responsive layout; активные параметры и поиск сохраняются в URL, browser
  console не содержит warning/error.
- [ ] Полный corporate fixture browser E2E для Bitrix24 mapping/action approval,
  автоматизированные accessibility/visual-regression и отдельный load/capacity
  gate. Успешная компиляция UI не считается заменой этих проверок.
- [ ] Real-portal pilot: OAuth/capabilities/import/recording/task должны быть
  проверены на настоящем тестовом Bitrix24. Без этого `connector_verified` не
  считается подтверждённым результатом поставки.
- [ ] Бесплатная промежуточная проверка на текущем портале: зарегистрировать и
  завершить синтетический внешний звонок через
  `telephony.externalCall.register`/`telephony.externalCall.finish`, убедиться,
  что он появился в статистике, найден backfill preview и импортирован без
  дублей. Этот шаг проверяет REST-контур, но не подтверждает реальную телефонию,
  передачу голоса или доступность аудиозаписи.
- [ ] Обязательная будущая приёмка: подключить реальный телефонный номер или
  SIP/REST-АТС, провести настоящий завершённый звонок, получить его через
  `voximplant.statistic.get`, импортировать доступную запись и только после
  остальных пунктов real-portal checklist разрешить `connector_verified`.

Операционный runbook и real-portal evidence checklist:
[Bitrix24 connector runbook](../runbooks/bitrix24-connector.md).

Связанные документы:

- [платформа интеграционного ingest](./integration-ingest-platform.md);
- [backend-контракт ingest](./integration-ingest-platform-backend.md);
- [frontend-контракт ingest](./integration-ingest-platform-frontend.md);
- [действия по итогам звонка](./call-actions-and-company-onboarding.md);
- [тарифы и Integration API v2](./pricing-plans-and-integration-api-v2.md);
- [retention звонков](./call-retention-and-instruction-history.md).

Этот документ дополняет общую платформу конкретным production-коннектором. При
конфликте общие инварианты безопасности, billing, retention, ACL и exactly-once
processing из связанных документов имеют приоритет. Изменение этих инвариантов
требует отдельного решения, а не скрытого исключения в адаптере Bitrix24.

## 1. Цель

Дать компании управляемый путь:

```text
Bitrix24 call → VerbaTrace ingest → транскрипция/анализ → внутреннее действие
→ подтверждение → задача Bitrix24 → наблюдаемый результат синхронизации
```

Одновременно улучшить поиск звонков по реальному времени разговора, длительности,
сотрудникам, отделам и источнику. Интерфейс должен продолжать существующую
информационную архитектуру VerbaTrace, а не выглядеть случайно добавленным модулем.

## 2. Проверенное исходное состояние

На дату документа в VerbaTrace уже есть:

- company/department/personal ACL звонков;
- роли `company_manager`, `department_leader`, `employee`;
- developer applications, connections, service accounts/API keys и scopes;
- sandbox/production ingest, `Idempotency-Key`, dedup, retry/cancel и audit;
- `occurred_at` в ingest item, но не отдельное время разговора в read model звонка;
- внутренние actions с assignee, department, deadline, evidence, transfer,
  reminders, `overdue` и append-only events;
- страница интеграций и базовые фильтры звонков: status, scope, uploader,
  `created_at` range, folder и поиск по title/original filename;
- светлая и тёмная темы через существующие semantic tokens.

Текущий generic ingest не является подтверждением реальной работы Bitrix24.
Статус `connector_verified` разрешён только после теста на настоящем портале.

## 3. Границы первой production-поставки

### 3.1. Входит

- company-scoped подключение Bitrix24 через полный server-side OAuth 2.0;
- один или несколько порталов на компанию без повторной привязки одного портала;
- получение истории звонков и recording URL через подтверждённые методы портала;
- event-assisted импорт плюс обязательная reconciliation-сверка;
- backfill за выбранный период с preview количества доступных записей;
- сопоставление Bitrix24 users с участниками и отделами VerbaTrace;
- маршрутизация звонка в company/department/folder и snapshot инструкций;
- внутреннее действие как источник истины;
- отдельный запрос и подтверждение создания задачи в Bitrix24;
- наблюдаемая one-way запись задачи и безопасная сверка её состояния;
- фильтры звонков по времени разговора, длительности, пользователю, отделу,
  компании, источнику, connection, folder, processing status и наличию действий;
- понятные light/dark, desktop/tablet/mobile, loading/empty/error/permission states;
- аудит, уведомления, health, retry, pause/reconnect/revoke и support-access flow.

### 3.2. Не входит без отдельного решения

- автоматическое изменение lead/deal/contact и стадий воронки;
- импорт чатов, email и произвольных CRM activities;
- массовая запись комментариев/транскриптов в CRM timeline;
- полная двусторонняя синхронизация всех полей actions/tasks;
- создание пользователей VerbaTrace или Bitrix24 без подтверждения;
- personal Bitrix24 connection;
- автоотправка задач сотрудниками без approval policy;
- обещание поддержки любого PBX, подключённого к Bitrix24;
- поддержка on-premise Bitrix24 до отдельного сетевого и version compatibility gate.

## 4. Неподвижные продуктовые решения

1. Connection принадлежит компании. Personal scope для Bitrix24 v1 запрещён.
2. Только `company_manager` запускает OAuth, меняет scopes/policy и отключает портал.
3. OAuth identity Bitrix24 и VerbaTrace service principal — разные credentials.
4. Frontend никогда не получает access token, refresh token или client secret.
5. `member_id`, а не portal domain, является стабильной identity портала.
6. Один active `member_id` нельзя привязать к двум компаниям VerbaTrace.
7. Event является сигналом, а не единственным источником истины; reconciliation
   обязательна из-за задержек, дублей и пропусков событий.
8. Внешний звонок создаёт не более одного VerbaTrace call и одной billable chain.
9. Неизвестный пользователь не создаётся и не сопоставляется автоматически по имени.
10. Внутренний action создаётся раньше внешней задачи и не зависит от доступности CRM.
11. Отправка задачи требует approval; requester не одобряет собственный запрос.
12. До real-portal проверки ambiguous timeout при `tasks.task.add` остаётся
    `reconciling`, а не слепо повторяется с риском дубля.
13. ACL применяется в SQL до фильтрации, сортировки и pagination.
14. Время хранится в UTC, входной offset сохраняется в provenance, отображение идёт
    в timezone пользователя.
15. Light и dark используют одну семантическую модель; отдельная случайная палитра
    Bitrix24-модуля запрещена.

## 5. Роли и доступ

| Возможность | Employee | Department leader | Company manager | Platform support |
|---|---:|---:|---:|---:|
| Видеть импортированный звонок | только по call ACL | свои отделы | компания | только по одобренной заявке |
| Видеть health connection | нет | read-only своего отдела | да | по заявке |
| Запустить OAuth/reconnect | нет | нет | да | нет |
| Менять глобальные import/write-back rules | нет | нет | да | по заявке с write scope |
| Сопоставлять пользователя | нет | только со своим отделом | вся компания | по заявке |
| Retry отдельного ingest item | нет | доступный отдел | да | по заявке |
| Начать backfill | нет | нет | да | нет |
| Запросить CRM task | автор/assignee доступного action | да | да | нет |
| Одобрить CRM task | нет | для target department | да | нет |
| Отключить/revoke/delete | нет | нет | да | нет |
| Читать OAuth/API secrets | нет | нет | нет | нет |

Frontend скрывает недоступные команды для ясности, но backend повторно проверяет
роль, active membership, company/department boundary и capability при каждой
команде. Потеря роли действует немедленно; открытая вкладка не сохраняет право.

### 5.1. Support-access request

Platform support не получает business-доступ из глобальной роли. Для диагностики
он создаёт заявку, содержащую:

- personal owner либо company и получателя `company_manager`;
- incident/request ID и понятную причину;
- точный набор объектов: connection, ingest items, calls или actions;
- scope `read` либо ограниченный перечень write-команд;
- срок начала и автоматического истечения;
- сведения, какие поля будут доступны; secrets всегда исключены.

Получатель видит notification с отдельной shield/admin emblem и может approve/deny.
Администратор не может одобрить свою заявку. Approval, denial, revoke, expiry и
каждое действие support записываются в append-only audit. Владелец может отозвать
доступ раньше срока. Для будущей personal-подписки заявку подтверждает сам владелец.

## 6. Контракт Bitrix24

### 6.1. Capability discovery

После OAuth backend проверяет, а не предполагает:

- portal identity и текущего OAuth user;
- выданные scopes;
- наличие нужных методов через `method.get`;
- право `Call Statistics — View` для `voximplant.statistic.get`;
- доступность `user.get` с минимальным `user_brief` либо более узким подходящим scope;
- доступность выбранной версии Tasks API;
- возможность подписки на нужные events;
- наличие записи на реальном завершённом звонке.

Минимальный кандидат scopes: `telephony`, `user_brief`, `tasks` для REST 3.0 и,
только если adapter реально читает CRM activities/bindings, `crm`. Старый `task`
не запрашивается одновременно с `tasks` без подтверждённой необходимости. Итоговый
набор фиксируется по фактически вызываемым methods и real-portal тесту.

### 6.2. Источник звонков

Primary reconciliation source — `voximplant.statistic.get`, потому что официальный
ответ содержит `ID`, `CALL_ID`, `EXTERNAL_CALL_ID`, `PORTAL_USER_ID`,
`CALL_START_DATE`, `CALL_DURATION`, `CALL_RECORD_URL`, result code и CRM binding IDs.

`onCrmActivityAdd/Update` может ускорять обнаружение: событие содержит activity ID,
после чего adapter получает объект отдельным запросом и связывает его с call
statistics. Event не заменяет polling/backfill и не считается media payload.

External identity:

```text
portal = member_id
external_call_id = "voximplant-statistic:" + statistic.ID
secondary IDs = CALL_ID, EXTERNAL_CALL_ID, CRM_ACTIVITY_ID
```

Display name, phone, domain и recording URL не используются как ключ дедупликации.

### 6.3. Ограничения API

Adapter имеет per-portal queue/rate limiter и учитывает vendor response time,
`QUERY_LIMIT_EXCEEDED`, 429/503 и `Retry-After`, если он возвращён. Значения лимитов
не зашиваются как универсальные: они зависят от тарифа и могут делиться между
приложениями с одного IP. List requests используют pagination, минимальный `select`
и bounded batch только после нагрузочной проверки.

## 7. Модель данных

### 7.1. Расширение connection

Provider settings versioned JSON содержит только несекретные значения:

```text
portal_member_id
portal_domain_display
oauth_user_external_id
default_destination
default_folder_uuid
instruction_mode
import_policy
writeback_policy
reconciliation_policy
provider_contract_version
```

Токены хранятся отдельно в `integration_oauth_credentials`: ciphertext, nonce,
key_version, expires_at, refresh status и timestamps. Plaintext запрещён в DB,
logs, audit, events, frontend state и error monitoring.

Constraints:

- unique active `(provider, portal_member_id)`;
- connection company обязана совпадать с destination company;
- optimistic `lock_version` для settings;
- OAuth credential не существует без connection и удаляется при revoke.

### 7.2. External identity mappings

`integration_external_user_mappings`:

```text
connection_uuid
external_user_id
internal_user_uuid nullable
department_uuid nullable
status: unmapped|mapped|conflict|inactive|ignored
external_display_snapshot
external_active
mapped_by_user_uuid
mapped_at
lock_version
```

Unique `(connection_uuid, external_user_id)`. Email/phone/name могут предлагать
candidate, но решение всегда подтверждается человеком. Полные контактные данные не
сохраняются, если для сценария достаточно external ID и display name.

### 7.3. Checkpoints и raw events

Checkpoint хранит `(CALL_START_DATE, statistic.ID)`, overlap window, last successful
reconciliation и contract version. Raw event хранится за ограниченный срок,
зашифрован или redacted согласно data classification. Advance watermark происходит
только после durable сохранения всех элементов страницы.

### 7.4. Call read model

В `calls` добавляется nullable `occurred_at`. Для импортированного звонка это
Bitrix24 `CALL_START_DATE`; для ручной загрузки значение отсутствует. API возвращает:

```text
occurred_at nullable
display_time
time_source: source|upload_fallback
source_provider nullable
connection_uuid nullable
external_call_id nullable
imported_at nullable
```

`display_time = occurred_at`, а при его отсутствии — `created_at` с явным
`time_source=upload_fallback`. UI не выдаёт время загрузки за подтверждённое время
разговора.

Индексы проектируются под ACL-first query:

- `(company_uuid, occurred_at DESC, call_uuid)` where `occurred_at IS NOT NULL`;
- `(department_uuid, occurred_at DESC, call_uuid)`;
- ingest provenance `(connection_uuid, occurred_at DESC, ingest_item_uuid)`;
- mapped participant `(internal_user_uuid, call_uuid)`;
- partial indexes по operational status после `EXPLAIN` на pilot × 10.

### 7.5. Task sync

`call_action_external_syncs`:

```text
sync_uuid
action_uuid
connection_uuid
provider
requester_user_uuid
approver_user_uuid nullable
state
external_task_id nullable
external_task_url nullable
idempotency_marker
request_payload_hash
last_confirmed_external_version nullable
attempts/available_at/error_code
lock_version
created_at/approved_at/synced_at
```

Unique active `(action_uuid, connection_uuid, operation=create_task)`. Повторная
кнопка возвращает существующий sync, а не создаёт новый.

### 7.6. Support access

Shared entities: `support_access_requests`, `support_access_grants` и
`support_access_events`. Grant имеет subject, resource allowlist, commands allowlist,
valid_from/expires_at и revoke_at. Проверка grant выполняется в runtime, не только
при открытии UI.

## 8. OAuth и lifecycle connection

1. Manager создаёт draft с company/destination и получает одноразовый flow UUID.
2. Backend формирует signed state с TTL и exact redirect URI.
3. Browser переходит в Bitrix24; token/code не проходят через frontend state.
4. Callback атомарно consumes state, повторно проверяет manager membership и меняет
   code на tokens server-to-server.
5. Сохраняются `member_id`, domain display и encrypted credentials.
6. Выполняется capability/permission health check.
7. Manager видит mapping/import/write-back preview и запускает test.
8. Только после успешного test connection становится `active`.

Состояния:

```text
draft → authorizing → testing → active ↔ degraded → paused → revoked
                          └──── reconnect_required ────┘
```

Обязательные edge cases:

- popup blocked/closed, full-page fallback, user cancel и expired state;
- callback replay и два одновременных callback;
- manager потерял роль между start и callback;
- тот же portal уже подключён к другой company;
- portal domain изменился, но `member_id` тот же;
- refresh token expired/revoked, refresh stampede и KMS outage;
- приложение удалено из Bitrix24;
- scopes уменьшены или OAuth user потерял доступ к call statistics/tasks;
- connection создан, но test не завершён;
- revoke во время fetch/write-back;
- optimistic conflict при двух открытых вкладках.

Pause прекращает новые claims и reconciliation, но не удаляет данные. Manager явно
выбирает policy для already committed items: continue, pause или cancel. Revoke
отзывает tokens, event subscriptions и machine credentials; созданные calls/actions
не удаляются. Erasure — отдельная подтверждаемая операция по retention inventory.

## 9. Импорт звонков

### 9.1. Normal flow

1. Event либо reconciler обнаруживает statistic record.
2. Raw fact и normalized candidate сохраняются durable до внешней загрузки.
3. Проверяются dedup, mapping, destination snapshot, policy и credit admission.
4. Recording скачивается backend-адаптером с vendor auth и resource limits.
5. Media проходит quarantine, type/size/duration/malware validation.
6. Generic ingest создаёт call через существующий `CallService.CreateCall`.
7. `occurred_at`, external provenance и mapping snapshot сохраняются атомарно.
8. Processing/analysis продолжаются общей очередью и billing contract.
9. Completion публикует outbox event; UI получает подтверждённый status.

### 9.2. Import policy

Manager настраивает:

- входящие/исходящие/all;
- импортировать ли missed/failed calls без media — default `нет`;
- минимальную/максимальную duration;
- какие Bitrix24 users/отделы включены;
- default destination/folder/instructions;
- поведение unmapped user: quarantine или route to reviewed company folder;
- backfill range и budget preview;
- обработку уже существующих звонков при изменении mapping.

Политика versioned. Каждый ingest item хранит snapshot; изменение не перемещает уже
созданные calls без отдельной audited bulk command.

### 9.3. Delayed/missing recording

- Пустой `CALL_RECORD_URL` не означает permanent failure сразу: candidate получает
  `waiting_for_recording` и повторно сверяется в bounded grace window.
- Число попыток и длительность grace определяются real-portal измерением до pilot.
- Missed/zero-duration/failed call без записи становится `skipped_no_media`, а не
  красной системной ошибкой.
- Если URL появился позднее, тот же external call продолжает исходный item.
- Изменившийся URL не создаёт новый call; payload hash change аудируется.
- 401/403 URL после refresh проверяются один раз с новым token; бесконечный retry
  запрещён.

### 9.4. Reconciliation/backfill

- сортировка `(CALL_START_DATE ASC, ID ASC)` с overlap window;
- checkpoint продвигается после commit полной страницы;
- duplicate/out-of-order pages безопасны;
- backfill имеет preview, budget/admission limit, pause/resume/cancel и progress;
- новый live event не ждёт завершения большого backfill: отдельная fair queue;
- один шумный portal не занимает общий worker pool;
- deleted/edited statistics record не удаляет call автоматически;
- повторный backfill использует тот же dedup и billing identity.

## 10. Сопоставление пользователей и отделов

Wizard показывает две колонки: Bitrix24 user и VerbaTrace user/department. Candidate
может предлагаться по verified email либо точному username только как подсказка.

Правила:

- 0 candidates → `unmapped`;
- 1 candidate → предложить, но не активировать без confirmation;
- несколько candidates → `conflict`, обязательный ручной выбор;
- один external user не маппится на два internal users;
- один internal user может иметь external IDs в разных portals;
- external inactive/terminated не удаляет historical mapping;
- internal suspended/left блокирует новый route/task assignment;
- user с несколькими departments требует выбора default destination;
- leader может выбрать только member своего department;
- cross-department mapping и конфликт лидеров решает manager;
- bulk mapping требует preview diff и одной audited command.

## 11. Фильтры звонков

### 11.1. Семантика

Фильтры работают только по доступному пользователю набору звонков:

- employee — personal и разрешённые company/department calls;
- department leader — тот же ACL, без автоматического доступа к чужим отделам;
- company manager — company calls по существующим правилам;
- support — только resource allowlist действующего grant.

SQL сначала применяет `visibleToUserCondition`, затем filters, stable order и cursor.
Filter options строятся тем же predicate и не раскрывают names/counts чужих сущностей.

### 11.2. API filters

```text
q
status (repeatable)
scope (repeatable)
company_uuid
department_uuid (repeatable)
participant_user_uuid (repeatable)
uploaded_by_user_uuid (legacy/manual upload)
folder_uuid (repeatable)
source_provider (manual|generic_api|bitrix24)
connection_uuid (repeatable)
occurred_from / occurred_to
imported_from / imported_to
duration_min_seconds / duration_max_seconds
has_analysis / has_actions / has_processing_error
favorite_only
sort (occurred_at|created_at|duration)
order (asc|desc)
cursor / limit
```

Ranges используют `[from, to)`. `from > to`, отрицательная duration, слишком
большой limit, неизвестный enum/UUID и несовместимые company/department дают stable
`400 invalid_call_filter`. Недоступный UUID возвращает non-disclosing empty/404 по
утверждённому контракту, но не подтверждает существование чужой сущности.

Старые `from/to` временно продолжают означать `created_at`; frontend переходит на
явные поля. Offset поддерживается на transition period, новый UI использует cursor
по `(sort_time, call_uuid)`, чтобы вставка новых звонков не дублировала страницы.

### 11.3. Timezone и даты

- server принимает ISO-8601 с offset и нормализует в UTC;
- date-only preset рассчитывается backend/client shared contract в timezone user;
- DST день может иметь 23/25 часов и не обрезается фиксированным `24h`;
- custom time-of-day применяется после timezone conversion;
- future range допустим, но возвращает empty;
- manual upload с неизвестным occurred time включается только при явном
  `include_upload_fallback=true` либо в default mixed view с понятной маркировкой;
- UI отдельно подписывает «Время разговора» и «Импортирован».

## 12. Создание задачи Bitrix24

### 12.1. Approval flow

1. Автор или assignee action нажимает «Отправить в Bitrix24».
2. Preview показывает portal, task title, assignee mapping, deadline, CRM binding,
   evidence link и поля, которые будут переданы.
3. Создаётся immutable sync request.
4. Target department leader либо company manager approve/reject; requester не может
   одобрить собственный request даже при наличии второй роли.
5. Worker повторно проверяет action version, membership, mapping, connection health
   и external permissions непосредственно перед вызовом.
6. После подтверждённого ответа сохраняются task ID/link и audit event.

Company policy `employee_direct_writeback` в первой поставке всегда `false`.

### 12.2. Передаваемые поля

- title;
- description с кратким context и безопасной ссылкой на VerbaTrace action/call;
- responsible user external ID;
- creator OAuth user либо иной подтверждённый policy actor;
- deadline в ISO-8601 с offset portal/user policy;
- optional CRM binding из подтверждённого source provenance;
- уникальный correlation marker, если поддерживаемое поле проверено на портале.

Полный transcript, raw recording URL, секреты и скрытые analysis fields не передаются.
Evidence link требует обычной авторизации VerbaTrace и не становится public share.

### 12.3. Idempotency и ambiguous outcome

Bitrix24 API не считается поддерживающим VerbaTrace `Idempotency-Key`, пока это не
подтверждено. Поэтому:

- unique DB sync reservation создаётся до внешнего вызова;
- один worker владеет lease; parallel approve/send возвращают тот же sync;
- request содержит stable correlation marker;
- timeout после отправки переводит sync в `reconciling`, не в немедленный retry;
- reconciler ищет подтверждённый task по marker/доступному search contract;
- если безопасный поиск marker не подтверждён, UI просит manager выбрать найденную
  задачу либо явно разрешить повтор с предупреждением о риске дубля;
- только подтверждённый task ID переводит state в `synced`.

### 12.4. Ownership и конфликты

В первой версии VerbaTrace владеет action; Bitrix24 task является внешним
представлением. Автоматически допускается читать внешний status. Изменения assignee,
deadline или title в Bitrix24 не перезаписывают action молча: появляется conflict с
вариантами «принять Bitrix24», «вернуть данные VerbaTrace» или «разорвать связь».

External completion может завершить action только после проверки task ID, mapped
actor и policy; иначе создаётся review notification. Удаление task не удаляет action.
Отмена action не удаляет task автоматически — manager выбирает close/unlink.

## 13. Backend API

Management session API:

```text
POST   /api/v1/integrations/bitrix24/oauth/start
GET    /api/v1/integrations/bitrix24/oauth/callback
POST   /api/v1/integrations/{id}/test
GET    /api/v1/integrations/{id}/health
GET    /api/v1/integrations/{id}/external-users
PATCH  /api/v1/integrations/{id}/external-user-mappings/{external_id}
POST   /api/v1/integrations/{id}/mapping-preview
POST   /api/v1/integrations/{id}/external-user-mappings/bulk
POST   /api/v1/integrations/{id}/backfills/preview
POST   /api/v1/integrations/{id}/backfills
GET    /api/v1/integrations/{id}/backfills
GET    /api/v1/integrations/{id}/backfills/{backfill_id}
POST   /api/v1/integrations/{id}/reconnect
POST   /api/v1/integrations/{id}/pause
POST   /api/v1/integrations/{id}/resume
DELETE /api/v1/integrations/{id}

POST   /api/v1/actions/{action_id}/external-sync-requests
POST   /api/v1/action-external-sync-requests/{id}/approve
POST   /api/v1/action-external-sync-requests/{id}/reject
POST   /api/v1/action-external-sync-requests/{id}/resolve
GET    /api/v1/actions/{action_id}/external-sync
GET    /api/v1/action-external-sync-requests/{id}

POST   /api/v1/support-access-requests
POST   /api/v1/support-access-requests/{id}/approve
POST   /api/v1/support-access-requests/{id}/deny
POST   /api/v1/support-access-grants/{id}/revoke
```

Vendor callback/event endpoints имеют отдельный body limit, rate limit, request ID,
application token verification и не используют browser session. Event handler быстро
делает durable commit и отвечает; network/media/Bitrix API calls выполняются worker.

Commands используют `Idempotency-Key`; update/approve/reject требуют `If-Match` либо
expected `lock_version`. Error envelope остаётся stable и redacted.

## 14. Ошибки и состояния

| Code | Категория | UI-действие |
|---|---|---|
| `bitrix_oauth_cancelled` | user | повторить подключение |
| `bitrix_oauth_state_expired` | user | начать новый flow |
| `bitrix_portal_already_connected` | conflict | показать владельца без утечки чужой company |
| `bitrix_scope_missing` | configuration | reconnect с нужным scope |
| `bitrix_call_statistics_forbidden` | permission | выдать право в Bitrix24 |
| `bitrix_recording_pending` | transient | показать следующую сверку |
| `bitrix_recording_unavailable` | skipped/permanent | объяснить отсутствие записи |
| `bitrix_rate_limited` | transient | автоматический retry/time |
| `bitrix_portal_unavailable` | transient | circuit breaker/retry |
| `bitrix_user_unmapped` | operator | открыть mapping |
| `bitrix_task_permission_denied` | permission | исправить OAuth user/task rights |
| `bitrix_task_outcome_unknown` | reconciliation | не повторять вслепую |
| `integration_destination_removed` | blocked | выбрать новую destination |
| `integration_insufficient_credits` | blocked | manager пополняет/меняет тариф |
| `support_access_required` | permission | создать/ожидать заявку |

UI не парсит message и не показывает raw vendor response. Неизвестная ошибка имеет
request ID и безопасный текст без token, phone, URL query и payload.

## 15. Frontend information architecture

Новый модуль не создаёт отдельную верхнеуровневую навигацию. Он встраивается в уже
существующий путь:

```text
Настройки → Интеграции → Каталог источников → Bitrix24
Настройки → Интеграции → {connection} → Обзор / Сопоставление / Импорты / Действия / Аудит
Звонки → существующая панель фильтров + «Все фильтры»
Звонок → блок «Источник»
Действие → блок «Синхронизация с Bitrix24»
Уведомления → запросы write-back и support access
```

Так connection configuration остаётся в Settings, поиск звонков — в Calls, а
результат внешней задачи — рядом с соответствующим action. Дублирующие dashboard и
случайные кнопки в несвязанных экранах запрещены.

### 15.1. Каталог и connection detail

Bitrix24 card показывает назначение, состояние `Не подключено/Требует внимания/
Работает`, необходимую роль и primary action. После подключения открывается detail:

- **Обзор:** portal, health, last event/import/reconciliation, queue/errors;
- **Сопоставление:** unmapped/conflict/mapped users, department routing;
- **Импорты:** timeline, backfill, retry/cancel, deep link в call;
- **Действия:** write-back policy, pending approvals, sync conflicts;
- **Аудит:** actor, operation, object, time, result; без secrets.

OAuth wizard:

1. portal и объяснение permissions;
2. destination/routing;
3. OAuth;
4. capability result;
5. user mapping;
6. import/write-back preview;
7. test и activation.

Step state хранится backend; refresh/back не теряет безопасный прогресс. Ошибка шага
не очищает успешно сохранённые предыдущие значения.

### 15.2. Фильтры звонков без перегрузки

Текущую узкую sidebar нельзя заполнять десятью select подряд. Основная строка:

- search;
- кнопка периода с текущим summary;
- кнопка «Фильтры» с числом активных;
- reset только при наличии изменений.

Под строкой — removable chips активных фильтров. Расширенные фильтры открываются:

- desktop: anchored popover/drawer достаточной ширины;
- tablet/mobile: bottom sheet/full-height drawer;
- keyboard: dialog semantics, focus trap, `Esc`, restore focus.

Группы: «Время», «Организация», «Источник», «Обработка». Department options зависят
от company, users — от доступных departments, connection — от provider. Сброс parent
не молча сохраняет несовместимый child: UI удаляет его и сообщает об изменении.

Preset: Сегодня, Вчера, 7 дней, 30 дней, Свой период. Custom range показывает
timezone. Duration имеет понятные presets и два числовых поля; invalid range
проверяется inline. URL хранит filters, cursor сбрасывается при изменении query.

Call row показывает primary time, duration и маленький source badge. Imported time
отображается вторично только в details/tooltip. Badge не вытесняет title/status и не
создаёт горизонтальный overflow.

### 15.3. Action write-back UX

В существующей странице action расположен один логический card:

- состояние connection;
- preview task fields;
- requester/approver и reason;
- primary command по capability;
- sync timeline;
- confirmed external link;
- error/reconcile/conflict actions.

Нельзя показывать «Создано», пока backend не сохранил confirmed task ID. Loading
после timeout становится «Проверяем результат», а не возвращает активную кнопку.

### 15.4. Уведомления

- write-back approval: task/check emblem, action/department/deadline summary;
- mapping conflict: users emblem;
- connection degraded: plug/warning emblem;
- support access: отдельная shield/admin emblem, scope и expiry;
- сообщения доступны текстом, не различаются только цветом;
- notification deep link проверяет ACL после перехода.

## 16. Light/dark, accessibility и responsive gate

Все новые компоненты используют существующие tokens: `surface`, `text`, `muted`,
`border`, `accent`, `danger`, `focus` и их уже принятые производные. Hard-coded
background/text для одной темы запрещены; Bitrix brand color допустим только как
необязательный accent с проверенным контрастом.

Обязательная visual matrix:

- themes: light, dark, system;
- widths: 360, 390, 768, 1024, 1280, 1440 px;
- zoom 200%, long Russian/English names, long domain/ID, empty and 100+ items;
- states: loading, refreshing, empty, partial, error, permission denied, disabled,
  reconnect, approval pending, ambiguous outcome и conflict;
- no horizontal document overflow, clipping, overlap и layout jumps;
- touch target минимум 44×44 px;
- WCAG AA contrast, visible focus, reduced motion;
- status имеет icon + text; chart/color-only semantics запрещены;
- skeleton повторяет финальную geometry;
- table превращается в cards, а не в горизонтально обрезанный desktop table;
- screenshot regression дополняется ручной оптической проверкой обеих тем.

## 17. Security, privacy и надёжность

- OAuth state: signed, one-time, short TTL, bound to user/company/connection/session;
- exact redirect allowlist, `return_to` только внутренний route ID;
- tokens AEAD/KMS, rotation, no plaintext fallback;
- application token Bitrix24 events проверяется constant-time;
- event/body/media limits до parse/allocate;
- raw vendor HTML/Markdown никогда не рендерится без allowlist sanitizer;
- external URLs формирует/валидирует backend; `https`, `noopener,noreferrer`;
- recording URL не попадает в frontend и generic logs;
- phone/email минимизируются, export защищён от spreadsheet formula injection;
- service principal не impersonates employee;
- retry exactly-once связан с ingest/billing operation;
- disable/revoke имеет server cut-off и deterministic in-flight policy;
- support grant проверяется на каждый request и не раскрывает secrets;
- retention inventory охватывает tokens, raw events, mappings, checkpoints, syncs,
  task links, support requests/grants/events и backups;
- delete/revoke не удаляет исторические calls/actions без отдельного erasure flow.

## 18. Concurrency, operations и observability

- single-flight token refresh на connection;
- small `SKIP LOCKED` batches для event/import/write-back/reconciliation workers;
- per-portal fair queue, limiter и circuit breaker;
- отдельные pools для Bitrix API, media fetch, ingest и task write-back;
- leases/heartbeat/reclaim, graceful shutdown и idempotent cleanup;
- no network call в долгой DB transaction;
- queue age/depth, last success, retry count, reconciliation lag, unmapped count,
  duplicate alarm, task sync unknown/conflict, OAuth refresh и vendor rate-limit;
- metrics bounded-cardinality; company/external IDs только logs/traces с redaction;
- health одного portal не делает весь VerbaTrace unready;
- runbook: OAuth expiry, scope loss, queue growth, portal outage, missed event,
  duplicate suspicion, ambiguous task, mapping conflict, KMS/storage/billing failure;
- численные SLO/RPO/RTO и capacity утверждаются до pilot по измерениям, не
  придумываются в коде.

## 19. Тестовая матрица и corner cases

### 19.1. Auth/ACL

- employee/leader не запускает OAuth и не читает settings/secrets;
- leader видит только доступный department health/items;
- manager другой company получает non-disclosing отказ;
- роль отозвана между page load и mutation/callback;
- support без grant, expired/revoked grant и command вне allowlist;
- support self-approval невозможен; owner revoke действует немедленно;
- approved read grant не разрешает write;
- frontend-hidden command отклоняется прямым HTTP request.

### 19.2. OAuth/vendor

- success/cancel/denied/expired/replayed state;
- popup blocked/closed и full-page fallback;
- duplicate callback и concurrent reconnect;
- wrong domain, changed domain/same member ID, same portal/other company;
- token refresh race, invalid_grant, app uninstall, scope reduction;
- cloud/on-premise network differences;
- call statistics permission absent;
- methods/scopes differ from expected contract version;
- 429/503/timeout/malformed JSON/partial page/slow response.

### 19.3. Import/media

- duplicate/out-of-order event, event before recording, no event;
- same statistic ID/different payload, new event/same call, same media/different call;
- no recording, delayed recording, expired URL, redirect/auth loss;
- missed/zero-duration/failed call according to policy;
- corrupt/unsupported/oversized/decompression bomb/malicious media;
- crash before/after event commit, fetch, CreateCall, billing reserve/settle/outbox;
- destination/folder/user removed during processing;
- credits exhausted and resume after funding;
- pause/revoke while queued/fetching/processing;
- concurrent live event and backfill produce one call/charge;
- checkpoint crash mid-page and overlap replay.

### 19.4. Mapping

- 0/1/N candidates;
- same names, changed email/name, external inactive, internal suspended;
- user in 0/1/N departments;
- two leaders update mapping concurrently;
- cross-company UUID injection;
- bulk preview differs before commit;
- history remains readable after remap.

### 19.5. Filters

- employee/leader/manager visible sets before and after every filter;
- multiple departments/users/statuses and empty intersections;
- occurred/imported range, exact boundary `[from,to)`, future and invalid range;
- DST transition, different user timezone, missing `occurred_at` fallback;
- duration 0, min=max, negative/overflow;
- deleted folder/connection/user and stale URL query;
- new call inserted between pages does not duplicate/skip cursor results;
- unauthorized options/counts do not leak;
- 10k+ calls `EXPLAIN` uses intended indexes;
- abort stale search and rapid filter changes.

### 19.6. Task write-back

- requester cannot self-approve;
- wrong department leader and removed approver role;
- unmapped/inactive assignee, past deadline, closed/cancelled action;
- duplicate clicks/approve, worker crash and parallel workers;
- success, explicit vendor error, timeout before send, timeout after remote commit;
- correlation found/not found/multiple candidates;
- external task changed/completed/deleted/reassigned;
- action changed between approval and send;
- connection paused/revoked or permission lost before send;
- external link host/path validation.

### 19.7. Frontend/visual

- component tests всех capability/state combinations;
- browser E2E с real backend: connect emulator, mapping, import, filters, action approval;
- real portal checklist отдельно от CI;
- keyboard-only, screen reader labels/live regions, focus restore;
- light/dark/system and reduced motion;
- visual matrix widths/zoom/long content/no overlap;
- refresh/back/deep link/workspace switch/logout очищают privileged state;
- timeout/partial response не показывает ложный success.

## 20. Rollout

1. Schema/API behind flags; workers выключены.
2. Call read model и filters для manual/generic data; ACL/performance/browser gates.
3. Bitrix contract emulator: OAuth, calls, delayed media, limits, tasks, events.
4. Internal company с synthetic portal fixtures и billing limits.
5. Настоящий Bitrix24 test portal: OAuth, permissions, one call, recording,
   backfill, task add, ambiguous-timeout experiment и uninstall/reconnect.
6. Один pilot tenant, manual write-back approval, low queue/budget limits.
7. Наблюдение duplicate rate, reconciliation lag, mapping burden, failures и UX.
8. Staged companies; provider/global kill switch сохраняется.

Rollback выключает новые events/claims/write-back, но не удаляет calls/actions и не
ломает чтение provenance. Старый binary остаётся совместим с forward migration.

## 21. Definition of Done

- все роли и support grants проверены backend tests и прямыми forbidden requests;
- OAuth install/refresh/reconnect/revoke/uninstall проверены на реальном портале;
- real `voximplant.statistic.get` возвращает запись, импорт создаёт один call;
- event loss восстанавливается reconciliation без дубля и повторной оплаты;
- backfill resumable, budgeted, pausable и наблюдаемый;
- mapping не создаёт пользователя и не пересекает company boundary;
- фильтры используют occurred/imported time корректно, ACL-first и cursor-stable;
- task write-back не врёт об успехе и безопасно обрабатывает ambiguous timeout;
- UI логично встроен в Settings/Calls/Action/Notifications;
- light/dark, responsive, accessibility и visual regression gates пройдены;
- secrets отсутствуют в frontend/log/audit/error/analytics fixtures;
- migrations, unit/property/fuzz/PostgreSQL integration/race/contract/E2E/load,
  lint, frontend build и Docker gate зелёные;
- dashboard, alerts, runbook, retention, backup/restore и kill switch подтверждены;
- `connector_verified` выставлен только после сохранённых результатов real-portal
  checklist. Mock/emulator не считается этим доказательством.

## 22. Решения Дмитрия до начала реализации

1. Разрешать ли несколько Bitrix24 portals одной компании — рекомендация: да.
2. Первый source: только встроенная телефония/statistics либо конкретный внешний PBX.
3. Missing user: quarantine или reviewed company folder — рекомендация: quarantine.
4. Backfill presets/максимальный период и credit preview policy.
5. Кто approve write-back при отсутствии department leader — рекомендация: manager.
6. Можно ли external completion автоматически завершать action — рекомендация:
   только после mapped actor/policy check, иначе review.
7. Срок support grant — рекомендация: минимально необходимый, hard maximum 24 часа.
8. Marketplace application или local application для первого pilot.
9. Поддержка on-premise Bitrix24 — рекомендация: отдельный более поздний этап.
10. Сроки хранения raw vendor events и audit по юридической политике.

## 23. Официальные источники Bitrix24

- [полный OAuth 2.0](https://apidocs.bitrix24.com/settings/oauth/index.html);
- [scopes и права пользователей](https://apidocs.bitrix24.com/api-reference/scopes/index.html);
- [доступные scopes](https://apidocs.bitrix24.com/api-reference/scopes/permissions.html);
- [история звонков `voximplant.statistic.get`](https://apidocs.bitrix24.com/api-reference/telephony/voximplant/voximplant-statistic-get.html);
- [CRM activity events](https://apidocs.bitrix24.com/api-reference/crm/timeline/activities/events/index.html);
- [безопасность event handlers](https://apidocs.bitrix24.com/api-reference/events/safe-event-handlers.html);
- [получение пользователей](https://apidocs.bitrix24.com/api-reference/user/user-get.html);
- [Tasks API](https://apidocs.bitrix24.com/api-reference/tasks/index.html);
- [создание task REST 3.0](https://apidocs.bitrix24.com/api-reference/tasks/tasks-task-add-rest-v3.html);
- [task events](https://apidocs.bitrix24.com/api-reference/tasks/events-tasks/index.html);
- [REST limits](https://apidocs.bitrix24.com/limits.html).

Источники подтверждают отдельные методы и ограничения, но не подтверждают работу
полного VerbaTrace flow на конкретном тарифе/портале. Это подтверждает только
реальный connector checklist из Definition of Done.
