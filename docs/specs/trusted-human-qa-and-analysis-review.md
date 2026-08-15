# Спецификация: доверенная ручная проверка и исправление анализа

Статус: готова к реализации  
Продукт: VerbaTrace  
Контуры: backend `C:\projects\VerbaTrace\Monolit`, frontend `C:\projects\VerbaTrace-frontend`  
Приоритет: P0

---

## 1. Цель

Добавить полноценный Human QA-контур, в котором результат ИИ является предложением,
а уполномоченный руководитель принимает отдельное, объяснимое и аудируемое решение.

Пользователь должен иметь возможность:

- открыть отдельную страницу проверки анализа;
- сопоставить каждый вывод и оценку ИИ с транскриптом и записью;
- подтвердить или исправить структурированные поля анализа;
- оставить содержательный комментарий при любой оценке, включая максимальную;
- сохранить незавершенный черновик;
- опубликовать итоговую человеческую версию;
- увидеть различия между ИИ и человеком;
- подать и разрешить апелляцию;
- восстановить полную историю решений без изменения старых записей.

Главный инвариант:

> Результат ИИ не перезаписывается. Исправление анализа создается как отдельная
> immutable human revision, привязанная к точной версии анализа и транскрипции.

## 2. Подтвержденное исходное состояние

В текущем backend подтверждены:

- `company_manager` и `department_leader` как реальные роли membership;
- области видимости звонка `personal`, `company`, `department`;
- активный результат в `call_analyses`;
- структурированный `result_json`, текстовый результат, provider и model;
- версия транскрипции и безопасный повторный анализ;
- evidence с диапазоном слов и таймкодами при однозначном совпадении;
- общий административный audit, который не является аудитом Human QA.

Не подтверждены и должны быть реализованы этой фичей:

- очередь проверок;
- ручная оценка рядом с оценкой ИИ;
- исправленная человеческая версия анализа;
- обязательные комментарии ко всем оценкам;
- публикация, возврат в работу и supersede ревизий;
- апелляции;
- предметный audit проверок;
- отдельная frontend-страница редактора анализа.

## 3. Границы первой версии

### 3.1. Входит в работу

- ручная проверка одного завершенного анализа звонка;
- отдельные черновик и опубликованные human revisions;
- исправление поддержанных структурированных полей;
- обязательный комментарий к каждому оцениваемому критерию;
- обязательный итоговый комментарий;
- положительный комментарий для максимальной оценки;
- очередь проверок с фильтрами и пагинацией;
- назначение проверяющего и безопасное взятие в работу;
- сравнение ИИ и human revision;
- апелляция на опубликованную ревизию;
- immutable event log;
- уведомления через существующий контур уведомлений;
- desktop, tablet и read-only mobile UI;
- backend/frontend tests, миграции, наблюдаемость и rollout.

### 3.2. Не входит в первый выпуск

- автоматическая калибровка проверяющих и inter-rater agreement;
- одновременная независимая проверка несколькими руководителями;
- конструктор карт оценки и versioned instruction rule set;
- внешняя CRM/task write-back;
- обучение сотрудников и gamification;
- автоматическое изменение prompt на основе комментариев;
- редактирование исходного аудио;
- юридически значимая электронная подпись.

Эти функции не должны блокироваться схемой данных первого выпуска.

## 4. Термины и принцип разделения данных

### 4.1. AI analysis

Неизменяемый результат конкретной успешной попытки анализа. Он содержит исходные
значения, сформированные моделью, и ссылки на использованные версии транскрипции и
инструкций, если такие ссылки доступны.

### 4.2. Quality review

Рабочий процесс проверки одного AI analysis. Содержит scope, назначение, текущий
статус и указатель на опубликованную human revision.

### 4.3. Human revision

Снимок решения человека. Черновик изменяем только через optimistic concurrency;
после публикации снимок immutable. Последующая правка создает новую ревизию.

### 4.4. Criterion decision

Решение по одному критерию: исходная оценка ИИ, человеческая оценка, статус
подтверждения, обязательный комментарий и ссылки на evidence.

### 4.5. Appeal

Формализованное возражение против последней опубликованной human revision. Апелляция
не меняет опубликованный результат до отдельного решения уполномоченного лица.

### 4.6. «Исправить анализ»

Это не прямой PATCH `call_analyses.result_json`. Операция создает или обновляет
human draft, а публикация формирует новую human revision. UI может называться
«Исправить анализ», но API и хранилище обязаны сохранять разделение источников.

## 5. Продуктовые правила

### 5.1. Комментарии обязательны всегда

При публикации:

- у каждого отображенного оцениваемого критерия должен быть непустой комментарий;
- правило одинаково для `0/10`, `10/10`, подтвержденной и измененной оценки;
- для максимальной оценки комментарий описывает, что выполнено хорошо;
- итоговый комментарий по звонку обязателен;
- строка только из пробелов невалидна;
- frontend не генерирует фиктивную похвалу от имени руководителя;
- разрешены выбираемые шаблоны положительного комментария, но пользователь должен
  явно выбрать или отредактировать текст до публикации;
