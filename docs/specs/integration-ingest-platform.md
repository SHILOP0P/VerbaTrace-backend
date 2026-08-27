# Спецификация: платформа автоматического поступления звонков и интеграций

Статус: production design, реализация не начата
Дата: 2026-08-22
Backend: `C:\projects\VerbaTrace\Monolit`
Frontend: `C:\projects\VerbaTrace-frontend`
Связанные документы:

- [Backend-контракт](./integration-ingest-platform-backend.md)
- [Frontend-контракт](./integration-ingest-platform-frontend.md)
- [Кредитный биллинг, developer platform и поэтапная поставка](./credit-billing-developer-platform-rollout.md)

## 1. Цель

VerbaTrace должен принимать разговоры автоматически из внешних систем и проводить
их через существующие транскрипцию и анализ без дублей, потери данных, обхода ACL
или повторного списания кредитов.

Целевой поток:

`источник → аутентифицированное событие → durable ingest → получение media →
создание звонка → транскрипция → анализ → подписанный webhook → наблюдаемый итог`.

Это не «endpoint загрузки по URL» и не отдельная логика для каждой CRM. Поставка
создаёт общий integration domain. Generic Ingest API является первым каналом;
Bitrix24 — первым адаптером после подтверждения общего контракта; amoCRM и телефония
используют то же ядро.

## 2. Проверенное исходное состояние

На момент написания:

- основной путь создания звонка — синхронная multipart-загрузка пользователя;
- `CallService.CreateCall` уже централизует media storage, ffprobe, ACL, billing и
  постановку транскрипции;
- `processing_jobs` поддерживает PostgreSQL claim через `FOR UPDATE SKIP LOCKED`,
  lease, retries и восстановление зависшей задачи;
- объект действия, evidence, уведомления, Human QA и retention уже имеют отдельные
  lifecycle;
- в backend не найдено публичного ingest API, service accounts, CRM/telephony
  connectors, integration health или исходящих webhooks;
- production использует абстракцию `AudioStorage`, но текущая сборка конфигурирует
  локальную реализацию; горизонтальный runtime потребует shared object storage;
- backend и frontend являются разными репозиториями. Готовность одной стороны не
  означает готовность всей функции.

## 3. Пользовательский результат

Менеджер компании подключает источник, выбирает отдел, папку и набор правил.
После этого звонки появляются без ручной загрузки. Он видит:

- состояние подключения и последнюю успешную синхронизацию;
- каждый принятый, повторный, обрабатываемый и отклонённый импорт;
- безопасную причину ошибки и допустимое действие;
- происхождение звонка и deep link во внешнюю систему;
- историю изменения подключения, ключей и ручных повторов.

Повтор одного события, timeout клиента, рестарт worker или временная ошибка CRM не
создают второй звонок и не расходуют кредиты повторно.

## 4. Границы поставки

### 4.1. Входит в платформу

- company-scoped integration connections;
- VerbaTrace service accounts/API keys с scopes, expiry, rotation и revoke;
- JSON Ingest API для media URL и отдельный потоковый upload API;
- durable event journal и ingest item lifecycle;
- многоуровневая идемпотентность и transactional outbox;
- SSRF-safe downloader, media validation и лимиты;
- status/retry/cancel для операторов с RBAC;
- исходящие HMAC-подписанные webhooks;
- append-only audit и operational diagnostics;
- integration health, metrics, alerts и runbook;
- frontend управления подключениями и импортами;
- generic connector emulator для CI/E2E;
- Bitrix24 adapter после подтверждения API на тестовом портале;
- совместимость с retention, billing, folders, instructions и analysis snapshots.

### 4.2. Не входит в первую поставку

- amoCRM и telephony production adapters;
- автоматическая запись действий обратно в CRM без preview и подтверждения;
- marketplace-публикация;
- построение собственной телефонии/CCaaS;
- перенос media через browser пользователя;
- автоматическое слияние разных звонков только из-за одинакового media hash;
- Kafka или иной новый broker без измеренной необходимости;
- обещание поддержки CRM-метода, не проверенного на официальной документации и
  реальном тестовом аккаунте.

Отсутствующее явно показывается как `not_supported`, а не маскируется пустым UI.

## 5. Термины

