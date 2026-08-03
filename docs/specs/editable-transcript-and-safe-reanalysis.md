# Спецификация: исправление транскрипции и участников с безопасным повторным анализом

Статус: ready for implementation
Область: `VerbaTrace/Monolit` и `C:\projects\VerbaTrace-frontend`
Тип изменения: версионируемое редактирование транскрипции, аудит и атомарная замена анализа

## 1. Цель

Дать пользователю возможность исправить ошибочно распознанные слова и говорящих,
после чего запустить новый анализ по исправленной версии транскрипции.

После реализации пользователь может:

- включить режим редактирования в карточке транскрипции;
- исправить текст распознанного слова;
- изменить говорящего у одного слова или выбранного непрерывного диапазона;
- сохранить исправления с причиной;
- увидеть номер версии и историю изменений;
- повторно проанализировать звонок по конкретной сохраненной версии;
- продолжать видеть старый готовый анализ, пока новый анализ не завершен;
- после успеха получить новый анализ и новые evidence-связи;
- при ошибке повторного анализа не потерять предыдущий готовый результат.

Главный принцип: исправление текста не создает новые факты о времени. В первой
production-версии редактора
разрешено менять текст и говорящего существующего слова, но нельзя добавлять,
удалять, объединять или разделять элементы `words`. Сохраненные ASR-таймкоды и
индексы остаются неизменными. Это не временное упрощение качества: структурные
правки выпускаются отдельным этапом после введения стабильных token UUID и явной
модели происхождения таймкодов, описанной ниже.

## 2. Подтвержденное исходное состояние

- `call_transcriptions` содержит одну запись на звонок и хранит `text`, `segments`
  и `words`.
- `GET /api/v1/calls/{uuid}/transcription` возвращает сегменты и слова.
- `POST /api/v1/calls/{uuid}/analysis` ставит асинхронную задачу `analyze_call`.
- `call_analyses` содержит одну запись на звонок; текущий `Create` использует
  `ON CONFLICT (call_uuid) DO UPDATE` и очищает предыдущий результат до завершения
  нового анализа.
- evidence обогащается после ответа модели точным сопоставлением с `words`.
- frontend уже умеет перематывать media к evidence и подсвечивать активное слово.
- существующий admin audit предназначен для административных операций и не должен
  переиспользоваться как продуктовый аудит транскрипций.

Следствие: простого `UPDATE call_transcriptions` и повторного вызова текущего
analysis endpoint недостаточно. Нужны версии транскрипции, optimistic locking и
отдельная попытка анализа, которая не уничтожает активный результат до успеха.

## 3. Production scope и этапы поставки

Фича проектируется как постоянная часть продукта. Поставка разделяется на этапы,
но каждый включенный этап должен быть эксплуатационно завершенным: с миграциями,
ACL, аудитом, метриками, резервным восстановлением, документацией и проверенным
rollback. Наличие следующего этапа не позволяет оставлять текущий в частично
рабочем состоянии.

### 3.1. Входит в работу

- исправление `text` существующих word-elements;
- изменение `speaker` существующего слова или непрерывного диапазона слов;
- серверная пересборка `text` и `segments` из исправленного массива `words`;
- immutable revision с безопасной ссылкой на дедуплицируемое содержимое;
- причина, автор и время изменения;
- optimistic locking по `revision`;
- история версий без текста в списочном ответе;
- безопасный повторный анализ по зафиксированной версии;
- сохранение старого активного анализа до успешного завершения нового;
- атомарное продвижение успешного результата в активный анализ;
- отмена продвижения результата, если версия транскрипции устарела;
- обновление frontend-состояния, API-типов и видимого сценария;
- backend, frontend, integration и browser tests;
- README и API-примеры.

### 3.2. Следующий обязательный этап: структурное редактирование

- добавление, удаление, объединение и разделение слов через стабильные token UUID;
- редактирование транскрипций без пословных данных на уровне сегментов;
- полноценный diff двух версий и выбор любой существующей версии как активной без создания новой revision;
- комментарий к диапазону и классификация причины исправления;
- пакетное исправление speaker и терминов по звонку с обязательным preview;
- экспорт транскрипции конкретной revision;
- API для получения полного snapshot версии при наличии права чтения транскрипта.

Для структурного редактирования исходные ASR-слова остаются immutable. Редактор
работает с производными tokens, каждый из которых хранит:

- стабильный `token_uuid`;
- `source_word_indexes`;
- `timing_status`: `source`, `range_derived` или `unavailable`;
- nullable временной диапазон;
- автора и revision происхождения.

При split части наследуют диапазон исходного слова с `timing_status=range_derived`,
но не получают выдуманных внутренних границ. При merge используется общий диапазон
источников. Вставленный без источника token имеет `timing_status=unavailable` и не
может самостоятельно стать кликабельным evidence. Удаление скрывает token в новой
revision, не уничтожая исходный snapshot.

### 3.3. Отдельные продуктовые модули, не являющиеся частью этой фичи

- изменение исходных ASR-таймкодов и `confidence`;
- автоматическое определение правильного говорящего;
- пользовательский словарь и обучение ASR-провайдера;
- ручная оценка анализа, override и апелляция;
- автоматический повторный анализ при каждом сохранении;
- изменение исходного media-файла;
- синхронное посимвольное совместное редактирование. Production-продукт поддерживает
  безопасную конкурентную работу через revisions, presence и уведомление о
  конфликте; CRDT/OT добавляется только при подтвержденной пользовательской
  необходимости.

Для старой транскрипции с `words: []` frontend показывает объяснение:
«Эта транскрипция создана без пословных данных и пока недоступна для исправления».
Система не должна придумывать таймкоды путем деления текста по длительности.

## 4. Термины и инварианты

### 4.1. Ревизия транскрипции

Положительное целое число, монотонно увеличиваемое при каждом успешном исправлении.
Исходный результат ASR имеет `revision=1`.

### 4.2. Активная версия

