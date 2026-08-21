# Спецификация: TTL звонков, полное удаление данных и исторические версии инструкций

Статус: production design
Дата: 2026-08-20
Backend: `C:\projects\VerbaTrace\Monolit`
Frontend: `C:\projects\VerbaTrace-frontend`

## 1. Цель

VerbaTrace должен реально исполнять срок хранения звонков, заявленный тарифом, и
при этом сохранять проверяемость анализа до момента удаления самого звонка.

Итоговый жизненный цикл:

`создание звонка → фиксированная дата удаления → предупреждение → постановка в
очередь очистки → удаление строк БД → надёжное удаление файлов → завершённый аудит`.

Инструкция имеет отдельный жизненный цикл:

`активная → мягко удалённая и недоступная для новых анализов → доступная только из
исторических анализов → окончательно удалённая после исчезновения последней ссылки`.

Удаление не должно быть одной большой хрупкой транзакцией или циклом, который
постоянно опрашивает БД. Два независимых периодических worker-а работают малыми
пачками, возобновляют незавершённую работу после сбоя и оставляют подробный аудит.

## 2. Проверенное исходное состояние

На момент написания спецификации:

- `plans.history_retention_days` существует, возвращается API и показывается в
  тарифах;
- значение ещё не управляет выборкой и автоматическим удалением звонков;
- есть ручной `DELETE /api/v1/calls/{uuid}`;
- ручное удаление сначала удаляет строку БД, затем audio и ASR cache, поэтому сбой
  object storage после commit может оставить бесхозный файл;
- связанные транскрипции, анализы, revisions, QA, comments и actions в основном
  удаляются каскадно, но единого проверенного перечня всех артефактов нет;
- инструкция — изменяемая строка с `is_active`, одним `file_path` и одним hash;
- анализ не хранит неизменяемый снимок точных версий применённых инструкций;
- страница списка инструкций есть, отдельной read-only страницы версии нет;
- в карточке звонка после AI-анализа сразу расположено обсуждение анализа.

Эта поставка заменяет декларативный retention исполняемым и вводит immutable
instruction snapshots. Она не должна полагаться на текущий `ON DELETE CASCADE` без
явной проверки полного графа данных.

## 3. Зафиксированные продуктовые решения

### 3.1. Сроки бизнес-тарифов

Три бизнес-тарифа получают сроки хранения соответственно:

| Уровень бизнес-тарифа | `history_retention_days` |
|---|---:|
| первый | 180 |
| второй | 365 |
| третий | 550 |

Миграция должна обновлять планы по стабильному `code`, а не по отображаемому
названию или UUID, предварительно сверив фактические коды в текущей БД. Если
ожидаются не ровно три бизнес-плана, миграция завершается ошибкой и ничего не
меняет. Персональные значения этой спецификацией не изменяются.

### 3.2. Формула срока хранения

При создании звонка сервер фиксирует:

```text
retention_days_at_creation = effective plan history_retention_days
retention_base_at          = call.created_at
retention_expires_at       = retention_base_at + retention_days_at_creation days
```

Срок — календарный интервал PostgreSQL в UTC. Нельзя прибавлять новый срок к уже
существующему `retention_expires_at`.

При повышении тарифа применяется формула:

```text
candidate = retention_base_at + new_plan_history_retention_days days
retention_expires_at = max(current_retention_expires_at, candidate)
```

Пример: звонок создан 1 января на тарифе 180 дней. Переход на 365 дней меняет дату
истечения на `1 января + 365 дней`, а не на `1 января + 180 + 365 дней`.

При понижении тарифа существующие звонки не сокращаются. Новые звонки получают срок
нового тарифа. Повторное применение одного и того же события подписки идемпотентно.

### 3.3. Смена владельца и организационной области

- срок принадлежит звонку и не пересчитывается при переносе между папками/отделами;
- перенос личного звонка в компанию не сокращает срок;
- если перенос должен дать преимущества более дорогого активного плана компании,
  применяется та же формула `max(current, created_at + company_days)`;
- выход пользователя из компании не меняет уже записанный срок;
- административное продление — отдельная команда с обязательной причиной и
  аудитом; дата не может быть раньше текущей;