- **Connection** — настройка одного внешнего источника в компании.
- **Service account** — техническая identity VerbaTrace, не пользовательская сессия.
- **Ingest event** — неизменяемый факт получения внешнего события.
- **Ingest item** — один импортируемый разговор и его lifecycle.
- **Connector adapter** — преобразование vendor contract в нормализованный ingest.
- **Delivery** — попытка отправить исходящий webhook.
- **Reconciliation** — периодическая сверка с источником для поиска пропущенных
  событий.
- **External identity** — стабильные ID портала, разговора, участника и сделки без
  предположения, что display name уникален.

## 6. Главные инварианты

1. Connection принадлежит ровно одной компании; personal ingest запрещён.
2. Только активный `company_manager` создаёт connection и управляет секретами.
3. Department/folder принадлежат той же компании и доступны техническому actor.
4. Backend является единственным источником ACL, состояния и полномочий.
5. `(connection_uuid, external_call_id)` создаёт не более одного call.
6. Один `Idempotency-Key` с тем же payload возвращает прежний результат; с другим
   payload — стабильный `409 idempotency_conflict`.
7. Event сохраняется до начала внешней загрузки. Успешный HTTP-ответ не выдаётся до
   durable commit.
8. Создание call, usage и processing job использует существующий доменный путь либо
   эквивалентную атомарную команду; параллельная упрощённая вставка запрещена.
9. Внешние вызовы не выполняются внутри долгой DB-транзакции.
10. Retry не создаёт второй call, не повторяет usage и не теряет audit chronology.
11. Ошибки разделяются на retryable, permanent и operator-action-required.
12. Секреты, presigned URL, Authorization и полный чувствительный payload не
    попадают в логи, audit, tracing или UI.
13. Redirect и DNS не позволяют обойти SSRF-защиту.
14. Исходящий webhook имеет стабильный event ID; получатель может дедуплицировать.
15. Retention звонка начинается от `calls.created_at`, а ingest event хранится по
    отдельной документированной политике.
16. Удаление/revoke connection не удаляет созданные звонки и исторический audit.
17. Backend-ready, frontend-ready, connector-verified и production-ready — разные
    статусы поставки.
18. Disable/revoke имеет server-defined cut-off: для каждого принятого item
    однозначно известно continue/pause/cancel; race worker с командой это не решает.
19. Недоверенный media до validation находится в quarantine и обрабатывается в
    resource-bounded sandbox без network/host filesystem privileges.
20. Удаление company/connection не каскадит provenance, usage и audit; erasure —
    inventory-driven workflow с retention/legal-hold policy.
21. Admission принимает item только вместе с durable reservation backlog/storage
    quota; `202` не создаёт неограниченный долг системы.

## 7. Основные сценарии

### 7.1. Создание generic connection

Менеджер выбирает company, name, department, optional folder, инструкции и webhook.
Backend повторно проверяет memberships и scope. После создания пользователь может
выпустить API key. Полный ключ показывается один раз; в БД хранится hash и prefix.

### 7.2. Ротация ключа

Новый и старый ключи могут действовать одновременно до заданного `overlap_until`.
После него старый автоматически отзывается. UI предупреждает о последнем
использовании обоих ключей. Повторная rotation-команда идемпотентна.

### 7.3. Приём по URL

Клиент отправляет external IDs, title, occurred_at, recording URL, participants и
metadata. API валидирует размер тела, auth, scope и schema; сохраняет event/item и
возвращает `202`. Worker безопасно получает media, вычисляет hash, проверяет формат,
создаёт call и отслеживает processing.

### 7.4. Потоковая загрузка

Для источника без доступного URL используется отдельный multipart endpoint.
Поток пишется во временный storage object с лимитом; после media validation объект
атомарно становится входом CreateCall. Обрыв удаляется cleanup worker-ом.

### 7.5. Повтор события

- тот же event ID и payload: вернуть исходный item;
- тот же event ID, другой payload: `409 external_event_conflict`;
- новый event ID, тот же external call ID: вернуть существующий item и записать
  `duplicate_event_received`;
- тот же media hash, другой external call ID: не объединять автоматически; поставить
  diagnostic flag для просмотра.

### 7.6. Сбой и retry