Текущее содержимое `call_transcriptions`. Исторические снимки хранятся отдельно и
не изменяются.

### 4.3. Попытка анализа

Отдельная асинхронная сущность, привязанная к точной ревизии транскрипции. До
успешного продвижения она не заменяет доступный пользователю готовый анализ.

### 4.4. Инварианты

1. В первой production-версии число и порядок элементов `words` не меняются между
   ревизиями; после введения tokens исходный массив ASR words всегда immutable.
2. `start_seconds`, `end_seconds`, `confidence` не изменяются пользователем.
3. Исправленный `text` и `segments` всегда вычисляет backend.
4. Evidence строится по snapshot той же ревизии, что использовалась анализатором.
5. Результат устаревшей ревизии никогда не становится активным.
6. Неуспешная попытка не удаляет предыдущий готовый анализ.
7. Ни полный текст, ни исправленные слова не попадают в application logs.

## 5. Модель данных

### 5.1. Расширение `call_transcriptions`

Добавить:

```sql
ALTER TABLE call_transcriptions
    ADD COLUMN revision INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN edited BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN last_edited_by_user_uuid UUID NULL REFERENCES users(user_uuid),
    ADD COLUMN last_edited_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT chk_call_transcriptions_revision CHECK (revision >= 1);
```

`updated_at` остается техническим временем обновления записи. Для пользовательского
контракта конкурентности используется `revision`, а не строковое время.

### 5.2. Разделение revision и содержимого

Revision хранит аудит и ссылку, но не дублирует полный текст. Содержимое хранится в
отдельном content-addressed слое, ограниченном одним `transcription_uuid`.

Дедупликация между разными звонками и компаниями запрещена, даже если хэши совпали.
Это исключает cross-tenant probing, при котором наличие известного хэша могло бы
подтвердить содержание чужого разговора. Экономия достигается для повторов и
restore внутри одного звонка, где ACL уже общий.

### 5.3. `call_transcription_contents`

```sql
CREATE TABLE call_transcription_contents (
    transcription_content_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    canonicalization_version SMALLINT NOT NULL,
    content_sha256 BYTEA NOT NULL,
    canonical_size_bytes BIGINT NOT NULL,
    storage_kind VARCHAR(16) NOT NULL,
    base_content_uuid UUID NULL REFERENCES call_transcription_contents(transcription_content_uuid),
    checkpoint_payload JSONB NULL,
    patch_payload JSONB NULL,
    chain_depth SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_transcription_content_sha256 CHECK (octet_length(content_sha256) = 32),
    CONSTRAINT chk_transcription_content_size CHECK (canonical_size_bytes > 0),
    CONSTRAINT chk_transcription_content_kind CHECK (storage_kind IN ('checkpoint', 'patch')),
    CONSTRAINT chk_transcription_content_depth CHECK (chain_depth BETWEEN 0 AND 20),
    CONSTRAINT chk_transcription_content_shape CHECK (
        (storage_kind = 'checkpoint' AND base_content_uuid IS NULL
            AND checkpoint_payload IS NOT NULL AND patch_payload IS NULL AND chain_depth = 0)
        OR
        (storage_kind = 'patch' AND base_content_uuid IS NOT NULL
            AND checkpoint_payload IS NULL AND patch_payload IS NOT NULL AND chain_depth > 0)
    )
);

CREATE INDEX idx_call_transcription_contents_hash_candidates
    ON call_transcription_contents (
        transcription_uuid,
        canonicalization_version,
        content_sha256,
        canonical_size_bytes
    );
```

Индекс намеренно не `UNIQUE`: SHA-256 существенно снижает вероятность случайной
коллизии, но не является доказательством равенства и не должен становиться
единственной границей целостности. При совпадении hash+size backend реконструирует
кандидат и выполняет точное побайтовое сравнение canonical representation. Для
длинных транскрипций сравнение и вычисление hash выполняются потоково с ограниченным
буфером; backend не держит одновременно несколько полных сериализованных копий в
RAM. Ссылка
переиспользуется только при полном побайтовом равенстве. При несовпадении создается
отдельный content row с тем же хэшем; операция не падает и не перезаписывает данные.

`base_content_uuid` обязан принадлежать тому же `transcription_uuid`. Обычный FK это
не гарантирует, поэтому repository проверяет принадлежность под блокировкой, а
схема дополнительно использует составной FK/UNIQUE в итоговой migration. Циклы
запрещены направлением только на уже существующий content и проверкой глубины.

### 5.4. Каноническое представление и hash

Хэш считается не от произвольного входного JSON и не от PostgreSQL textual output.
Backend сначала строит внутреннюю валидированную структуру точной версии:

```json
{
  "schema_version": 1,
  "text": "...",
  "segments": [],
  "words": []
}
```

Затем применяет версионированную детерминированную канонизацию:

- UTF-8 без BOM;
- фиксированный порядок полей структур;
- сохранение порядка массивов;
- однозначное представление JSON strings, integers, booleans и null;
- запрет `NaN`, `Infinity`, отрицательного нуля и чисел вне принятого диапазона;
- времена нормализуются один раз в целое число микросекунд перед сериализацией;
- отсутствие поля и `null` не считаются автоматически одинаковыми;
- canonicalization version входит в область поиска и никогда не меняет правила
  задним числом.

Нужно использовать одну audited реализацию канонизации для записи, проверки,
restore и анализа, с golden vectors. Hash вычисляется как SHA-256 от canonical
bytes. Эти bytes не принимаются от клиента.

### 5.5. Checkpoint + patch без неограниченных цепочек

Полный snapshot не записывается при каждом небольшом исправлении:

- `checkpoint` содержит полное валидированное состояние;
- `patch` содержит минимальный серверный набор операций над стабильными word/token
  identifiers и ссылается на предыдущий content;
- максимальная глубина цепочки — 20;
- новый checkpoint создается раньше, если patch превышает 40% canonical размера
  полного состояния или реконструкция превышает установленный latency budget;