- legal hold в первой реализации не предоставляется пользователю, но schema
  резервирует `retention_hold_until` и `retention_hold_reason` для дальнейшего
  юридического процесса. Активный hold запрещает постановку на удаление.

### 3.4. Backfill существующих звонков

Для звонков без `retention_expires_at` одноразовая миграционная job вычисляет:

```text
retention_base_at = calls.created_at
retention_days_at_creation = срок текущего эффективного плана владельца/компании
retention_expires_at = created_at + retention_days_at_creation days
```

Backfill не удаляет данные. Если вычисленная дата уже прошла, звонок получает
`retention_state='grace'` и минимальное переходное окно, например 30 дней. Точное
окно задаётся конфигурацией выпуска и показывается пользователю заранее. Запуск без
зафиксированного grace запрещён.

Неоднозначный владелец или отсутствующая подписка переводят запись в
`retention_state='blocked'`, а не выбирают срок предположительно.

## 4. Границы удаления звонка

После успешной очистки не должны оставаться пользовательские данные звонка.
Inventory обязан включать как минимум:

- `calls` и связи папок/избранного;
- media object, preview/derived media и `asr_cache_path`;
- speaker hints, diarization roles и participant mappings;
- все transcription rows, words, segments, contents, revisions, assignments и edit
  audit;
- analyses, attempts, result JSON/text, evidence и processing jobs;
- human QA drafts, revisions, criteria, evidence, challenges и events;
- comments, comment revisions и mentions;
- actions, dispositions, evidence, transfer requests, events, deliveries и
  относящиеся к ним pending outbox entries/notifications;
- call exports и файлы экспортов;
- deep/aggregate-analysis membership и производные materialized facts;
- search documents, embeddings, vector index, caches и saved-monitor membership;
- usage reservation, если она всё ещё открыта;
- новые таблицы с FK на call, добавленные после этой спецификации.

Агрегированные обезличенные метрики могут пережить звонок только при наличии
отдельно утверждённого контракта, который гарантирует невозможность восстановления
текста, участника или call UUID. По умолчанию они тоже удаляются/пересчитываются.

Обычный продуктовый audit не должен хранить title, transcript, quote или PII после
очистки. В retention audit остаются UUID, технические статусы, количества, размеры,
hash пути и timestamps. Полный object key допустим только в защищённой operational
таблице с ограниченным TTL.

## 5. Модель данных backend

### 5.1. Поля `calls`

```sql
retention_base_at          timestamptz not null
retention_days_at_creation integer not null check (retention_days_at_creation > 0)
retention_expires_at       timestamptz not null
retention_state            text not null default 'active'
retention_hold_until       timestamptz null
retention_hold_reason      text null
retention_version          bigint not null default 1
```

`retention_state`: `active | grace | queued | deleting | deletion_failed | blocked`.
После физического удаления строки терминальное состояние существует только в
retention audit/job.

Индексы:

```sql
(retention_state, retention_expires_at, call_uuid)
WHERE retention_state IN ('active','grace','deletion_failed')
```

### 5.2. Durable deletion jobs

`retention_call_deletions`:

```text
deletion_uuid             uuid PK
call_uuid                 uuid UNIQUE, без cascade FK
scope_type/scope_uuid     снимок владельца
retention_expires_at      timestamptz
status                    claimed|db_deleted|files_pending|completed|failed|blocked
attempts                  integer
available_at              timestamptz
locked_at/locked_by       nullable
last_error_code           nullable
last_error_message_safe   nullable
db_manifest               jsonb
file_manifest_encrypted   jsonb
created_at/updated_at/completed_at
```

Job должна переживать удаление `calls`, поэтому FK на `calls` запрещён. Manifest
создаётся до DB deletion и содержит перечень storage objects, ожидаемые количества
строк по категориям и correlation ID.

### 5.3. Retention audit

`retention_audit_events` — append-only:

```text
event_uuid, run_uuid, deletion_uuid, entity_type, entity_uuid,
event_type, item_count, byte_count, metadata_safe, created_at
```

Обязательные события одного запуска:

- `call_worker_run_started`;
- `calls_selected_for_deletion` — «взяты такие UUID», cutoff и количество;
- `call_manifest_created`;
- `call_database_deletion_started`;
- `call_database_rows_deleted` — количества по таблицам;
- `call_files_deletion_started`;
- `call_file_deleted` или агрегированное событие по безопасной пачке;
- `call_files_deleted` — количество и bytes;
- `call_deletion_completed`;
- `call_deletion_failed`/`call_deletion_blocked` с безопасным error code;
- `call_worker_run_completed` с totals/duration.

Аудит пишется после каждого устойчивого этапа, а не только в конце. Повтор job не
создаёт ложное второе удаление: события содержат `deletion_uuid`, attempt и stage.

### 5.4. Версии и снимки инструкций

Изменение title или файла не переписывает историческую версию.

`analysis_instruction_versions`:

```text
instruction_version_uuid uuid PK
instruction_uuid         uuid not null
version                  integer not null
title_snapshot           text not null
scope_snapshot           text not null
owner snapshots          nullable
original_filename        text not null
mime_type/size_bytes/content_sha256/file_path
status                   draft|published|retired
created_by_user_uuid     nullable
created_at/published_at
UNIQUE(instruction_uuid, version)
```

`call_analysis_instruction_snapshots`:

```text
analysis_uuid             uuid references call_analyses on delete cascade
instruction_version_uuid  uuid references analysis_instruction_versions on delete restrict
position                  integer
selection_source          explicit|personal|company|department|folder
title_snapshot/scope_snapshot/content_sha256
PRIMARY KEY(analysis_uuid, instruction_version_uuid)
```

Снимки создаются атомарно с analysis attempt до обращения к модели. Повторный
анализ создаёт новый набор снимков. История старого анализа не меняется.

### 5.5. Мягкое и окончательное удаление инструкции

В `analysis_instructions` добавляются:

```text
deleted_at, deleted_by_user_uuid, deletion_reason,
purge_state(active|eligible|queued|failed), purge_after
```

Пользовательское Delete означает soft delete:

- инструкция исчезает из выбора, поиска и обычного списка;
- новые analysis snapshots не могут на неё ссылаться;
- текущая опубликованная версия и файл сохраняются;
- из анализа она остаётся доступной по ACL звонка в read-only режиме;
- прямой URL без видимого анализа не расширяет доступ.

Окончательная очистка разрешена, когда `NOT EXISTS` ни одного
`call_analysis_instruction_snapshots` для всех версий инструкции и нет другого
явного RESTRICT-reference. Проверка и постановка в очередь выполняются в одной
транзакции/под блокировкой. Сначала удаляются файлы версий, затем version rows и
корневая инструкция. При ошибке файла metadata не удаляется, job повторяется.

## 6. Worker звонков

### 6.1. Расписание и нагрузка

- рекомендуемый запуск: один раз в сутки в низконагруженное окно;
- cron/interval конфигурируется, например `CALL_RETENTION_CRON`;
- запуск каждые пять минут не требуется;
- один активный экземпляр обеспечивается PostgreSQL advisory lock;
- выборка — keyset pagination и `FOR UPDATE SKIP LOCKED`, batch по умолчанию 100;
- между batch допускается короткая пауза и лимит общего времени запуска;
- индексированный запрос не делает full scan;
- ручной безопасный запуск поддерживает dry-run и limit.

### 6.2. Алгоритм

1. Получить singleton lock и создать `run_uuid`.
2. Выбрать истёкшие `active/grace/deletion_failed`, без hold, с
   `retention_expires_at <= now()`.
3. Записать `calls_selected_for_deletion` с точным набором UUID пачки.
4. Для каждого звонка под row lock повторно проверить expiry, hold и version.
5. Собрать file/DB manifest и создать/переиспользовать deletion job.
6. Перевести звонок в `deleting`; пользовательские API возвращают `410
   call_deletion_in_progress`, mutations запрещены.
7. В транзакции удалить DB graph, processing/outbox work и записать counts/audit.
8. После commit удалить storage objects по manifest идемпотентно: `not found`
   считается успехом, timeout — retry.
9. Удалить search/vector/cache artifacts либо через тот же job, либо durable outbox.
10. Сверить manifest, отметить `completed`, очистить operational file manifest после
    ограниченного срока.

### 6.3. Частичные сбои

- сбой до DB commit: транзакция откатывается, stage повторяется;
- DB удалена, worker упал до файлов: job остаётся `db_deleted/files_pending`;
- часть файлов удалена: повторный DELETE безопасен;
- object storage недоступен: exponential backoff с jitter и dead-letter после
  лимита, alert оператору;