- API повторяет всю валидацию независимо от frontend.

Черновик разрешено сохранять с пустыми или незавершенными комментариями. В ответе
backend возвращает `publication_blockers`, чтобы клиент не дублировал бизнес-логику.

Ограничения первой версии:

- комментарий критерия: после trim от 3 до 2000 Unicode code points;
- итоговый комментарий: от 3 до 5000 code points;
- причина изменения оценки включается в комментарий, отдельное обязательное поле не
  требуется;
- HTML запрещен; хранится plain text; ссылки автоматически не активируются.

### 5.2. Максимальная оценка

`10/10` не означает отсутствие обратной связи. Для максимальной оценки UI показывает
нейтральную подсказку: «Отметьте, что именно было сделано хорошо». Запрещено заранее
подставлять непроверенную похвалу вроде «Идеальная работа».

Если шкала критерия отличается от 10, максимальной считается `score == score_max`.
В API никогда не делать hard-code только на число `10`.

### 5.3. Подтверждение и исправление

Для критерия хранится `decision`:

- `confirmed` — human score равен AI score;
- `overridden` — human score отличается;
- `not_applicable` — критерий неприменим, если карта оценки это допускает;
- `unscored` — ИИ не дал числовой оценки, человек добавил комментарий и при
  разрешении карты может выставить оценку.

Backend вычисляет `decision`, а не доверяет значению от клиента.

### 5.4. Итоговая оценка

Итоговый human score вычисляется backend детерминированно из human criterion scores
по сохраненным весам/шкалам snapshot. Клиент не передает итог как источник истины.

Если текущий AI analysis не содержит достаточного контракта весов, первый выпуск:

- сохраняет критерии с их исходными `score`, `score_max` и `weight`;
- использует `weight=1` только как явно версионированный fallback;
- показывает пользователю предупреждение о равных весах;
- не меняет исходную AI total score;
- не смешивает критерии с разными шкалами без нормализации.

### 5.5. Редактируемые поля анализа

Первая версия поддерживает только allowlist полей:

- критерии и их оценки;
- комментарии/обоснования к критериям;
- резюме;
- сильные стороны;
- проблемы и рекомендации;
- риски;
- следующие шаги;
- результат разговора;
- evidence-ссылки к исправленным выводам.

Неизвестные поля AI JSON сохраняются в исходном анализе и отображаются read-only.
Запрещен универсальный редактор произвольного JSON.

Для каждого исправляемого поля хранится `field_key`, `ai_value`, `human_value` и
`change_kind`. Удаление значения представляется явно, а не пустой строкой.

### 5.6. Evidence

- подтвержденный или исправленный критерий может иметь 0..N evidence links;
- существующее matched evidence можно переиспользовать;
- новое evidence выбирается только из слов активной transcription revision;
- произвольные client timestamps не принимаются;
- backend проверяет word indices, порядок, timestamp и принадлежность call/revision;
- `ambiguous` и `not_found` evidence не получают seek target;
- отсутствие evidence не скрывается: UI показывает «Доказательство не приложено»;
- политика может требовать evidence для overridden решения; в первом выпуске это
  publication blocker для числового override, кроме `not_applicable`.

## 6. Права доступа

### 6.1. Матрица

| Действие | Сотрудник | Лидер отдела | Менеджер компании | Глобальный admin |
|---|---:|---:|---:|---:|
| Читать видимый AI analysis | по текущему ACL | да | да | только admin API |
| Читать опубликованный human result | если видит звонок | в своем отделе | в своей компании | только отдельное admin permission |
| Читать draft | нет | только свой или назначенный | в компании | нет по умолчанию |
| Создать/редактировать review | нет | звонок своего отдела | звонок своей компании | нет по умолчанию |
| Публиковать human revision | нет | звонок своего отдела | звонок своей компании | нет по умолчанию |
| Разрешить апелляцию | нет | если не автор спорной ревизии | да | нет по умолчанию |
| Подать апелляцию | см. 6.4 | см. 6.4 | см. 6.4 | нет |

### 6.2. Реальные названия ролей

Использовать существующие значения:

- `company_manager`;
- `department_leader`;
- `employee`.

Не вводить параллельные роли `manager`, `leader`, `reviewer`. Право reviewer является
вычисляемой capability из активного membership и scope звонка.

### 6.3. Scope

- `department` call: активный leader именно этого department или активный manager
  соответствующей company;
- `company` call: только активный company manager;
- `personal` call: Human QA недоступен в первом выпуске;
- uploader не получает право исправления только из-за загрузки файла;
- suspended/left membership немедленно теряет mutation access;
- смена роли не удаляет историю авторства;
- company manager может завершить draft бывшего лидера, создав новую ревизию от
  своего имени; выдавать себя за прежнего автора нельзя;
- global `admin`/`superadmin` не получают неявное продуктовое право.

Проверка выполняется на каждом read и write, а не только при открытии страницы.
Невидимый объект возвращает тот же внешний `404`, что и отсутствующий.