- значения 20 и 40% являются начальными инженерными параметрами и подтверждаются
  benchmark на production-like данных до релиза;
- active `call_transcriptions` остается материализованным представлением для
  обычного чтения и не требует реконструкции цепочки;
- после реконструкции backend повторно вычисляет hash и size; несовпадение означает
  corruption, данные не выдаются и создается security/operations alert;
- patch имеет строгую типизированную schema, лимиты числа операций и размера; JSON
  Patch общего назначения от клиента не сохраняется.

Таким образом, одинаковые состояния используют один content row, близкие версии
обычно хранят компактный patch, а время чтения истории ограничено checkpoint depth.

### 5.6. `call_transcription_revisions`

```sql
CREATE TABLE call_transcription_revisions (
    transcription_revision_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    transcription_content_uuid UUID NOT NULL REFERENCES call_transcription_contents(transcription_content_uuid),
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    reason TEXT NOT NULL,
    changed_word_indexes JSONB NOT NULL,
    restored_from_revision INTEGER NULL,
    created_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (transcription_uuid, revision),
    CONSTRAINT chk_transcription_revision_positive CHECK (revision >= 1),
    CONSTRAINT chk_transcription_revision_reason CHECK (length(btrim(reason)) BETWEEN 3 AND 500),
    CONSTRAINT chk_transcription_revision_changed_indexes CHECK (jsonb_typeof(changed_word_indexes) = 'array')
);

CREATE INDEX idx_call_transcription_revisions_call_revision
    ON call_transcription_revisions (call_uuid, revision DESC);
```

`transcription_content_uuid` также обязан принадлежать тому же
`transcription_uuid`; это закрепляется составным FK. Revision immutable на уровне
repository и database privileges: application role имеет `INSERT/SELECT`, но не
`UPDATE/DELETE` отдельных revisions. Удаление допускается только каскадом при
удалении звонка или отдельной проверенной retention procedure.

При migration для существующих `transcribed` записей с непустыми `words` создать
checkpoint content и revision 1. Для исходной версии:

- `reason = 'Исходная транскрипция ASR'`;
- `created_by_user_uuid = NULL`;
- `changed_word_indexes = '[]'`.

Production migration выполняет возобновляемый batch backfill без длительной
блокировки таблицы. Lazy creation допустим только как совместимый fallback во время
rollout, но API не должен отдавать историю, в которой отсутствует revision 1.

### 5.7. `call_analysis_attempts`

```sql
CREATE TABLE call_analysis_attempts (
    analysis_attempt_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    transcription_revision_uuid UUID NOT NULL REFERENCES call_transcription_revisions(transcription_revision_uuid),
    transcription_revision INTEGER NOT NULL,
    requested_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    status VARCHAR(16) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    model VARCHAR(128) NULL,
    result_json JSONB NULL,
    result_text TEXT NULL,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_call_analysis_attempt_status
        CHECK (status IN ('pending', 'processing', 'done', 'failed', 'superseded'))
);

CREATE INDEX idx_call_analysis_attempts_call_created
    ON call_analysis_attempts (call_uuid, created_at DESC);
CREATE UNIQUE INDEX uq_call_analysis_attempt_active
    ON call_analysis_attempts (call_uuid)
    WHERE status IN ('pending', 'processing');
```

Добавить в `call_analyses`:

```sql
ALTER TABLE call_analyses
    ADD COLUMN transcription_revision INTEGER NULL,
    ADD COLUMN source_attempt_uuid UUID NULL REFERENCES call_analysis_attempts(analysis_attempt_uuid);
```

Существующие результаты получают `transcription_revision = 1`, если соответствующая
транскрипция существует. Если это нельзя подтвердить, значение остается `NULL` и
frontend показывает «Версия транскрипции не зафиксирована».

### 5.8. Почему нужен отдельный attempt

Текущий upsert `call_analyses` очищает `result_json` при новом запуске. Для safe
reanalysis это поведение недопустимо. Новая попытка сначала живет в
`call_analysis_attempts`; только готовый и актуальный результат атомарно копируется
в `call_analyses`.

## 6. API-контракт

### 6.1. Расширение чтения транскрипции

`GET /api/v1/calls/{uuid}/transcription` дополнительно возвращает:

```json
{
  "revision": 3,
  "edited": true,
  "editable": true,
  "last_edited_by": {
    "user_uuid": "...",
    "display_name": "Дмитрий Мухачев"
  },
  "last_edited_at": "2026-08-02T18:30:00Z"
}
```

`editable=false`, если транскрипция не `transcribed`, `words` пусты или у пользователя
нет права изменения. Причина возвращается машинным кодом `editability_reason`:
`not_transcribed`, `words_unavailable`, `forbidden`.

### 6.2. Сохранение исправлений

`PATCH /api/v1/calls/{uuid}/transcription`

```json
{
  "expected_revision": 2,
  "reason": "Исправлены имя клиента и говорящий",
  "edits": [
    { "word_index": 17, "text": "Александр" },
    { "word_index": 28, "speaker": "Менеджер" },
    { "word_index": 29, "speaker": "Менеджер" }
  ]
}
```

Правила:

- `expected_revision >= 1`;
- от 1 до 500 уникальных `word_index` за один запрос;
- индекс должен существовать в активной версии;
- edit обязан менять `text`, `speaker` или оба поля;
- `text` после trim содержит 1–200 Unicode code points и не содержит перевод строки;
- `speaker` после trim содержит 1–100 code points; пустая строка допустима только как
  явное удаление неподтвержденной подписи говорящего;
- неизменившийся edit отклоняется как `400 no_transcription_changes`;
- duplicate indexes отклоняются, а не молча объединяются;
- reason после trim имеет длину 3–500 code points;
- неизвестные JSON-поля и trailing JSON запрещены;
- тело запроса ограничено по размеру;
- backend не принимает времена, confidence, полный `words`, `segments` или `text`.