- неизвестный новый FK блокирует удаление: `blocked`, данные не обходятся через
  отключение constraints;
- два worker-а: advisory lock плюс unique `call_uuid` не допускают две jobs;
- пользователь удаляет звонок одновременно: ручной endpoint использует тот же
  orchestrator, а не отдельный порядок действий;
- повышение тарифа после claim, но до deletion: row-lock повторная проверка
  `retention_version`; при продлении job отменяется как `superseded`;
- восстановление после удаления DB невозможно; UI предупреждает об этом до
  ручного удаления, автоматическое удаление имеет grace/notification policy.

## 7. Worker инструкций

### 7.1. Расписание

- отдельный процесс/worker и отдельный singleton lock;
- один раз в сутки после ожидаемого завершения call-retention worker-а достаточно;
- допустим более редкий период, например раз в неделю, без влияния на корректность;
- по умолчанию `INSTRUCTION_PURGE_CRON` задаётся позже call worker-а;
- batch по умолчанию 50; никакого постоянного polling.

### 7.2. Алгоритм

1. Выбрать soft-deleted instructions с `purge_after <= now()`.
2. Под lock повторно проверить отсутствие snapshot/reference.
3. Записать `instructions_selected_for_purge` с UUID и версиями.
4. Создать durable purge job и file manifest.
5. Удалить файлы версий идемпотентно.
6. В транзакции удалить версии и корневую строку.
7. Записать counts и `instruction_purge_completed`.

Если ссылки существуют, это нормальное состояние, а не ошибка: инструкция остаётся
исторически доступной. Чтобы не проверять её ежедневно бесконечно, `purge_after`
сдвигается, например на 7 дней, либо eligibility обновляется событием завершения
call worker-а. Предпочтителен гибрид: дешёвая индексированная ежедневная проверка
небольшой пачки плюс событие после удаления звонков.

## 8. Backend API

### 8.1. Звонки и retention

Call DTO для пользователей с доступом:

```json
{
  "retention": {
    "expires_at": "2027-08-20T10:00:00Z",
    "days_at_creation": 365,
    "state": "active",
    "can_extend": false
  }
}
```

Не раскрывать внутренние manifests/job errors обычному пользователю.

Административные endpoints:

- `GET /api/v1/admin/retention/runs`;
- `GET /api/v1/admin/retention/deletions/{uuid}`;
- `POST /api/v1/admin/retention/dry-run`;
- `POST /api/v1/admin/retention/deletions/{uuid}/retry`;
- `POST /api/v1/admin/calls/{uuid}/retention/extend` с reason и optimistic version.

### 8.2. Instruction history

- `DELETE /api/v1/instructions/{uuid}` становится soft delete;
- `GET /api/v1/analyses/{analysis_uuid}/instructions` возвращает только снимки
  реально применённых версий;
- `GET /api/v1/analyses/{analysis_uuid}/instructions/{version_uuid}` возвращает
  read-only detail при наличии доступа к анализу;
- `GET /api/v1/instruction-versions/{version_uuid}` без analysis context не должен
  обходить ACL;
- изменение активной инструкции создаёт новую version, не меняет published row;
- download исторического файла использует short-lived URL и аудит доступа.

Пример списка:

```json
{
  "items": [{
    "instruction_id": "...",
    "version_id": "...",
    "version": 3,
    "title": "Контроль следующего шага",
    "scope": "department",
    "selection_source": "folder",
    "state": "deleted_but_preserved",
    "content_available": true
  }]
}
```

Если snapshot legacy-звонка отсутствует, API возвращает явное
`historical_instruction_snapshot_unavailable`, а UI не подставляет текущую версию.

## 9. ACL и безопасность

- доступ к исторической инструкции из анализа равен доступу к конкретному звонку и
  анализу, а не текущему членству в scope инструкции;
- пользователь, потерявший доступ к звонку, теряет и доступ к deep link версии;
- soft-deleted instruction нельзя активировать через старый endpoint;
- только владелец personal instruction или разрешённая организационная роль может
  soft-delete активную instruction;