Timeout/429/5xx/временная storage-ошибка → exponential backoff с jitter.
Невалидный URL, запрещённый адрес, превышение размера, неподдерживаемый media и
отозванный scope → permanent failure. Billing/ACL/configuration conflict →
`blocked`, понятное действие оператору. Ручной retry не сбрасывает attempts/audit.

### 7.7. Завершение

После создания call item становится `processing`. Terminal status определяется по
существующим call jobs. При `analyzed` создаётся outbox event `call.analysis.ready`;
при terminal failure — `call.processing.failed`. Webhook delivery не меняет успех
самого анализа.

### 7.8. Bitrix24

Connection устанавливается через OAuth 2.0 либо локальное серверное приложение.
Токены шифруются, refresh single-flight и optimistic. Vendor event сначала
сохраняется неизменяемо, затем нормализуется. Reconciliation закрывает пропуски
webhook. Точный call/recording API и требуемые permissions фиксируются только после
проверки на тестовом портале.

Проверка доступна через бесплатный облачный портал с ограниченным пробным доступом
к REST/Marketplace либо партнёрский NFR. Постоянный REST может потребовать платный
тариф; это внешнее условие и не должно скрываться в onboarding.

### 7.9. amoCRM

До production adapter используются contract fixtures/emulator. Реальный OAuth и
webhooks проверяются в пробном аккаунте. После истечения trial может потребоваться
оплата. Наличие trial не считается подтверждением конкретного API записи звонков.

## 8. Состояния

Connection:

`draft → active ↔ degraded → disabled → revoked`.

Ingest item:

`received → fetching/uploading → validating → creating_call → processing →
completed`.

Ответвления:

- retryable: `* → retry_wait → previous executable stage`;
- operator: `* → blocked → retry_wait` после исправления;
- terminal: `* → failed|cancelled`.

Переходы выполняются compare-and-set по `lock_version`. Произвольный PATCH status
запрещён.

## 9. Нефункциональные требования

- API availability target и latency SLO фиксируются перед pilot по baseline;
- приём события не зависит от времени транскрипции;
- workers горизонтально масштабируются без двойного claim;
- backpressure ограничивает загрузки по connection/company/global;
- max media size/duration согласованы с тарифом и ручной загрузкой;
- очередь имеет age/size limits и admission control;
- все list API paginated с deterministic order;
- timestamps — UTC/TIMESTAMPTZ, UI — timezone пользователя;
- payload schema версионируется; breaking change требует новой API version;
- raw vendor payload имеет отдельный TTL и redaction policy;
- численные capacity envelope, RPO/RTO, queue-age SLO и DB/object-storage restore
  consistency фиксируются до pilot;
- data residency, subprocessors, lawful basis/consent, DSAR/erasure и cross-border
  transfer решения закрываются до production data;
- accessibility: keyboard, focus, aria-live, contrast, reduced motion;
- responsive UI и обе темы обязательны.

## 10. Безопасность и приватность

- ключ формата `vt_live_<prefix>.<secret>`, CSPRNG не менее 256 бит;
- hash через HMAC-SHA-256 с server-side pepper либо Argon2id; constant-time compare;
- scopes: `calls:ingest`, `ingest:read`, `webhooks:manage`; least privilege;
- rate limit по key, connection, company и source IP;
- optional IP allowlist, expiry, last-used и emergency revoke;
- OAuth secrets и webhook signing secrets — envelope encryption с key version;
- HTTPS, TLS verification, безопасный redirect policy;
- блок private, loopback, link-local, multicast, unspecified, metadata endpoints и
  DNS rebinding на каждом connect;
- Content-Length не считается достаточным: streaming hard limit обязателен;
- MIME определяется содержимым и ffprobe, filename не является доверием;
- metadata/participants проходят length, count и Unicode validation;
- payload/log redaction и запрет секретов в error messages;
- access/export audit и retention deletion inventory расширяются новыми таблицами.
- OAuth callback защищён одноразовым state, строгим redirect URI, повторной
  membership/session проверкой и PKCE S256 там, где vendor поддерживает;
- media parser/ffprobe/antivirus запускаются изолированно с CPU/RAM/time/process
  limits; malware policy и patch ownership закрыты до pilot;