Успешный ответ: `200` с полной новой `TranscriptionResponse` и `revision=N+1`.

Ошибки:

- `400 invalid_transcription_edit`;
- `400 no_transcription_changes`;
- `403 transcription_edit_forbidden`;
- `404 call_or_transcription_not_found`;
- `409 transcription_revision_conflict` с текущим `revision`;
- `409 transcription_not_editable`;
- `409 analysis_in_progress`;
- `413 transcription_edit_too_large`.

### 6.3. История

`GET /api/v1/calls/{uuid}/transcription/revisions?limit=20&offset=0`

Список содержит метаданные, но не полный snapshot:

```json
{
  "items": [
    {
      "revision": 3,
      "reason": "Исправлен говорящий",
      "changed_word_indexes": [28, 29],
      "changed_words_count": 2,
      "created_by": { "user_uuid": "...", "display_name": "..." },
      "created_at": "2026-08-02T18:30:00Z"
    }
  ],
  "total": 3
}
```

Полный snapshot версии отдается отдельным защищенным endpoint:

`GET /api/v1/calls/{uuid}/transcription/revisions/{revision}`

Он использует тот же ACL, что активная полная транскрипция, поддерживает `ETag`,
ограничение размера ответа и возвращает `Cache-Control: private, no-store`.

Восстановление выполняется через:

`POST /api/v1/calls/{uuid}/transcription/revisions/{revision}/restore`

Операция не переписывает историю и не создаёт новую revision. В отдельной таблице
`call_transcription_revision_state` атомарно переключается `active_revision`, а
`call_transcriptions` обновляется материализованным содержимым выбранной версии.
Переключение защищено `expected_revision`, ACL и audit-событием, но не требует от
пользователя указывать причину. Повторный выбор уже активной версии идемпотентен.

### 6.4. Повторный анализ

Расширить существующий endpoint без изменения базового пути:

`POST /api/v1/calls/{uuid}/analysis`

```json
{
  "transcription_revision": 3
}
```

Для обратной совместимости пустое тело означает текущую активную ревизию. Endpoint:

1. проверяет доступ к звонку;
2. проверяет, что запрошенная ревизия является текущей;
3. запрещает второй активный attempt;
4. создает `call_analysis_attempts` со ссылкой на immutable snapshot;
5. ставит job с `entity_uuid = analysis_attempt_uuid`;
6. возвращает `202 Accepted` и attempt DTO.

```json
{
  "attempt_uuid": "...",
  "call_uuid": "...",
  "transcription_revision": 3,
  "status": "pending",
  "created_at": "..."
}
```

На первом выпуске frontend может получать состояние attempt через:

`GET /api/v1/calls/{uuid}/analysis/attempt`

Он возвращает последнюю попытку и сведения об активном анализе. Существующий
`GET /api/v1/calls/{uuid}/analysis` продолжает возвращать активный результат и
дополняется полями `transcription_revision` и `reanalysis_attempt`.

## 7. Права доступа

Чтение остается равным текущей видимости звонка. Право изменения строже:

- personal call: загрузивший пользователь;
- company call: загрузивший пользователь, `owner`, `admin` или `manager` компании;
- department call: загрузивший пользователь, `owner`/`admin` компании или manager
  соответствующего department;
- superadmin не получает неявное продуктовое право через admin endpoints.

Точные названия ролей необходимо сверить с существующей моделью и использовать
текущие membership checks, не создавать параллельную ACL-логику.

Одинаковая проверка применяется к PATCH, истории и повторному анализу. Нельзя
раскрывать существование невидимого звонка через различия между `403` и `404`.

## 8. Backend: применение исправлений

Операция выполняется в одной транзакции:

1. Загрузить звонок с проверкой права изменения.
2. Взять `call_transcriptions` `FOR UPDATE`.
3. Сравнить `expected_revision` с текущим значением.
4. Убедиться, что нет pending/processing analysis attempt.
5. Декодировать и проверить `words`.
6. Применить edits к копии массива.
7. Проверить неизменность порядка и временных полей.
8. Пересобрать канонический `text`.
9. Пересобрать `segments`, группируя соседние слова с одинаковым `speaker`.
10. Канонизировать новое состояние и найти content candidates только внутри текущего
    `transcription_uuid` по version+SHA-256+size.
11. При точном равенстве переиспользовать content UUID; иначе записать bounded patch
    или checkpoint по правилам раздела 5.5.
12. Создать immutable revision `N+1`, ссылающуюся на content.
13. Обновить активную транскрипцию, `revision`, editor metadata и `updated_at`.
14. Зафиксировать транзакцию.

### 8.1. Пересборка текста

Не использовать простое `strings.Join(words, " ")`: это ломает пунктуацию.
Создать один общий deterministic renderer, покрытый русской и английской
пунктуацией. Он должен:

- не ставить пробел перед `.,!?;:%)]}`;
- не ставить пробел после открывающих `([{`;
- сохранять самостоятельные тире и кавычки читаемо;
- не менять сохраненное `word.text` ради форматирования;
- использоваться и для общего текста, и для текста сегмента.

### 8.2. Пересборка сегментов

- соседние слова с одинаковым непустым speaker образуют сегмент;
- начало/конец берутся из первого/последнего слова;
- при пустом speaker допустим сегмент без подписи, но frontend не должен выдавать
  его за диаризованный диалог;
- исходные границы сегментов не считаются каноническими после исправления speaker.

## 9. Backend: безопасный повторный анализ

Worker получает `analysis_attempt_uuid`, а не только `call_uuid`:

1. Атомарно переводит attempt `pending -> processing`.
2. Читает связанный immutable transcription snapshot.
3. Загружает effective instructions и персонализацию как в текущем анализе.
4. Передает snapshot text анализатору.
5. Нормализует результат.
6. Обогащает evidence словами из того же snapshot.
7. В транзакции блокирует attempt, активную транскрипцию и `call_analyses`.
8. Если текущая ревизия отличается, помечает attempt `superseded`; активный анализ
   не меняет.