- окончательная purge — только system actor;
- содержание инструкции не включается в общий audit/log/metric labels;
- download логируется как security/access audit;
- UUID enumeration возвращает 404 вместо различимых 403;
- worker service account имеет минимальные DB/storage permissions;
- manifests шифруются либо хранят object keys отдельно от общего аудита;
- резервные копии подчиняются документированной backup retention. Нельзя обещать
  мгновенное удаление из уже созданного immutable backup; срок восстановления и
  повторное удаление после restore должны быть описаны в политике.

## 10. Frontend: карточка звонка

### 10.1. Размещение

В `CallDetailPanel` внутри `analysis-card-stack` добавить блок сразу после карточки
AI-анализа и перед `AnalysisComments`. Это соответствует отмеченному месту на
макете: под действием «Открыть полный анализ», над «Обсуждение анализа».

Название: **«Применённые инструкции»**. Подзаголовок: **«Правила, по которым был
сформирован этот анализ»**.

Компактное состояние:

- icon документа;
- количество, например «3 инструкции»;
- до трёх chips с title; остальные — «+2»;
- справа chevron/кнопка «Посмотреть»;
- весь интерактивный элемент имеет заметный focus state и accessible name.

Нажатие на конкретную instruction открывает
`/app/calls/{call_uuid}/analyses/{analysis_uuid}/instructions/{version_uuid}`.
Если инструкций несколько, нажатие на общий блок сначала раскрывает список; выбор
строки ведёт на отдельную страницу.

Состояния:

- loading skeleton без скачка высоты;
- 0 snapshots: «Для этого анализа инструкции не применялись»;
- legacy unknown: «Данные о применённых инструкциях для этого анализа не
  сохранились»;
- deleted but preserved: badge «Удалена из настроек · сохранена для истории»;
- content temporarily unavailable: title/hash видимы, retry без ложного текста;
- analysis pending/failed: блок не показывается до появления snapshot либо
  показывает нейтральный processing state, согласованный с API.

### 10.2. Дата удаления звонка

В метаданных звонка показать «Хранится до …» с tooltip, что дата зафиксирована при
создании и может быть только продлена повышением тарифа/администратором. За grace
период показать предупреждение, не используя цвет как единственный сигнал.

При `deleting` карточка становится read-only и сообщает, что удаление уже начато.

## 11. Frontend: отдельная страница версии инструкции

### 11.1. Маршрут и навигация

Маршрут привязан к call+analysis context, чтобы ACL и возврат были однозначны:

`/app/calls/:callId/analyses/:analysisId/instructions/:versionId`

Back возвращает в тот же звонок и сохраняет scroll/expanded state, если переход
был внутри SPA. Прямой deep link после reload также работает.

### 11.2. Содержимое

- breadcrumb: «Звонки → {название звонка} → Анализ → Инструкция»;
- title и badge состояния;
- «Версия N», дата публикации/применения;
- область и источник выбора: personal/company/department/folder/explicit;
- hash/имя файла и кнопка скачивания при разрешении;
- читаемый preview содержимого в исходной кодировке;
- уведомление: «Показана версия, применённая к этому анализу. Текущая инструкция
  могла измениться»;
- для soft-deleted: «Инструкция удалена из настроек, но сохранена, чтобы можно было
  проверить оценку этого звонка»;
- никаких Edit/Restore/Apply controls на исторической странице.

Markdown отображается безопасным renderer-ом без raw HTML/scripts, внешние ссылки
получают безопасные attributes. Для plain text сохраняются переносы. Для формата,
который нельзя безопасно отобразить, показываются metadata и контролируемое
скачивание; содержимое не угадывается.

### 11.3. Геометрия и темы

- desktop content width 760–900 px, длинные строки переносятся;
- sidebar/shell остаются существующими, страница не выглядит отдельным продуктом;
- mobile — одна колонка, sticky back/header только если не перекрывает content;
- минимум 44 px для touch targets;
- light: существующая кремовая поверхность, графитовый текст, оранжевый accent;
- dark: существующая графитовая поверхность, светлый текст, тот же semantic accent;
- цвета берутся из текущих CSS variables, без hard-coded дубликатов;
- contrast не ниже WCAG AA; focus, hover, disabled, loading и error проверяются в
  обеих темах;