- audit append-only не называется tamper-proof без external checkpoint/WORM;
  документируется доверие к DBA/backup operators.

## 11. Наблюдаемость

Correlation: `request_id`, `trace_id`, `connection_uuid`, `ingest_item_uuid`,
`external_call_id_hash`, `call_uuid`, `outbox_uuid`.

Метрики:

- accepted/deduplicated/rejected events;
- ingest latency по этапам и provider;
- queue depth/oldest age/claim recovery;
- retry/permanent/blocked rate по error code;
- bytes/duration и concurrent downloads;
- time-to-call/transcript/analysis;
- webhook delivery latency/retry/failure;
- OAuth refresh failures и connection health;
- orphan temp objects и reconciliation mismatches.

Алерты строятся по SLO и trend, а не по единичной пользовательской ошибке.

## 12. Rollout

Интеграционный rollout начинается только после этапов 0-9 связанной спецификации
кредитного биллинга. Затем:

1. Миграции и выключенные workers; проверка rollback/forward compatibility.
2. Generic API за feature flag для внутренней company с реальным reserve/settle.
3. Emulator E2E: duplicates, timeouts, 429, corrupt media, restart, webhook retry,
   insufficient credits и продолжение после funding.
4. Ограниченный pilot с budgets, dashboard, runbook и on-call ownership.
5. Bitrix24 test portal, затем один pilot tenant.
6. Постепенное повышение лимитов; kill switch на connection/provider/global.
7. amoCRM только после post-pilot review общего контракта.

## 13. Критерии готовности

### Backend-ready

- schema, API, workers, SSRF, idempotency, outbox, audit и tests готовы;
- migration/integration/unit/race/lint проходят;
- fault injection доказывает восстановление после commit/network/process failures.

### Frontend-ready

- connection/key/import UX работает с реальным API;
- ошибки/permissions/loading/empty/partial states проверены;
- responsive, keyboard, light/dark и production build проверены;
- visual matrix, geometry assertions и ручная проверка симметрии/отступов прошли
  требования связанной поэтапной спецификации.

### Connector-verified

- OAuth install/refresh/revoke и реальный vendor event проверены в sandbox/trial;
- реальные media и permissions подтверждены, fixtures обновлены из redacted facts;
- reconciliation и rate limiting проверены.

### Production-ready

- backend + frontend + connector готовы;
- alerts/dashboard/runbook/secret rotation/backup/restore подтверждены;
- pilot прошёл без дублей и неразрешённого доступа;
- exact pushed SHA и CI проверены только при отдельном запросе на публикацию.

## 14. Открытые решения до кода

- production object storage и malware-scanning policy;
- точные plan entitlements/quotas для integrations;
- TTL raw payload и operational logs;
- публичный base URL для OAuth/webhooks;
- юридические consent/PII требования к автоматически полученным записям;
- data residency, subprocessors, DPA/DSAR, legal hold и сроки по каждому классу
  ingest/audit/delivery/idempotency данных;
- численные SLO/capacity/RPO/RTO и согласованная disaster recovery процедура;
- Bitrix24 marketplace против per-customer local app;
- поддерживаемый первым telephony provider.

Ни одно открытое решение нельзя молча заменить предположением разработчика.

## 15. Проверенные внешние ограничения

- Bitrix24 разрешает создать бесплатный облачный портал, но для REST API официально
  предлагает demo/trial Marketplace, постоянную подписку либо NFR для
  технологических партнёров:
  <https://www.bitrix24.ru/apps/dev.php> и
  <https://apidocs.bitrix24.ru/market/preparing-to-publish/how-to-add-app.html>.
- Локальные/server-side приложения Bitrix24 используют OAuth 2.0 и требуют
  административного доступа к конкретному порталу:
  <https://apidocs.bitrix24.ru/settings/how-to-call-rest-api/authorization.html>.
- Официальная документация amoCRM подтверждает возможность зарегистрировать
  аккаунт с пробным периодом для разработки интеграции:
  <https://www.amocrm.ru/developers/content/chats/chat-start>.

Эти источники подтверждают способ получить тестовую среду, но не подтверждают, что
на выбранном тарифе доступен каждый необходимый метод получения записи звонка.
Такой метод и его permissions проверяются отдельно перед фиксацией adapter contract.