9. Если ревизия актуальна, записывает готовый результат в attempt и атомарно
   обновляет `call_analyses`, включая `transcription_revision` и
   `source_attempt_uuid`.
10. После commit обновляет call status/event.

При ошибке attempt становится `failed`, а предыдущий активный анализ остается
доступным. Call не должен переходить в общий `failed`, если у него уже есть готовый
активный анализ; frontend показывает ошибку именно повторной попытки.

Повторная доставка одного job должна быть идемпотентной. `done`, `failed` и
`superseded` attempts повторно не запускаются.

## 10. Связь исправлений с evidence

- matcher использует `words` выбранного revision snapshot;
- исправленный текст слова участвует в exact matching;
- индексы и времена остаются проверяемыми, потому что элемент слова не был добавлен
  или удален;
- исправленный speaker попадает в новое evidence;
- старый активный анализ продолжает ссылаться на свою `transcription_revision`;
- frontend явно показывает, если активный анализ построен не по текущей ревизии:
  «Транскрипция изменена. Анализ относится к версии N».

После сохранения исправлений нельзя молча скрывать старый анализ или представлять
его как актуальный.

## 11. Frontend

### 11.1. Типы и API

Обновить `src/types.ts`:

- `TranscriptionResponse.revision`, `edited`, `editable`, editor metadata;
- `TranscriptionEditRequest`, `TranscriptionWordEdit`;
- `TranscriptionRevisionSummary` и list response;
- `AnalysisAttemptResponse`;
- `AnalysisResponse.transcription_revision` и attempt state.

Обновить `src/api.ts`:

- `updateTranscription(callId, input)`;
- `listTranscriptionRevisions(callId, pagination)`;
- `analyzeCall(callId, { transcription_revision })`;
- `getLatestAnalysisAttempt(callId)` при необходимости polling.

Все UUID в path проходят `encodeURIComponent`.

### 11.2. Режим редактирования

Точка входа находится в карточке транскрипции `CallDetailPanel`. Кнопка показывается
только при `editable=true` и открывает отдельную страницу редактора, чтобы длинный
диалог и управляющие действия не были ограничены размером карточки звонка.

Поведение:

1. Страница создаёт локальный draft из текущих words и группирует их в читаемые
   реплики по фактическому speaker; табличное представление не используется.
2. Каждое слово редактируется на месте внутри реплики без изменения его таймкода.
3. Поле говорящего в заголовке реплики назначает speaker всем словам этого блока.
4. Изменённые слова и реплики визуально отмечаются, а закреплённая нижняя панель
   всегда показывает число правок и действия «Сбросить» и «Сохранить исправления».
5. Причина не запрашивается у пользователя: клиент отправляет фиксированное
   продуктовое значение «Исправление транскрипции».
6. Во время запроса действия блокируются от повторной отправки.
7. После успеха локальная транскрипция заменяется ответом backend и пользователь
   возвращается к звонку.
8. После `409` draft сохраняется, показывается конфликт и предлагается загрузить
    новую версию; автоматическое наложение изменений не выполняется.

Над диалогом размещается панель «Участники разговора». Для каждого фактического
speaker отдельно хранятся технический `speaker_key`, отображаемое имя, продуктовая
роль и необязательная ссылка на контакт пользователя. Имя или UUID контакта не
записываются в слова транскрипции и не меняют исторические snapshots. Один и тот же
`speaker_key` получает стабильный цвет во всех репликах; палитра должна сохранять
контраст в светлой и тёмной темах. Привязать можно только контакт из списка текущего
пользователя. Изменение участников не создаёт новую текстовую revision.

Не использовать `contenteditable` для всего транскрипта: он затрудняет стабильное
сопоставление DOM и word indexes. Использовать контролируемый word editor.

### 11.3. Повторный анализ

После успешного сохранения показать ненавязчивый stale-banner:

«Транскрипция обновлена до версии 3. Текущий анализ создан по версии 2».

Действия:

- «Повторить анализ»;
- «Оставить текущий анализ».

Перед запуском modal подтверждает:

- revision;
- что старый результат останется доступным до успеха;
- что операция может учитывать лимиты тарифа, если текущая billing-логика списывает
  анализ повторно. Конкретное правило списания должно быть подтверждено владельцем
  продукта до реализации и одинаково применяться backend/frontend.

Во время attempt старый анализ остается на экране с индикатором «Обновляется».
При успехе одновременно обновляются transcription, analysis и call state. При
ошибке показывается retry action без удаления старого результата.

### 11.4. История

Панель истории показывает revision, дату, автора, количество измененных слов и
явно отмечает активную версию. Нажатие на версию открывает отдельную страницу
сравнения. На странице одновременно выбираются от двух до пяти версий через
действие «Добавить версию»; каждой версии назначается устойчивый цвет в рамках
текущего сравнения. Для каждой пары соседних выбранных версий показываются дата,
диапазоны word indexes, таймкоды, старый и новый текст, а также смена speaker.
Пользователь может выбрать существующую версию как активную без создания копии
или новой revision. Для больших транскрипций diff загружается лениво и
виртуализируется.

Выбранные версии размещаются как окна: пользователь перетаскивает карточку за её
заголовок, без отдельных кнопок перемещения. Рабочая область изначально состоит из
двух магнитных колонок. Во время перетаскивания между колонками, а также слева и
справа от них, появляются компактные зоны вставки без отдельной панели или надписи.
Сброс карточки в такую зону создаёт колонку именно в выбранной позиции и позволяет
расширить рабочее пространство вплоть до одной колонки на каждую выбранную версию.
Пустые колонки автоматически схлопываются. Одна карточка
занимает всю высоту колонки, а две и более
карточки в одной колонке делят её по вертикали. Поэтому поддерживается, например,
раскладка «одна версия слева, две версии столбиком справа».