### 6.4. Кто может подать апелляцию

Апелляцию может подать активный пользователь, который:

- является `reviewed_subject_user_uuid`, явно назначенным review;
- либо загрузил звонок и видит его, если subject не назначен;
- либо является department leader/manager с доступом, но не автором текущей
  опубликованной ревизии.

Нельзя вычислять оцениваемого сотрудника из speaker label `manager`, имени или
результата ИИ. Subject задается UUID существующего активного участника компании и
может быть `NULL`, если звонок не является оценкой конкретного сотрудника.

### 6.5. Конфликт интересов

- автор опубликованной ревизии не может единолично разрешить апелляцию на нее;
- company manager может разрешить апелляцию на решение department leader;
- другой leader того же отдела может разрешить ее при сохраненной capability;
- если альтернативного решающего нет, апелляция остается `open`, а UI
  объясняет причину; система не автоотклоняет ее.

## 7. Состояния и переходы

### 7.1. Quality review

```text
unassigned -> assigned -> in_review -> published
     |           |           |            |
     +---------->+-----------+            +-> appealed -> resolved
                              \
                               -> canceled
```

Допустимые статусы:

- `unassigned`;
- `assigned`;
- `in_review`;
- `published`;
- `appealed`;
- `resolved`;
- `canceled`.

`canceled` допустим только до первой публикации. После публикации результат можно
supersede новой ревизией, но не удалять.

### 7.2. Human revision

- `draft` — редактируемая версия;
- `published` — immutable;
- `superseded` — опубликованная версия, замененная более новой;
- `voided` — только для подтвержденной технической/правовой ошибки, с обязательной
  причиной и отдельным событием; данные не удаляются.

### 7.3. Appeal

- `open`;
- `in_review`;
- `accepted`;
- `partially_accepted`;
- `rejected`;
- `withdrawn`.

Принятая или частично принятая апелляция обязана создать новую human revision.
Отклонение сохраняет текущую revision и требует комментарий решающего.

## 8. Модель данных backend

Имена миграций ниже ориентировочные; номер выбирается после текущей последней
миграции.

### 8.1. `call_quality_reviews`

```sql
review_uuid                    uuid primary key
call_uuid                      uuid not null references calls
analysis_uuid                  uuid not null references call_analyses
analysis_attempt_uuid          uuid null
transcription_revision         integer not null
company_uuid                   uuid not null references companies
department_uuid                uuid null references departments
reviewed_subject_user_uuid     uuid null references users
assignee_user_uuid             uuid null references users
status                         varchar not null
active_revision_uuid           uuid null
lock_version                   bigint not null default 1
due_at                         timestamptz null
created_by_user_uuid           uuid not null references users
created_at                     timestamptz not null
updated_at                     timestamptz not null
published_at                   timestamptz null
```

Ограничения:

- один активный review на `analysis_uuid`;
- department обязан принадлежать company;
- scope snapshot не меняется при последующей реорганизации компании;
- `transcription_revision` совпадает с источником analysis;
- `active_revision_uuid` указывает только на published revision этого review.

### 8.2. `call_quality_review_revisions`

```sql
revision_uuid                  uuid primary key
review_uuid                    uuid not null references call_quality_reviews
revision_number                integer not null
base_revision_uuid             uuid null
author_user_uuid               uuid not null references users
status                         varchar not null
overall_comment                text null
human_score                    numeric null
score_max                      numeric null
payload_json                   jsonb not null
source_hash                    varchar not null
expected_review_lock_version   bigint not null
created_at                     timestamptz not null
updated_at                     timestamptz not null
published_at                   timestamptz null
unique(review_uuid, revision_number)
```

Draft может существовать только один на пару `(review_uuid, author_user_uuid)`.
Published payload immutable на уровне repository/API. `source_hash` строится из
канонического AI analysis payload, analysis UUID, attempt UUID и transcription
revision, чтобы обнаруживать неправильную привязку.

### 8.3. `call_quality_review_criteria`

```sql
criterion_uuid                 uuid primary key
revision_uuid                  uuid not null references call_quality_review_revisions
criterion_key                  varchar not null
title_snapshot                 text not null
ai_score                       numeric null
human_score                    numeric null
score_min                      numeric null
score_max                      numeric null
weight                         numeric not null
decision                       varchar not null
comment                        text null
position                       integer not null
unique(revision_uuid, criterion_key)
```

`criterion_key` должен быть стабильным ключом из analysis contract. Если текущий
analyzer его не дает, backend формирует deterministic key из нормализованного title
и ordinal и сохраняет `key_origin=derived` в payload. Переименование title не должно
молча сопоставляться с другим критерием.

### 8.4. `call_quality_review_evidence`

Хранит `revision_uuid`, необязательный `criterion_uuid`/`field_key`, quote snapshot,
speaker snapshot, word start/end, seconds start/end, match status и transcription
revision. Все значения проверяются backend по каноническим словам.