- длинный русский title, filename, scope и 100+ KB текста не ломают ширину;
- code/pre имеет горизонтальный scroll внутри контейнера, не всей страницы.

## 12. Уведомления

До автоматического удаления нужны предупреждения, например за 30 и 7 дней, если
продуктовая политика не отключает их для конкретного scope. Delivery дедуплицируется
по `(call_uuid, retention_version, kind)`.

Продление срока инвалидирует старые pending notifications и создаёт новые по новой
дате. Понижение тарифа не создаёт уведомление об ускоренном удалении, потому что
старые сроки не сокращаются.

## 13. Corner cases

1. `history_retention_days=0`: трактовка должна быть заранее определена. Для этой
   поставки рекомендуется считать это некорректным планом, а не «удалить сразу» и
   не «хранить вечно».
2. Leap year/DST: расчёт в UTC через PostgreSQL interval; UI форматирует timezone.
3. Звонок создан ровно во время смены плана: subscription/usage transaction должна
   вернуть один зафиксированный plan snapshot.
4. Upgrade event доставлен дважды: `max(base+days)` не увеличивает срок повторно.
5. Upgrade 180→365→550: итог `created_at+550`, не сумма.
6. Downgrade 550→180: старый звонок остаётся до `created_at+550`, новый получает
   180.
7. Upgrade после downgrade: кандидат сравнивается с текущим сроком; сокращения нет.
8. Просроченный звонок в grace обновлён дорогим тарифом: если candidate в будущем,
   вернуть `active`; если deletion уже перешёл DB commit, восстановление запрещено.
9. Active legal hold: worker пропускает и аудирует без раскрытия причины обычному
   пользователю.
10. Файл отсутствует до удаления: `not found` — успех с audit marker.
11. В БД удаление успешно, storage нет: job продолжает retries без строки call.
12. Storage удалён, DB transaction откатилась: повторное удаление DB допустимо, но
    call media уже недоступна; поэтому предпочтителен durable copy/manifest и
    порядок DB→storage с явным deleting UI.
13. Новый тип файла добавлен без manifest provider: readiness check блокирует
    выпуск retention worker-а.
14. Открытый action/appeal не продлевает retention сам по себе. Если бизнес требует
    блокировку, это отдельный documented hold, а не скрытое исключение.
15. Удалённая инструкция используется тысячами анализов: она хранится до последней
    ссылки; worker не делает full scan благодаря индексу snapshot by instruction.
16. Instruction soft-delete одновременно с началом анализа: snapshot transaction
    либо фиксирует published version до delete, либо анализ не стартует; полунабора
    быть не может.
17. File replacement одновременно с анализом: analysis использует immutable version
    hash, не mutable path.
18. Последний связанный звонок удалён, instruction worker уже идёт: повторная
    reference check под lock определяет eligibility.
19. Компания удалена раньше инструкций: scope snapshot не должен каскадно уничтожить
    историческую version; FK owner snapshots используют SET NULL/denormalized data.
20. Пользователь удалён: historical page показывает нейтральное «Автор недоступен»,
    не ломает snapshot.
21. Legacy analysis без snapshots не связывается задним числом с текущими правилами.
22. Purge worker удалил instruction metadata, но файл failed: такой порядок запрещён;
    metadata удаляется только после подтверждения storage stage.
23. Backup restore возвращает уже удалённый call: tombstone/deletion ledger должен
    позволить повторить purge после восстановления.
24. Clock skew: решение принимает DB `now()`, не часы frontend/worker host.
25. Worker пропустил несколько суток: в следующий запуск обрабатывает backlog
    batches без монопольной блокировки таблиц.

## 14. Наблюдаемость и эксплуатация

Метрики:

- eligible/claimed/completed/failed/blocked calls;
- oldest overdue retention age;
- DB rows и storage bytes deleted;
- stage duration и retries;
- orphan file count from reconciliation;
- soft-deleted instructions, eligible instructions, purge failures;
- preserved instruction bytes по причине active references.

Alerts:

- oldest eligible call старше 48 часов;
- repeated worker failure;
- deletion job stuck в одном stage;
- storage orphan или manifest mismatch;
- instruction purge blocked неожиданной FK;
- business plan с нулевым/невалидным retention.

Structured logs содержат run/deletion/correlation UUID и error code, но не transcript,
instruction content, title, email или object URL.