Карточку можно переносить между колонками и менять её вертикальную позицию внутри
колонки. Перетаскивание к левому или правому краю соседней колонки переставляет
колонку целиком перед или после неё; центральная зона складывает карточки по
вертикали. Во время drag-and-drop показываются целевая колонка и точное место вставки.
Порядок и колонка каждой версии сохраняются в URL без повторной загрузки snapshot и
определяют последовательность попарного сравнения. Добавление и перестановка
анимируются через View Transitions с FLIP fallback; уже открытые карточки не
перемонтируются, поэтому их прокрутка не сбрасывается. При
`prefers-reduced-motion: reduce` переходы отключаются.

Если в snapshot присутствуют speaker labels, каждая колонка сравнения группирует
слова по фактическим говорящим и сохраняет их метки отдельно для каждой версии.
Смена speaker считается самостоятельным изменением и отображается в хронологии.
Если диаризации нет, роли и говорящие не синтезируются: выводится обычный текст.
Изменённые слова выделяются непосредственно внутри всех карточек транскрипции:
первая выбранная версия сравнивается со следующей, остальные — с предыдущей.

### 11.5. Доступность и мобильный режим

- все word actions доступны с клавиатуры;
- выбранное слово имеет видимый focus;
- ошибка связана с конкретным полем;
- modal удерживает focus и возвращает его инициатору;
- цвет не является единственным признаком изменения;
- на мобильной ширине editor не создает горизонтальный scroll;
- пользователь может слушать запись и редактировать без autoplay.

## 12. События и обновление состояния

Текущий call event stream должен различать обычный первый анализ и reanalysis.
Добавить либо расширить payload событиями:

- `transcription.updated` с `call_uuid`, `revision`, `edited_at`;
- `analysis_attempt.pending|processing|done|failed|superseded` с UUID attempt и
  revision, без текста;
- `analysis.promoted` с новой активной revision.

Если event stream пока не поддерживает типизированные доменные события, frontend
может временно polling latest attempt. Это должно быть явно временным решением;
несколько конкурирующих polling loops в `App.tsx` создавать нельзя.

## 13. Валидация, ошибки и конкурентность

- два клиента читают revision 4;
- первый сохраняет и получает revision 5;
- второй получает `409 transcription_revision_conflict`;
- второй клиент не перезаписывает revision 5;
- frontend не пытается автоматически угадывать merge.

Редактирование запрещено при активном analysis attempt. Анализ также нельзя начать
между проверкой revision и созданием attempt: проверка и создание выполняются в
транзакции с блокировкой транскрипции.

Сохранение новой revision после уже начатого анализа не допускается в первой
production-версии. Защита
`superseded` в worker остается обязательной как defense in depth и для старых job.

## 14. Безопасность и приватность

- исправления и snapshots имеют тот же ACL, что полный транскрипт;
- list history не раскрывает текст изменений;
- request/response body не логируются;
- validation errors не включают исправленный текст;
- telemetry содержит только call UUID, revision, counts, duration и status;
- удаление звонка каскадно удаляет revisions, attempts и active analysis;
- экспорт старого анализа должен использовать версию, указанную в анализе, либо
  явно маркировать несовпадение; он не должен соединять старый анализ с новым
  транскриптом как будто они согласованы;
- rate limit PATCH и reanalysis применяется по пользователю и звонку;
- reason считается пользовательскими данными и не отправляется провайдеру анализа.

### 14.1. Угрозы content-addressed хранения и меры

| Угроза | Обязательная мера |
|---|---|
| Hash collision | SHA-256 используется только для поиска кандидатов; reuse только после точного сравнения canonical bytes и размера |
| Cross-tenant hash probing | Дедупликация и поиск кандидатов ограничены одним `transcription_uuid`; API не принимает и не возвращает hash |
| Подмена content UUID | Составные FK подтверждают принадлежность content, revision и transcription одному звонку; repository повторяет проверку |
| Цикл или слишком длинная patch chain | Ссылка только на существующий base того же звонка, `chain_depth <= 20`, cycle detection и reconstruction budget |
| Patch bomb / resource exhaustion | Типизированные операции, лимиты payload/operations/result size, context deadline и rate limit до дорогой реконструкции |
| Повреждение checkpoint/patch | После реконструкции обязательна проверка canonical size и hash; fail closed, alert, восстановление из backup |
| Race при dedup | Advisory/row lock на transcription, повторная проверка кандидатов внутри транзакции, idempotency key операции |
| Несанкционированный garbage collection | Отдельная service role, reference check в транзакции, audit, batch limits и проверенный restore |
| Утечка через логи/метрики | Hash, canonical bytes, patch, reason и текст не логируются; метрики содержат только counts/status/duration |
| Алгоритмический DoS через JSON | Клиент не передает snapshot/patch; строгие request limits, streaming decode с запретом unknown fields и post-decode size checks |

Побайтовое сравнение не обязано быть constant-time: кандидаты всегда относятся к
тому же звонку и уже прошли ACL, а API не предоставляет hash-oracle. Приоритет —
потоковая обработка с фиксированным верхним пределом RAM и полное сравнение до reuse.

SHA-256 здесь помогает дедупликации и проверке целостности, но не используется как
ACL, цифровая подпись или подтверждение авторства. Для защиты от несанкционированного
изменения действуют database permissions, транзакции, audit и backup verification.

## 15. Наблюдаемость

Метрики без пользовательского текста:

- `transcription_edits_total` по scope/status;
- количество измененных words на revision;
- revision conflicts;
- доля edited transcriptions;
- analysis attempts по status;
- reanalysis duration;
- superseded attempts;
- stale analysis duration до повторного анализа;
- evidence matched/ambiguous/not_found после reanalysis.

Структурированные логи содержат UUID, revision, actor UUID, counts и status, но не
слова, speaker labels, reason или transcript text.

### 15.1. Эксплуатационная готовность