### 8.5. `call_quality_review_appeals`

```sql
appeal_uuid                    uuid primary key
review_uuid                    uuid not null references call_quality_reviews
revision_uuid                  uuid not null references call_quality_review_revisions
author_user_uuid               uuid not null references users
status                         varchar not null
reason                         text not null
resolution_comment             text null
resolved_by_user_uuid          uuid null references users
created_at                     timestamptz not null
updated_at                     timestamptz not null
resolved_at                    timestamptz null
lock_version                   bigint not null default 1
```

Только одна открытая апелляция на опубликованную revision от одного автора. Повторная
отправка с тем же idempotency key возвращает исходный объект.

### 8.6. `call_quality_review_events`

Append-only журнал:

```sql
event_uuid                     uuid primary key
review_uuid                    uuid not null
revision_uuid                  uuid null
appeal_uuid                    uuid null
actor_user_uuid                uuid not null
actor_company_role_snapshot    varchar null
actor_department_role_snapshot varchar null
event_type                     varchar not null
before_json                    jsonb null
after_json                     jsonb null
reason                         text null
request_id                     varchar null
ip_address                     inet null
user_agent                     text null
created_at                     timestamptz not null
```

Обязательные события: create, assign, claim, draft_saved, publish, supersede,
appeal_opened, appeal_withdrawn, appeal_resolution_started, appeal_accepted,
appeal_rejected, subject_changed, due_date_changed, access_denied и void.

`access_denied` можно писать в security log без review UUID, если раскрытие объекта
недопустимо; продуктовый event log не должен создавать oracle существования.

## 9. Snapshot и устаревание

Review всегда привязан к конкретным:

- `analysis_uuid`/attempt;
- transcription revision;
- AI payload hash;
- критериям, шкалам и весам;
- evidence snapshot.

Если во время черновика выполнен повторный анализ:

- черновик не переносится автоматически;
- review получает `source_outdated=true`;
- публикация старого источника блокируется по умолчанию;
- руководитель может начать новый review на новом analysis;
- UI предлагает side-by-side перенос комментариев вручную;
- backend никогда не сопоставляет критерии только по позиции.

Если изменилась только speaker assignment/transcription revision, применяется то же
правило. Уже опубликованная revision остается исторически корректной и помечается
`based_on_previous_source`, но не становится недействительной автоматически.

## 10. API-контракт

Базовый prefix: `/api/v1`.

### 10.1. Очередь

```http
GET /quality-reviews?company_uuid=&department_uuid=&status=&assignee_uuid=&subject_uuid=&date_from=&date_to=&limit=&cursor=
```

Требования:

- cursor pagination со стабильным `(updated_at, review_uuid)`;
- серверный ACL до подсчета total;
- фильтры не раскрывают чужие отделы/пользователей;
- лимит 1..100, default 25;
- возвращать permissions/capabilities для каждой строки.

### 10.2. Создание review

```http
POST /calls/{call_uuid}/quality-reviews
Idempotency-Key: <uuid>

{
  "analysis_uuid": "uuid",
  "reviewed_subject_user_uuid": "uuid|null",
  "assignee_user_uuid": "uuid|null",
  "due_at": "RFC3339|null"
}
```

Ответ `201`; повтор с тем же ключом и payload — исходный `200/201`; тот же ключ с
другим payload — `409 idempotency_key_reused`.

### 10.3. Чтение

```http
GET /calls/{call_uuid}/quality-reviews
GET /quality-reviews/{review_uuid}
GET /quality-reviews/{review_uuid}/events?cursor=
GET /quality-reviews/{review_uuid}/revisions/{revision_uuid}
GET /quality-reviews/{review_uuid}/compare?left=&right=
```

Detail возвращает:

- source AI analysis;
- current draft, только если разрешено;
- active published human revision;
- publication blockers;
- source outdated state;
- appeals summary;
- capability flags: `can_claim`, `can_edit`, `can_publish`, `can_appeal`,
  `can_resolve_appeal`, `can_view_events`.

### 10.4. Claim и назначение

```http
POST  /quality-reviews/{review_uuid}/claim
PATCH /quality-reviews/{review_uuid}/assignment
```

Claim атомарен: два лидера не могут одновременно стать assignee. При конфликте
возвращается `409 review_already_claimed` с безопасным актуальным состоянием.
Manager может переназначить review с обязательной причиной.

### 10.5. Черновик

```http
PUT /quality-reviews/{review_uuid}/draft
If-Match: "review-lock-version"
Idempotency-Key: <uuid>
```

Payload содержит только allowlist human fields, criterion decisions, комментарии и
evidence word ranges. Backend:

1. повторно проверяет ACL и активный membership;
2. блокирует review;
3. проверяет `If-Match`;
4. проверяет source hash;
5. нормализует строки и числа;
6. валидирует evidence;
7. вычисляет decisions, score и blockers;
8. обновляет draft и lock version;
9. пишет event;
10. возвращает каноническое состояние.