Admin UI должен показывать этапы именно в понятных категориях: «взято звонков»,
«строки БД удалены», «файлы удалены», «ошибки/повторы», с раскрытием UUID и времени.

## 15. Миграция и rollout

1. Инвентаризировать все FK, non-FK references и storage providers; зафиксировать
   automated test, который ломается при появлении неучтённой call-owned таблицы.
2. Добавить schema без запуска worker-ов.
3. Обновить business plan values по проверенным codes.
4. Начать записывать retention snapshot новым звонкам.
5. Ввести immutable instruction versions/snapshots и переключить analysis creation.
6. Backfill instruction version 1 и snapshots только там, где точную связь можно
   доказать; неизвестные legacy не угадывать.
7. Запустить call backfill в dry-run, проверить counts и grace.
8. Показать даты в UI и отправить предупреждения.
9. Запустить worker в shadow/dry-run и сравнить manifest вручную.
10. Включить маленький batch для ограниченного scope, проверить DB+storage+UI.
11. Включить ежедневный call worker, затем instruction worker.
12. Выполнить reconciliation orphan objects и audit completeness.

Rollback worker-а означает остановку новых claims; незавершённые jobs должны быть
безопасно доведены до конца или явно оставлены для resume. Откат schema до
завершения jobs запрещён.

## 16. Проверка backend

Unit:

- формулы 180/365/550, upgrade/downgrade и повтор события;
- UTC/date boundaries;
- state transitions и retry classification;
- ACL historical instruction;
- snapshot immutable при replace/delete;
- purge eligibility.

Integration PostgreSQL:

- `SKIP LOCKED`, singleton, concurrent manual delete;
- полный cascade/inventory для звонка со всеми типами данных;
- manifest и audit stages;
- worker resume после каждого возможного crash point;
- upgrade отменяет ещё не committed deletion;
- instruction не purge-ится при одной оставшейся ссылке и purge-ится без ссылок;
- migration/backfill идемпотентны.

Storage contract:

- delete success/not-found/timeout/permission denied;
- partial multi-file failure;
- retries не повреждают чужие objects;
- reconciliation обнаруживает orphan.

API/RBAC:

- visible call открывает snapshot;
- invisible UUID маскируется 404;
- deleted instruction нельзя выбрать для нового анализа;
- legacy state честно отражается;
- admin retry/extend требует capability, reason и version.

## 17. Проверка frontend

- блок расположен между AI-анализом и обсуждением на desktop/mobile;
- 0/1/many/deleted/legacy/loading/error состояния;
- переход на точную version page и back/reload/deep link;
- изменение текущей инструкции не меняет историческую страницу;
- потеря ACL закрывает уже открытую ссылку после reload;
- русский длинный текст, большие файлы, pre/code и filename;
- keyboard order, screen reader labels, focus restoration;
- light/dark screenshots на desktop/tablet/mobile;
- contrast, zoom 200%, reduced motion;
- дата TTL и grace предупреждение не создают layout shift;
- `deleting` read-only state;
- browser smoke подтверждает реальный API payload, а не mock.

## 18. Критерии готовности

Функция готова только если одновременно выполнено следующее:

1. Три business plans имеют проверенные 180/365/550 дней.
2. Каждый новый звонок получает воспроизводимую дату удаления.
3. Upgrade использует `max(current, created_at + new_days)` и никогда не суммирует
   тарифные интервалы.
4. Ежедневный worker удаляет весь доказанный data graph и storage objects.
5. Audit позволяет ответить: какие звонки взяли, какие строки БД удалили, какие
   файлы удалили, где произошёл сбой и что было повторено.
6. Crash после любого stage не создаёт вечный orphan и допускает resume.
7. Soft-deleted инструкция не применяется заново, но доступна из связанного анализа.
8. После удаления последней ссылки отдельный редкий worker окончательно очищает все
   версии и файлы инструкции.
9. В карточке звонка блок находится под AI-анализом и над обсуждением.
10. Отдельная instruction version page читабельна и ровна в обеих темах и на mobile.
11. Legacy uncertainty не маскируется текущими данными.
12. Backend CI, frontend build, integration tests и authenticated browser smoke
    проходят на реальных persisted данных.