До общего включения функции команда обязана определить и зафиксировать численные
SLO; эта спецификация не выдумывает их без продуктовой нагрузки и инфраструктурных
измерений. Минимально контролируются:

- доступность PATCH/history/restore/reanalysis API;
- p50/p95/p99 latency чтения, сохранения revision и promotion анализа;
- доля конфликтов, ошибок сохранения и permanently failed attempts;
- глубина и возраст очереди reanalysis;
- время от запуска до promotion;
- рост PostgreSQL и стоимость хранения snapshots;
- доля успешного восстановления из backup.

Обязательны dashboards, alert thresholds, runbook для stuck attempt, повторного job,
ошибки migration/backfill, рассинхронизации active revision и active analysis, а
также инструкция безопасного отключения записи при сохранении чтения.

### 15.2. Хранение, backup и восстановление

- revisions и attempts включаются в штатные backup/restore процедуры PostgreSQL;
- до релиза проводится тестовое восстановление в изолированную базу;
- RPO/RTO принимаются владельцем продукта вместе с общей политикой VerbaTrace;
- content rows не удаляются отдельным фоновым процессом раньше звонка без явно
  утвержденной retention policy;
- удаление по запросу пользователя охватывает active transcription, revisions,
  attempts, analyses, exports, search indexes и резервные копии согласно принятой
  политике удаления;
- migration/backfill поддерживает возобновление после прерывания и наблюдаемый
  прогресс; длительная блокировка production-таблиц недопустима;
- объем checkpoints, patches и коэффициент дедупликации измеряются на реальных
  длинных звонках; оптимизацию нельзя внедрять ценой невоспроизводимой истории;
- удаление неиспользуемого content выполняется только reference-aware collector:
  content удаляется, если на него не ссылается ни revision, ни analysis attempt, ни
  другой patch как на base; операция повторно проверяет ссылки в одной транзакции;
- collector не доступен через пользовательский API и работает ограниченными batch с
  dry-run метриками.

### 15.3. Производительность и масштабирование

- PATCH выполняет работу пропорционально размеру транскрипции с установленным
  верхним лимитом payload и измеренным пределом длительности;
- чтение списка revisions не декодирует snapshot JSON;
- snapshot и diff endpoints поддерживают pagination/streaming там, где фактический
  размер этого требует;
- frontend не перерисовывает всю длинную транскрипцию при каждом вводе; editor и
  history виртуализируются после измеренного порога;
- reanalysis использует существующую durable queue, retry/backoff и idempotency;
- нагрузочные тесты покрывают одновременное чтение, редактирование и фоновые анализы
  на согласованном production capacity target.

### 15.4. Поддержка и продуктовая аналитика

- у support/admin должен быть безопасный diagnostic view статусов и UUID без
  раскрытия текста по умолчанию;
- пользователь видит понятный request/correlation ID при серверной ошибке;
- продуктовые события измеряют вход в editor, сохранение, конфликт, отказ от
  повторного анализа, success/failure и restore без содержимого разговора;
- схема событий версионируется и документируется;
- feature flag имеет owner, план rollout и дату удаления после стабилизации, а не
  остается постоянной альтернативной веткой поведения.

## 16. Порядок реализации

### Этап 1. Версионирование backend

1. Добавить migrations и backfill/lazy-snapshot strategy.
2. Расширить domain/repository models и scanners.
3. Реализовать revision repository и transactional edit.
4. Реализовать deterministic renderer текста/segments.
5. Добавить ACL и PATCH/history handlers.
6. Обновить mocks и README.

### Этап 2. Safe reanalysis backend

1. Добавить attempts repository и DTO.
2. Изменить job entity с call UUID на attempt UUID для новых reanalysis jobs.
3. Сохранить совместимость обработки существующих initial-analysis jobs либо
   выполнить атомарную migration очереди до rollout worker.
4. Реализовать snapshot-based analysis и conditional promotion.
5. Разделить failure attempt и общий call failure.
6. Добавить events/monitoring.

### Этап 3. Frontend editor

1. Расширить типы и API.
2. Добавить controlled word editor и speaker range action.
3. Добавить reason confirmation и conflict UX.
4. Добавить stale analysis banner и history panel.

### Этап 4. Frontend reanalysis и QA

1. Добавить confirm flow и attempt progress.
2. Сохранить видимость старого анализа до promotion.
3. Обновить единое состояние `App.tsx` после success event.
4. Проверить desktop/mobile и реальные API payloads.
5. Выполнить `graphify update .`.

## 17. Тестирование

### 17.1. Backend unit tests

- validation каждого edit-поля;
- Unicode rune limits;
- duplicate/out-of-range indexes;
- immutable times/confidence;
- punctuation renderer;
- regrouping speakers into segments;
- changed indexes sorted and unique;
- no-op detection;
- stale revision;
- ACL matrix;
- evidence по исправленным словам;
- отсутствие transcript/reason в logs.
- golden vectors канонизации и неизменность результатов одной версии алгоритма;
- одинаковое содержимое дает одинаковые canonical bytes/hash;
- разные schema/canonicalization versions не дедуплицируются;
- hash+size match без byte equality не переиспользует content;
- patch reconstruction, checkpoint threshold, depth limit и cycle rejection;
- malformed/oversized patch fail closed без чрезмерного CPU/RAM.

### 17.2. Repository/integration tests

- forward/down migration;
- backfill или lazy creation revision 1;
- edit transaction и rollback;
- повторное состояние и restore переиспользуют content UUID без копирования payload;
- дедупликация не пересекает разные `transcription_uuid` и tenants;
- конкурентное создание одинакового content не нарушает целостность;
- составные FK запрещают ссылку revision/patch на content другого звонка;
- reference-aware collector не удаляет достижимый content;
- corruption hash/size обнаруживается до выдачи или анализа;
- одновременные PATCH дают один success и один conflict;
- cascade delete;
- один active attempt на звонок;
- attempt читает точный snapshot;
- failed attempt сохраняет active analysis;
- successful current attempt атомарно продвигается;
- stale attempt становится superseded;
- повторная доставка job идемпотентна;
- API не расширяет видимость звонка;
- старые payloads получают корректные defaults.