`PUT` заменяет весь draft, чтобы удаленные на клиенте критерии не оставались
невидимыми. Autosave использует debounce и idempotency key.

### 10.6. Публикация

```http
POST /quality-reviews/{review_uuid}/publish
If-Match: "review-lock-version"
Idempotency-Key: <uuid>

{
  "draft_revision_uuid": "uuid"
}
```

Публикация в одной транзакции:

1. проверяет права и назначение;
2. блокирует review/draft;
3. убеждается, что source не устарел;
4. повторно вычисляет blockers;
5. запрещает публикацию при любом blocker;
6. делает draft immutable published revision;
7. supersede предыдущую active revision;
8. обновляет active pointer/status;
9. пишет события;
10. создает notifications после commit через надежный outbox/job механизм.

### 10.7. Апелляции

```http
POST /quality-reviews/{review_uuid}/appeals
POST /quality-review-appeals/{appeal_uuid}/claim
POST /quality-review-appeals/{appeal_uuid}/resolve
POST /quality-review-appeals/{appeal_uuid}/withdraw
```

Причина апелляции: 10..5000 code points. Resolve требует комментарий и решения по
каждому оспоренному критерию. Для `accepted`/`partially_accepted` backend публикует
новую human revision в той же транзакции или не меняет ничего.

### 10.8. Ошибки

Минимальные стабильные коды:

- `quality_review_not_found` — внешний 404;
- `quality_review_forbidden` — 403 только когда существование уже допустимо раскрыть;
- `quality_review_source_not_ready` — 409;
- `quality_review_source_outdated` — 409;
- `quality_review_already_exists` — 409;
- `quality_review_already_claimed` — 409;
- `quality_review_version_conflict` — 409;
- `quality_review_publication_blocked` — 422 со списком полей;
- `quality_review_comment_required` — 422;
- `quality_review_evidence_invalid` — 422;
- `quality_review_appeal_conflict` — 409;
- `quality_review_conflict_of_interest` — 403.

Ошибки не должны включать сырой AI payload, transcript или чужие UUID.

## 11. Backend-пакеты

Рекомендуемая структура без смешивания с admin audit:

```text
Monolit/internal/models/quality_review.go
Monolit/internal/repository/quality_review/
Monolit/internal/service/quality_review/
Monolit/internal/api/quality_review/
Monolit/internal/api/response/quality_review.go
Monolit/internal/repository/mocks/mock_quality_review_repository.go
Monolit/internal/service/mocks/mock_quality_review_service.go
```

Добавить интерфейсы в существующие агрегирующие `repository.go`, `service.go` и API
contracts, затем сгенерировать mocks штатным способом проекта.

Нельзя декодировать и произвольно мутировать JSON в HTTP handler. Нормализация,
allowlist, вычисление score и state machine принадлежат service/domain слою.

## 12. Frontend: информационная архитектура

### 12.1. Маршруты

```text
/quality-reviews                         очередь
/quality-reviews/:reviewUuid             отдельная страница проверки/исправления
/quality-reviews/:reviewUuid/history     история и сравнение
/quality-reviews/:reviewUuid/appeal      апелляция или ее разрешение
```

Кнопка «Исправить анализ» в карточке звонка:

- видна только при `can_create_quality_review` или `can_edit_quality_review`;
- для employee не показывается disabled-кнопка, раскрывающая лишние права;
- если review существует, ведет в него;
- если анализ processing/failed, показывает понятное read-only состояние;
- создание выполняется после явного подтверждения выбранного source analysis.

### 12.2. Страница проверки

Desktop layout:

- верхняя панель: звонок, сотрудник, отдел, статус, assignee, срок, source version;
- левая/центральная область: критерии и исправляемые поля;
- правая область: media player, transcript и evidence;
- sticky footer: состояние autosave, blockers, «Сохранить черновик», «Опубликовать»;
- переключатель «ИИ / Версия человека / Различия»;
- явные подписи источника у каждого значения, не только цвет.

Каждая карточка критерия содержит:

- title и шкалу;
- AI score read-only;
- human score control;
- computed badge «Подтверждено»/«Изменено»/«Неприменимо»;
- обязательный textarea комментария;
- счетчик символов и inline error;
- evidence list и «Добавить фрагмент»;
- переход к аудио только для валидного matched evidence.

При максимальной human score комментарий остается обязательным. Placeholder:
«Что именно выполнено хорошо?».

### 12.3. Отдельный редактор анализа

Исправление не размещать в тесной боковой панели звонка. Отдельная страница нужна
для:

- сохранения контекста source/human;
- одновременного transcript/media просмотра;
- больших комментариев;
- безопасной публикации;
- истории и апелляций;
- корректной работы browser back/refresh/deep link.

Нельзя использовать `contenteditable` для структурированных полей. Использовать
контролируемые inputs с типами, schema validation и явным состоянием удаления.

### 12.4. Autosave и несохраненные изменения

- локальные изменения помечаются dirty немедленно;
- autosave через 1–2 секунды после паузы, но не во время IME composition;
- одновременно выполняется только один save; следующий coalesced;
- save использует If-Match и idempotency key;
- при offline состояние хранится только в памяти/session storage без transcript и
  AI payload; чувствительные данные не писать в localStorage;
- закрытие страницы при unsaved changes вызывает browser warning;
- после успешного server save warning снимается;
- конфликт версии не перезаписывает сервер: показывается compare/refresh flow.

### 12.5. Публикация

Перед публикацией modal показывает:

- количество подтвержденных и измененных критериев;
- итоговую human score;
- отсутствующие comments/evidence;
- source analysis/version;
- предупреждение о необратимости published snapshot.

Кнопка disabled только вместе с видимым объяснением. Финальное решение принимает
backend; frontend обрабатывает новый blocker после race.

### 12.6. Очередь

Колонки desktop:

- звонок и дата;
- subject;
- company/department;
- AI score;
- human score;
- статус;
- assignee;
- due date;
- updated at.

Фильтры отражаются в URL. Сортировка стабильна. Empty state отличается для «нет
проверок» и «фильтр ничего не нашел». После claim строка обновляется из ответа API.

### 12.7. Апелляция

Пользователь видит:

- опубликованный вывод;
- собственную причину;
- оспариваемые критерии;
- статус и срок;
- решение с автором и комментарием.

Решающему показывается conflict-of-interest blocker до загрузки editor actions.
Принятие апелляции использует тот же typed human revision editor, а не отдельный
несовместимый payload.

### 12.8. Mobile и tablet

- mobile в первом выпуске read-only для published result и appeal status;
- редактирование на узком экране блокируется объяснением и ссылкой продолжить на
  desktop, если невозможно безопасно показать source и evidence;
- tablet допускает tabs «Оценка»/«Источник», сохраняя текущий criterion selection;
- никакой горизонтальной таблицы как единственного UI;
- touch targets не менее 44 CSS px.

### 12.9. Доступность

- все controls доступны с клавиатуры;
- errors связаны через `aria-describedby`;
- score не кодируется только цветом;
- live-region сообщает autosave/publish status без спама;
- focus после validation переходит к первому blocker;
- modal удерживает focus и возвращает его инициатору;
- media seek доступен кнопкой с понятным accessible name;
- изменения AI/human читаются screen reader как отдельные значения.

## 13. Corner cases

### 13.1. Данные и анализ

- анализ еще не завершен или failed;
- result JSON пустой, поврежденный или неизвестной schema version;
- нет критериев, есть только текстовое резюме;
- AI score отсутствует, выходит за шкалу или имеет дробное значение;
- два критерия имеют одинаковый title;
- критерий удален новой версией instructions;
- evidence ambiguous/not_found;
- аудио удалено, но transcript сохранен;
- transcript revision больше не активна;
- повторный анализ завершился во время редактирования;
- provider/model отсутствуют в legacy записи;
- старый анализ не содержит stable criterion keys.

Для неизвестной schema UI остается read-only и предлагает начать review только по
поддержанным полям. Нельзя терять исходный JSON.

### 13.2. Права и организация

- leader состоит в нескольких отделах;
- subject переведен в другой отдел после звонка;
- leader suspended во время открытой страницы;
- manager потерял роль перед publish;
- отдел soft-deleted;
- последний manager покинул компанию;
- assignee удален/left;
- call перемещен между папками;
- uploader не является subject;
- personal call ошибочно передан в create endpoint;
- admin пытается использовать обычный product endpoint.

Права проверяются по текущему активному membership, а scope/source snapshots
сохраняют исторический контекст. Потеря права запрещает mutation, но не переписывает
author snapshot.

### 13.3. Конкурентность

- два лидера одновременно claim;
- две вкладки одного автора autosave один draft;
- publish и autosave приходят одновременно;
- publish и reanalysis завершаются одновременно;
- appeal и superseding revision создаются одновременно;
- повтор POST после timeout;
- очередь доставила notification job дважды.

Использовать row locks, lock version, unique constraints, idempotency keys и outbox.
Last-write-wins для human decisions запрещен.

### 13.4. Frontend и сеть

- refresh после несохраненного изменения;
- offline во время save/publish;
- ответ save пришел не по порядку;
- 401 refresh session во время autosave;
- 403 после смены роли;
- 409 version conflict;
- 422 blockers изменились после открытия modal;
- длинный русский комментарий, emoji и combining characters;
- вставка HTML/script;
- IME composition;
- media URL истек;
- browser back/forward восстанавливает фильтры очереди.

### 13.5. Удаление и retention

- удаление call должно каскадно или policy-driven обрабатывать review artifacts;
- legal hold/retention имеет приоритет над обычным delete;
- удаление пользователя не удаляет audit author identity: хранится UUID и безопасный
  display snapshot согласно политике персональных данных;
- экспорт данных включает опубликованные решения и апелляции в разрешенном scope;
- draft должен иметь отдельный retention, например 90 дней неактивности, но job не
  включать без утвержденной продуктовой политики.

## 14. Безопасность и приватность