Database-sharing integration suites запускать последовательно.

### 17.3. Frontend tests

- editor открывается только при `editable`;
- draft не мутирует исходный response;
- изменение word/speaker формирует минимальный request;
- cancel очищает draft;
- reason обязателен;
- conflict сохраняет draft;
- stale banner сравнивает revisions;
- старый analysis остается во время attempt;
- success атомарно обновляет UI;
- failure оставляет старый result и показывает retry;
- keyboard и mobile behavior;
- legacy `words: []` остается читаемым и не редактируется.

### 17.4. Browser QA

На реальном сохраненном звонке:

1. Открыть транскрипцию и сверить слово с аудио.
2. Исправить слово и speaker, указать причину, сохранить.
3. Проверить `GET transcription`: revision увеличился, времена не изменились.
4. Перезагрузить страницу и подтвердить сохранение исправлений.
5. Убедиться, что старый анализ маркирован как относящийся к предыдущей версии.
6. Запустить повторный анализ и во время обработки открыть старый результат.
7. После успеха проверить новый analysis revision и evidence seek.
8. Искусственно вызвать provider failure и подтвердить сохранность старого анализа.
9. Создать конфликт двух вкладок и проверить `409` без потери draft.
10. Проверить пользователя только с read-доступом.
11. Проверить мобильную ширину, клавиатуру и длинную транскрипцию.

Компиляция или unit tests без этого сценария не подтверждают готовность.

## 18. Совместимость и rollout

1. Выпустить additive migrations.
2. Выпустить backend, который читает старые записи как `revision=1`.
3. Создать snapshots revision 1 и проверить counts.
4. Выпустить attempts/safe promotion до появления frontend-кнопки reanalysis.
5. Выпустить frontend editor под feature flag.
6. Проверить реальные edits/evidence на ограниченной группе.
7. Включить reanalysis flow.
8. После стабилизации удалить старый destructive reanalysis path либо оставить его
   только для initial analysis без активного результата.

Rollback frontend безопасен. Rollback backend после появления revisions/attempts
должен продолжать игнорировать additive columns, но старый `POST analysis` нельзя
возвращать в эксплуатацию для повторного анализа: он очистит активный результат.

## 19. Definition of Done

Работа завершена только если:

1. Пользователь с правом изменения может исправить текст слова и speaker.
2. Времена, confidence, количество и порядок words не меняются.
3. Backend сам пересобирает text и segments.
4. Каждое сохранение создает immutable revision с actor/reason/time.
5. Конкурирующий stale PATCH возвращает 409 и ничего не перезаписывает.
6. Старые транскрипции без words не получают вымышленных таймкодов.
7. Анализ запускается по immutable snapshot конкретной revision.
8. Старый готовый анализ доступен до успешного promotion нового.
9. Failed/superseded attempt не меняет active analysis.
10. Evidence нового анализа связано со словами той же revision.
11. Frontend явно показывает несовпадение revisions.
12. ACL одинаково применяется к edit, history и reanalysis.
13. Удаление звонка удаляет все связанные revisions и attempts.
14. В логах и telemetry отсутствуют transcript text, исправленные слова и reason.
15. Backend/frontend tests и browser QA пройдены.
16. README и API-примеры обновлены.
17. Выполнен `graphify update .` после реализации кода.
18. История, diff и restore работают без перезаписи существующих revisions.
19. Backup restore проверен на данных с revisions и analysis attempts.
20. Приняты SLO, capacity target, RPO/RTO; dashboards, alerts и runbooks готовы.
21. Backfill проверен на production-like объеме и может безопасно продолжиться после
    прерывания.
22. Feature flag rollout и rollback проверены; назначены owner и условие удаления
    флага.
23. Retention/delete охватывает все новые таблицы, exports, indexes и backup policy.
24. Структурное редактирование либо выпущено с token provenance, либо его этап и
    ограничения первой production-версии явно отражены в пользовательской
    документации без ложного обещания свободного редактирования.
25. Revision не содержит копию полного payload и ссылается на content того же звонка.
26. Полностью одинаковое состояние переиспользует content только после byte equality,
    а не только по hash.
27. Restore создает новую revision со ссылкой на существующий content без копирования.
28. Patch chain ограничена, проверяется по hash/size и регулярно завершается
    checkpoint.
29. Cross-call/cross-tenant дедупликация и hash probing невозможны через API.
30. Collector не удаляет content, достижимый из revision, attempt или patch base;
    сценарий восстановления после ошибочного запуска проверен.

## 20. Обязательные решения до начала реализации

До реализации владелец продукта должен письменно подтвердить:

1. Списывается ли тарифный лимит за повторный анализ после ручного исправления.
2. Кто кроме загрузившего пользователя вправе исправлять company/department calls.
3. Можно ли удалять speaker label пустым значением или только заменять его.
4. Нужна ли обязательная причина для исходного пользователя либо только для
   manager/admin.
5. Какой максимальный размер одного пакета edits нужен для длинных звонков.
6. Retention policy для revisions, attempts, exports и резервных копий.
7. Production SLO, capacity target, RPO/RTO и ответственных за alerts/runbooks.
8. Требования к юридическому основанию хранения исправлений и отображению имени
   редактора другим участникам компании.
9. Нужны ли раздельные права `transcription:edit`, `transcription:history` и
   `transcription:restore` вместо вывода только из текущих ролей.
10. Модель поддержки структурных edits: срок включения token UUID этапа и допустимое
    поведение evidence для `range_derived`/`unavailable` tokens.

Без этих решений функция не считается готовой к production rollout. Они не отменяют
архитектурные требования: immutable revisions, optimistic locking, snapshot-based
analysis и сохранение старого результата до успеха нового.