- parameterized SQL;
- object-level authorization на каждом endpoint;
- CSRF-модель соответствует текущей cookie/session архитектуре;
- rate limit на mutation и appeal endpoints;
- plain-text sanitation и output escaping;
- запрет логирования comments, transcript, AI payload и quotes;
- structured logs содержат UUID, status, latency, error code и counts;
- audit events append-only; UPDATE/DELETE запрещены repository контрактом;
- media/evidence URLs выдаются с текущей ACL и ограниченным сроком;
- экспорт review доступен только в исходном scope;
- frontend cache/query keys включают user/scope и очищаются при logout;
- запрещено передавать Human QA comments внешнему AI provider без отдельного opt-in;
- analytics/telemetry не содержит содержимого разговоров и комментариев.

## 15. Наблюдаемость

Метрики без high-cardinality labels:

- reviews created/claimed/published/appealed/resolved;
- draft save latency/error/conflict rate;
- publish blockers по коду;
- время от создания до claim/publish;
- доля confirmed/overridden/not_applicable;
- разница AI и human score агрегированно;
- доля максимальных оценок с валидным комментарием;
- appeals acceptance rate;
- notification outbox lag/failures;
- source outdated count.

Не использовать `call_uuid`, `user_uuid`, текст комментария или criterion title как
metric labels.

Logs/traces должны связываться через request ID и review UUID с соблюдением ACL.
Добавить alert на устойчивый рост publish failures, outbox lag и DB conflicts.

## 16. Уведомления

Минимальные события:

- review назначен;
- приближается/просрочен due date;
- review опубликован;
- апелляция открыта;
- апелляция назначена;
- апелляция разрешена;
- source устарел до публикации.

Уведомление содержит минимальные metadata и deep link, но не текст комментария или
цитату. Проверка доступа повторяется при открытии ссылки. Дубликаты подавляются по
event UUID.

## 17. Производительность

- очередь использует индексы по company, department, status, assignee, subject,
  updated_at;
- detail не загружает весь event history;
- transcript/media загружаются отдельно и лениво;
- payload revision имеет разумный hard limit, например 1 MiB после согласования с
  текущим максимальным analysis payload;
- comments и criteria валидируются до тяжелых DB операций;
- compare строится по двум snapshots без N+1;
- evidence selection не отправляет весь массив words обратно при каждом autosave;
- API использует gzip/br, ETag/If-Match и cursor pagination в рамках текущего стека.

## 18. Совместимость и миграции

### 18.1. Expand

1. Создать новые таблицы, constraints и индексы.
2. Не менять существующий `call_analyses.result_json`.
3. Добавить nullable ссылки на attempt/instruction snapshot, если их еще нет.
4. Развернуть backend read/API за feature flag.
5. Развернуть frontend без включения mutations.

### 18.2. Enable

1. Включить создание review только для внутренних/pilot companies.
2. Проверить реальные analysis schema variants.
3. Собрать метрики conflicts/blockers/latency.
4. Включить publish и notifications.
5. Затем включить appeals.

### 18.3. Rollback

Отключение feature flag прекращает новые mutations, но:

- не удаляет данные;
- сохраняет read-only доступ managers к опубликованным revisions;
- не меняет AI analysis;
- pending outbox events либо безопасно завершаются, либо переводятся в explicit
  canceled с operational audit.

Down migration допустима только до появления production данных. После enable rollback
является application rollback, не DROP TABLE.

## 19. Матрица тестирования backend

### 19.1. Domain/unit

- state machine принимает только допустимые переходы;
- comments обязательны при publish для всех score, включая max;
- Unicode code point limits и trim;
- score range, decimal и scale normalization;
- decision вычисляется backend;
- total score и weights;
- publication blockers;
- allowlist полей;
- canonical hash;
- source outdated;
- conflict-of-interest;
- evidence validation.

### 19.2. Repository/integration

- unique active review per analysis;
- transactional publish и supersede;
- row lock/optimistic conflict;
- concurrent claim;
- idempotency reuse/replay;
- append-only events;
- appeal resolution создает ровно одну revision;
- rollback при failure между revision и active pointer;
- outbox создается в одной транзакции;
- indexes используются для representative queue query;
- soft-deleted membership/department cases;
- cleanup/retention policy.

DB-sharing integration tests запускать последовательно.

### 19.3. API/security

- department leader только своего отдела;
- manager только своей компании;
- employee mutation forbidden;
- personal call forbidden;
- suspended/left forbidden;
- admin не получает implicit access;
- invisible object не различим от missing;
- stale If-Match;
- invalid/oversized payload;
- HTML/script безопасно возвращается как текст;
- rate limits;
- cursor/filter ACL;
- повтор запросов после timeout.

## 20. Матрица тестирования frontend

- route guards и capability-driven actions;
- отдельная страница открывается deep link;
- AI/human/diff modes;
- обязательный comment на 10/10;
- draft с пустым comment сохраняется, publish блокируется;
- max score placeholder не является сохраненным значением;
- autosave debounce, coalescing и out-of-order response;
- version conflict flow без потери локального текста;
- source outdated banner/block;
- offline/401/403/409/422/5xx;
- evidence seek и отсутствие seek для unmatched;
- publish modal и focus management;
- queue URL filters, pagination и empty states;
- appeal states/conflict-of-interest;
- mobile read-only/tablet tabs;
- keyboard/screen reader/contrast;
- logout очищает sensitive query cache.

## 21. Browser QA

Обязательные сквозные сценарии:

1. Leader берет department review, подтверждает все критерии, выставляет 10/10,
   оставляет положительные комментарии и публикует.
2. Публикация 10/10 без комментария блокируется frontend и backend.
3. Leader изменяет оценку, прикладывает фрагмент, публикует; manager видит diff.
4. Employee видит опубликованный результат, но не редактор/draft.
5. Subject подает appeal; автор исходной revision не может ее разрешить; manager
   принимает и создается новая revision.
6. Две вкладки создают version conflict без silent overwrite.
7. Reanalysis делает draft outdated и блокирует публикацию.
8. Роль leader снимается во время страницы; следующий save получает безопасный
   отказ, локальный текст не отправляется повторно бесконечно.
9. Media отсутствует: проверка остается доступной, evidence честно read-only.
10. Узкий viewport не ломает чтение опубликованного результата.

Проверять не только наличие DOM, но и реальный API payload, persisted revisions,
seek geometry, keyboard navigation и reload после publish.

## 22. Порядок реализации

### Этап 0. Контракт анализа

- инвентаризировать реальные варианты `result_json`;
- определить schema version и stable criterion keys;
- зафиксировать score/weight fallback;
- завершить quote-to-audio frontend для evidence проверки.

### Этап 1. Backend review core

- migrations и модели;
- repository и state machine;
- ACL capability service;
- create/list/detail/claim/draft/publish;
- event log и idempotency;
- unit/integration/API tests.

### Этап 2. Frontend review editor

- queue;
- отдельный editor route;
- criteria/comments/evidence;
- autosave/concurrency;
- publish/diff/history;
- responsive/read-only modes;
- browser QA.

### Этап 3. Апелляции и уведомления

- appeal lifecycle и conflict-of-interest;
- revision on acceptance;
- outbox notifications;
- deadlines и overdue states;
- security/browser tests.

### Этап 4. Production rollout

- feature flags;
- pilot companies;
- dashboards/alerts;
- backup/restore drill;
- нагрузочная проверка очереди и autosave;
- обновление README и API documentation.

## 23. CI и критерии завершения

Backend:

- formatting, lint, unit, repository, integration, race-sensitive targeted tests;
- migration up/down на пустой БД и up на snapshot текущей схемы;
- OpenAPI/API contract checks, если генератор присутствует;
- mock generation clean;
- `git diff --check`;

Frontend:

- typecheck;
- lint;
- unit/component tests;
- production build;
- browser smoke desktop/tablet/mobile;
- accessibility smoke;
- проверка реальных backend payloads, не только mocks.

## 24. Definition of Done

Фича завершена только если одновременно выполнено следующее:

- AI analysis остается неизменным и доступен для сравнения;
- leader своего отдела и manager компании могут открыть отдельный editor;
- employee не может редактировать анализ;
- personal call не попадает в Human QA первого выпуска;
- draft сохраняется независимо от готовности к публикации;
- публикация требует комментарий у каждого критерия при любой оценке;
- 10/10 без валидного комментария не публикуется; UI явно просит отметить сильную
  сторону, но backend не пытается недостоверно определять «положительность» текста;
- backend вычисляет decisions и итоговую оценку;
- evidence проверяется по точной transcription revision;
- published revision immutable;
- повторная правка создает новую revision и supersede предыдущую;
- optimistic concurrency исключает silent overwrite;
- reanalysis корректно помечает draft outdated;
- апелляция имеет полный lifecycle и conflict-of-interest protection;
- все существенные действия присутствуют в append-only event log;
- notifications идемпотентны;
- права повторно проверяются на каждом endpoint;
- очередь ACL-aware и масштабируется индексами;
- frontend корректно обрабатывает loading/empty/error/offline/conflict states;
- browser QA подтверждает сохраненные данные, reload и реальный media evidence;
- миграции, backend CI и frontend CI проходят;
- README/API documentation обновлены;

## 25. Решения, которые нельзя менять молча

Перед отклонением от этой спецификации требуется отдельное продуктовое/техническое
решение по следующим пунктам:

1. Исходный AI analysis не перезаписывается.
2. Комментарий обязателен для каждого критерия при любой оценке, включая максимум.
3. Исправление выполняется на отдельной странице.
4. Mutations доступны только активным `department_leader` в своем отделе и
   `company_manager` в своей компании.
5. Глобальный admin не получает неявное продуктовое право.
6. Published revisions и event history immutable.
7. Reanalysis не переносит draft автоматически на новый источник.
8. Принятая апелляция создает новую human revision.
