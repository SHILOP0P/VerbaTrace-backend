# Спецификация: семантическая маскировка персональных данных и безопасные версии медиа

Статус: реализована в рабочем дереве; техническая приёмка обычного аудиозвонка пройдена
Дата: 2026-09-02
Backend: `C:\projects\VerbaTrace\Monolit`
Frontend: `C:\projects\VerbaTrace-frontend`
Тип изменения: versioned privacy policy, provider-neutral redaction, redaction-aware analysis, ACL и производные media-файлы

## 0. Фактический статус реализации

Реализованы migration, backend-контракты, AssemblyAI adapter, русская
нормализация маркеров и spans, redaction-aware analysis, защита обычного
редактора, отдельный workflow исправления масок, ACL оригинала, локальные
очищенные media-варианты, audit/retention/provider cleanup, API и frontend.

Подтверждено 2 сентября 2026 года:

- полный `scripts/verify-ci.ps1` завершён с кодом 0: format, lint, unit,
  PostgreSQL integration, `go vet` и Docker build;
- frontend `npm.cmd run build` завершён с кодом 0;
- migration проверена на чистой PostgreSQL-базе и отдельным циклом down/up;
- платный live-тест обычного синтетического русского аудиозвонка в AssemblyAI
  вернул русские `[ИМЯ]`, `[ТЕЛЕФОН]`, `[ЭЛЕКТРОННАЯ_ПОЧТА]`, сохранил денежную
  сумму и удалил provider artifact; повторная проверка списка показала 0
  оставшихся тестовых artifacts за проверяемый интервал;
- runtime FFmpeg matrix прошла для MP3, WAV, M4A, OGG, MP4, MOV, WebM и MKV;
  у проверочного MP4 сохранились audio/video streams и исходный H.264 video
  codec, отклонение длительности проверочного audio составило 32 ms;
- browser QA страницы «Защита данных» выполнен в светлой и тёмной теме;
  обнаруженное сжатие checkbox и русских маркеров на узком контейнере исправлено,
  после исправления console errors отсутствуют.

Этот статус подтверждает техническую реализацию, но не является заявлением о
юридическом соответствии или абсолютном обнаружении всех персональных данных.
Перед массовым production rollout всё ещё нужен benchmark на репрезентативной
выборке реальных звонков и обычный операционный rollout из раздела 21.

## 1. Цель

VerbaTrace должен скрывать персональные данные в рабочей транскрипции, не превращая
звонок в непригодный для проверки материал и не заставляя модель считать скрытое
значение отсутствующим.

После реализации:

- обычные аудиозвонки и аудиодорожки видеозвонков обрабатываются одинаково;
- исходное аудио или видео не изменяется и остаётся основной записью для тех, у
  кого есть право на оригинал;
- рабочая транскрипция содержит только русские смысловые маркеры: `[ИМЯ]`,
  `[ТЕЛЕФОН]`, `[ЭЛЕКТРОННАЯ_ПОЧТА]`, `[АДРЕС]` и другие;
- модель понимает, что маркер подтверждает наличие значения в разговоре, а не
  обозначает ошибку распознавания;
- денежные суммы по умолчанию не скрываются, чтобы не ломать проверку цены,
  скидки, лимита и соответствия инструкции;
- очищенная аудио- или видеокопия создаётся только для экспорта либо для
  пользователя, которому разрешён звонок, но запрещён оригинал;
- все решения фиксируются снимком политики, ACL, аудитом и стабильными API-кодами;
- английские provider-labels никогда не показываются пользователю.

Главный продуктовый принцип:

> Оригинальная запись остаётся доказательством, русская семантически маскированная
> транскрипция становится безопасным рабочим контекстом для анализа, интерфейса и
> обычного экспорта.

## 2. Подтверждённое исходное состояние

На момент написания спецификации:

- `calls.audio_path` указывает на исходный аудио- или видеофайл;
- `calls.asr_cache_path` указывает на mono 16 kHz Opus OGG, создаваемый через
  `ffmpeg` только для ASR;
- `GET /api/v1/calls/{uuid}/media` и legacy alias `/audio` отдают исходный файл с
  поддержкой Range requests;
- frontend выбирает audio/video player через `media_kind` и `mime_type`;
- `call_transcriptions` хранит `text`, `segments`, `words`, язык и provider;
- `call_transcription_contents` и `call_transcription_revisions` уже обеспечивают
  историю редактирования и восстановление;
- word timestamps используются для seek/highlight и deterministic evidence;
- AssemblyAI-запрос содержит `audio_url`, `speech_models`, language detection,
  diarization и optional speaker identification, но не содержит PII-параметров;
- текущий анализатор получает одну строку транскрипции без structured redaction
  context;
- текущий редактор позволяет менять текст любого word-element и поэтому без
  дополнительных ограничений сможет случайно или намеренно снять маску;
- отчёты используют текущий `Transcription.Text`, отдельного privacy variant в
  `call_report_exports` нет;
- `ffmpeg` и `ffprobe` уже входят в runtime и readiness checks;
- retention worker удаляет основной файл и ASR cache, но не знает о производных
  privacy-media;
- текущий ACL звонка не разделяет право читать звонок и право получать оригинал.

Следствие: включение одного `redact_pii=true` недостаточно. Нужны versioned policy,
нормализация меток, структурированные spans, защита редактора, redaction-aware
analysis, отдельный media ACL и удаление производных файлов.

## 3. Зафиксированные продуктовые решения

### 3.1. Что входит в первую production-поставку

- личная и company-scoped политика маскирования;
- immutable опубликованные версии политики и снимок на каждом звонке;
- AssemblyAI PII detection с `entity_name` substitution;
- backend-нормализация provider-labels в русские маркеры;
- структурированные redaction spans с word/time ranges;
- маскированные `text`, `segments` и `words` как основной transcription contract;
- передача только маскированной транскрипции в анализатор;
- redaction-aware правила системного prompt и статус `not_evaluable`;
- ручное добавление или исправление маски через отдельный privacy workflow;
- запрет обхода маски через обычный transcription editor;
- исходное media как основной player для пользователя с правом на оригинал;
- on-demand sanitized media для экспорта и restricted-original ACL;
- локальное построение sanitized media из исходника и сохранённых таймкодов;
- безопасный экспорт, аудит, метрики, retries, retention и provider cleanup;
- backend, frontend, integration и browser tests.

### 3.2. Что намеренно не входит

- размытие лиц, документов, экранов и других объектов в видеоряде;
- изменение исходного аудио или видео;
- попытка восстановить скрытое значение через LLM;
- автоматическое юридическое решение о наличии согласия или законного основания;
- маскирование денежных сумм по умолчанию;
- department override политики в первой версии;
- использование английских маркеров в пользовательском интерфейсе;
- автоматический backfill всех старых звонков без preview и подтверждения стоимости;
- заявление о полном обезличивании или соответствии закону только по факту
  включения этой функции.

### 3.3. Почему не используется provider redacted audio как основной механизм

AssemblyAI умеет создавать redacted audio, однако для VerbaTrace это не основной
путь:

- текущий provider получает преимущественно ASR cache, а не исходное media;
- redacted audio URL ограничен по времени и требует немедленного скачивания;
- для видео provider возвращает аудиофайл, а не готовое видео;
- привязка media lifecycle к одному provider ухудшает заменяемость ASR;
- сохранённые word timestamps позволяют построить производную копию локально из
  исходного media и не ухудшать качество до уровня ASR cache.

Provider redacted audio можно отдельно исследовать как fallback в последующей
версии, но v1 его не запрашивает и не включает в контракт.

## 4. Термины

### 4.1. Оригинал

Исходный media-файл, сохранённый при ручной загрузке или ingest. Он не изменяется
после создания звонка.

### 4.2. Рабочая транскрипция

Текущие `text`, `segments` и `words`, доступные обычному интерфейсу, анализатору,
поиску и redacted export. При включённой политике они содержат русские маркеры.

### 4.3. Маркер

Русский однозначный token без пробелов, например `[ИМЯ]`. Маркер не является
пустым значением и не должен интерпретироваться как ошибка ASR.

### 4.4. Redaction span

Структурированная запись о скрытом диапазоне: тип сущности, русский маркер,
word range, временной интервал, происхождение и revision.

### 4.5. Очищенная media-копия

Производный аудио- или видеофайл, в котором звук внутри redaction spans заменён
коротким нейтральным сигналом. В видеозвонке видеоряд не изменяется.

### 4.6. Политика и снимок политики

Политика задаёт поведение для будущих звонков scope. Опубликованная версия
immutable. При создании звонка backend сохраняет полный snapshot; последующее
изменение политики не меняет in-flight или готовый звонок.

## 5. Обязательные инварианты

1. Анализатор никогда не получает исходную немаскированную транскрипцию звонка с
   активной privacy policy.
2. `text`, `segments`, `words` и `redaction_spans` относятся к одной revision и не
   могут публиковаться частично.
3. Пользовательские маркеры всегда русские. Provider labels считаются внутренней
   деталью adapter-а.
4. Денежные суммы не входят в default category set.
5. Наличие `[ИМЯ]` подтверждает, что имя было произнесено; оно не подтверждает
   конкретное имя.
6. Критерий, требующий скрытого точного значения, не получает штрафной балл только
   из-за redaction.
7. Обычный transcription edit не может изменить redacted word.
8. Оригинальное media никогда не перезаписывается sanitized media.
9. Пользователь без `can_read_original_media` не получает original URL, redirect,
   filename, signed path или fallback на оригинал.
10. Если policy enforcement равен `required`, ошибка redaction останавливает
    analysis. Silent downgrade запрещён.
11. Повтор worker-а не создаёт вторую транскрипцию, второй span set или второй
    media artifact для того же idempotency tuple.
12. Logs, traces, metrics и audit metadata не содержат исходное скрытое значение.
13. Удаление звонка охватывает original, ASR cache, privacy artifacts, spans,
    revisions, exports и provider-side transcript cleanup state.
14. Legacy звонок без privacy snapshot не помечается как защищённый.
15. Global admin role сама по себе не предоставляет доступ к business media.

## 6. Категории и русские маркеры

Внутренний enum стабилен и provider-neutral. Русский marker является частью API
contract версии `ru-v1`.

| Internal entity type | AssemblyAI policy | Маркер `ru-v1` | По умолчанию | Комментарий |
|---|---|---|---:|---|
| `person_name` | `person_name` | `[ИМЯ]` | Да | Имя или ФИО человека |
| `phone_number` | `phone_number` | `[ТЕЛЕФОН]` | Да | Телефон или факс |
| `email_address` | `email_address` | `[ЭЛЕКТРОННАЯ_ПОЧТА]` | Да | Email |
| `address` | `location_address`, `location_address_street`, `location_zip` | `[АДРЕС]` | Да | Полный адрес, отдельная улица или индекс; город/страна сами по себе не скрываются |
| `date_of_birth` | `date_of_birth` | `[ДАТА_РОЖДЕНИЯ]` | Да | Не любая дата |
| `passport_number` | `passport_number` | `[НОМЕР_ПАСПОРТА]` | Да | Номер паспорта |
| `drivers_license` | `drivers_license` | `[НОМЕР_ВОДИТЕЛЬСКОГО_УДОСТОВЕРЕНИЯ]` | Да | Номер удостоверения |
| `account_number` | `account_number` | `[НОМЕР_СЧЁТА]` | Да | Клиентский или банковский счёт |
| `banking_information` | `banking_information` | `[БАНКОВСКИЕ_ДАННЫЕ]` | Да | Банковские реквизиты |
| `credit_card_number` | `credit_card_number` | `[НОМЕР_КАРТЫ]` | Да | Номер карты |
| `credit_card_cvv` | `credit_card_cvv` | `[КОД_КАРТЫ]` | Да | CVV/CVC |
| `credit_card_expiration` | `credit_card_expiration` | `[СРОК_ДЕЙСТВИЯ_КАРТЫ]` | Да | Срок карты |
| `password` | `password` | `[СЕКРЕТ]` | Да | Пароль, PIN, код доступа |
| `ip_address` | `ip_address` | `[СЕТЕВОЙ_АДРЕС]` | Нет | Включается для технических звонков |
| `username` | `username` | `[ИМЯ_ПОЛЬЗОВАТЕЛЯ]` | Нет | Login/handle |
| `medical_condition` | `medical_condition` | `[МЕДИЦИНСКИЕ_ДАННЫЕ]` | Нет | Только для соответствующего ICP |
| `money_amount` | `money_amount` | `[ДЕНЕЖНАЯ_СУММА]` | **Нет** | Ломает проверку цены и лимитов |
| `organization` | `organization` | `[ОРГАНИЗАЦИЯ]` | **Нет** | Обычно нужно business-анализу |

`number_sequence`, broad `date`, broad `location`, `city`, `country`, `duration`,
`occupation` и другие слишком широкие категории запрещены в default set. Их можно
добавить новой marker-contract version после benchmark на русских звонках.

Frontend показывает только русские названия. Internal enum и provider policy не
рендерятся пользователю даже в error message.

## 7. Политика и наследование

### 7.1. Scope

- personal call получает активную personal policy загрузившего пользователя;
- company и department call получают активную company policy;
- integration ingest получает policy конечной placement company, а не владельца
  API key;
- sandbox mock использует deterministic fixture и статус `simulated`; он не может
  подтверждать работу provider detection;
- если опубликованной policy нет, применяется `disabled` для обратной
  совместимости. Автоматическое включение существующим пользователям запрещено.

Department override не входит в v1. Модель данных допускает новый `scope_type`, но
backend возвращает 422 для неподдерживаемого scope.

### 7.2. Поля policy config v1

```json
{
  "schema_version": 1,
  "enabled": true,
  "enforcement": "required",
  "marker_contract": "ru-v1",
  "entity_types": [
    "person_name",
    "phone_number",
    "email_address",
    "address",
    "date_of_birth",
    "passport_number",
    "drivers_license",
    "account_number",
    "banking_information",
    "credit_card_number",
    "credit_card_cvv",
    "credit_card_expiration",
    "password"
  ],
  "original_media_access": "call_acl",
  "sanitized_media": "on_demand",
  "exports": "redacted_by_default",
  "analysis_input": "redacted"
}
```

Допустимые значения:

- `enforcement`: в v1 только `required`;
- `original_media_access`:
  - `call_acl` — сохраняет текущий доступ;
  - `uploader_and_scope_managers` — uploader, company manager и leader отдела
    звонка;
  - `uploader_only` — только uploader;
- `sanitized_media`: `off` или `on_demand`;
- `exports`: `redacted_by_default` или `redacted_only`;
- `analysis_input`: в v1 только `redacted`.

Запрещённые комбинации:

- restricted `original_media_access` вместе с `sanitized_media=off`, если scope
  содержит иных call readers;
- empty `entity_types` при `enabled=true`;
- `money_amount` без явного `high_impact_acknowledged=true` в publish request;
- `analysis_input=original`;
- unknown marker contract или entity type.

### 7.3. Preview и publish

Редактирование выполняется как draft. Publish требует:

- `If-Match: "<lock_version>"`;
- `preview_hash`, рассчитанный backend по canonical config;
- подтверждение high-impact categories;
- причину длиной 10–500 символов для company policy;
- active company manager для company scope или самого пользователя для personal.

Опубликованная version immutable. Повтор с тем же idempotency key и тем же body
возвращает ту же version. Тот же key с другим body возвращает 409.

## 8. Модель данных

Целевая migration: `Monolit/migrations/202609010001_create_transcription_privacy.sql`.
SQL ниже задаёт обязательную форму; названия индексов могут быть сокращены только
при сохранении семантики.

### 8.1. `transcription_privacy_policies`

```sql
CREATE TABLE transcription_privacy_policies (
    privacy_policy_uuid UUID PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('personal','company')),
    owner_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    company_uuid UUID NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    active_version_uuid UUID NULL,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type='personal' AND owner_user_uuid IS NOT NULL AND company_uuid IS NULL)
        OR
        (scope_type='company' AND company_uuid IS NOT NULL AND owner_user_uuid IS NULL)
    )
);

CREATE UNIQUE INDEX uq_privacy_policy_personal
    ON transcription_privacy_policies(owner_user_uuid)
    WHERE scope_type='personal';
CREATE UNIQUE INDEX uq_privacy_policy_company
    ON transcription_privacy_policies(company_uuid)
    WHERE scope_type='company';
```

Policy row создаётся лениво при первом сохранении draft. Один scope не может иметь
две policy rows даже при конкурентных запросах: repository использует
`INSERT ... ON CONFLICT ... RETURNING` и затем блокирует найденную строку.

### 8.2. `transcription_privacy_policy_drafts`

```sql
CREATE TABLE transcription_privacy_policy_drafts (
    privacy_policy_uuid UUID PRIMARY KEY
        REFERENCES transcription_privacy_policies(privacy_policy_uuid) ON DELETE CASCADE,
    config_schema_version SMALLINT NOT NULL CHECK (config_schema_version = 1),
    config JSONB NOT NULL CHECK (jsonb_typeof(config)='object'),
    config_sha256 BYTEA NOT NULL CHECK (octet_length(config_sha256)=32),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    updated_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Draft хранит только последнюю непубличную конфигурацию scope. `PUT draft` требует
`If-Match` draft `lock_version`, атомарно увеличивает его и возвращает новый ETag.
Если draft отсутствует, создание использует `If-None-Match: *`. Публикация удаляет
draft в той же транзакции, в которой создаётся immutable version и меняется
`active_version_uuid`.

### 8.3. `transcription_privacy_policy_versions`

```sql
CREATE TABLE transcription_privacy_policy_versions (
    privacy_policy_version_uuid UUID PRIMARY KEY,
    privacy_policy_uuid UUID NOT NULL
        REFERENCES transcription_privacy_policies(privacy_policy_uuid) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    config_schema_version SMALLINT NOT NULL CHECK (config_schema_version = 1),
    config JSONB NOT NULL CHECK (jsonb_typeof(config)='object'),
    config_sha256 BYTEA NOT NULL CHECK (octet_length(config_sha256)=32),
    publish_reason TEXT NOT NULL CHECK (char_length(btrim(publish_reason)) BETWEEN 3 AND 500),
    published_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (privacy_policy_uuid, version)
);

ALTER TABLE transcription_privacy_policies
    ADD CONSTRAINT fk_privacy_policy_active_version
    FOREIGN KEY (active_version_uuid)
    REFERENCES transcription_privacy_policy_versions(privacy_policy_version_uuid)
    ON DELETE RESTRICT;
```

Canonical JSON использует отсортированные ключи и отсортированный unique список
entity types. Hash не заменяет точное сравнение body при idempotency replay.

### 8.4. `call_privacy_states`

```sql
CREATE TABLE call_privacy_states (
    call_uuid UUID PRIMARY KEY REFERENCES calls(call_uuid) ON DELETE CASCADE,
    privacy_policy_version_uuid UUID NULL
        REFERENCES transcription_privacy_policy_versions(privacy_policy_version_uuid)
        ON DELETE RESTRICT,
    policy_source TEXT NOT NULL
        CHECK (policy_source IN ('none','personal','company','simulated')),
    policy_snapshot JSONB NOT NULL CHECK (jsonb_typeof(policy_snapshot)='object'),
    marker_contract TEXT NOT NULL DEFAULT 'ru-v1',
    status TEXT NOT NULL CHECK (
        status IN ('not_requested','queued','processing','ready','failed','legacy_unprotected')
    ),
    transcription_revision INTEGER NULL CHECK (transcription_revision > 0),
    detected_spans INTEGER NOT NULL DEFAULT 0 CHECK (detected_spans >= 0),
    last_error_code TEXT NULL,
    last_error_message_safe TEXT NULL,
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    CHECK (
        (status='ready' AND transcription_revision IS NOT NULL
            AND completed_at IS NOT NULL AND last_error_code IS NULL)
        OR status <> 'ready'
    )
);

CREATE INDEX idx_call_privacy_worker
    ON call_privacy_states(status, updated_at, call_uuid)
    WHERE status IN ('queued','processing','failed');
```

Call state содержит итог текущего privacy flow, а не provider-specific попытку.
Это не даёт повторной обработке затереть identifier предыдущего provider job.

### 8.5. `transcription_provider_attempts`

```sql
CREATE TABLE transcription_provider_attempts (
    provider_attempt_uuid UUID PRIMARY KEY,
    call_uuid UUID NULL REFERENCES calls(call_uuid) ON DELETE SET NULL,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    provider TEXT NOT NULL,
    request_sha256 BYTEA NOT NULL CHECK (octet_length(request_sha256)=32),
    provider_job_id TEXT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'submitting','polling','succeeded','failed','delete_pending','deleting',
        'deleted','delete_failed'
    )),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ NULL,
    locked_by TEXT NULL,
    last_error_code TEXT NULL,
    last_error_message_safe TEXT NULL,
    submitted_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (call_uuid, attempt_no)
);

CREATE UNIQUE INDEX uq_provider_attempt_job
    ON transcription_provider_attempts(provider, provider_job_id)
    WHERE provider_job_id IS NOT NULL;
CREATE INDEX idx_provider_attempt_cleanup
    ON transcription_provider_attempts(status, available_at, provider_attempt_uuid)
    WHERE status IN ('delete_pending','delete_failed');
```

`provider_job_id` не возвращается product API. Каждая фактически созданная задача
провайдера имеет отдельную строку и собственный cleanup lifecycle. Raw provider
request/response не сохраняются; `request_sha256` считается от canonical
безопасного request без URL с credentials. `call_uuid` становится `NULL` при
удалении звонка, чтобы durable cleanup task не исчезла раньше provider artifact;
строка attempt удаляется retention worker-ом только после `deleted` и истечения
операционного audit window.

### 8.6. `call_transcription_redaction_spans`

```sql
CREATE TABLE call_transcription_redaction_spans (
    redaction_span_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL
        REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision > 0),
    entity_type TEXT NOT NULL,
    marker TEXT NOT NULL CHECK (marker ~ '^\[[А-ЯЁ0-9_]+\]$'),
    word_start_index INTEGER NOT NULL CHECK (word_start_index >= 0),
    word_end_index INTEGER NOT NULL CHECK (word_end_index >= word_start_index),
    start_seconds NUMERIC(12,3) NOT NULL CHECK (start_seconds >= 0),
    end_seconds NUMERIC(12,3) NOT NULL CHECK (end_seconds >= start_seconds),
    source TEXT NOT NULL CHECK (source IN ('provider','manual')),
    provider_policy TEXT NULL,
    created_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transcription_uuid, revision, word_start_index, word_end_index)
);

CREATE INDEX idx_redaction_spans_revision
    ON call_transcription_redaction_spans(transcription_uuid, revision, word_start_index);
```

Raw value, raw provider fragment и reversible hash хранить запрещено.

### 8.7. `call_media_variants`

```sql
CREATE TABLE call_media_variants (
    media_variant_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    variant TEXT NOT NULL CHECK (variant IN ('redacted_audio','redacted_video')),
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    privacy_policy_version_uuid UUID NULL
        REFERENCES transcription_privacy_policy_versions(privacy_policy_version_uuid)
        ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('pending','processing','ready','failed','deleting')),
    storage_path TEXT NULL,
    file_name TEXT NULL,
    mime_type TEXT NULL,
    size_bytes BIGINT NULL CHECK (size_bytes IS NULL OR size_bytes > 0),
    processor_contract TEXT NOT NULL DEFAULT 'ffmpeg-redaction-v1',
    container TEXT NULL,
    audio_codec TEXT NULL,
    video_codec TEXT NULL,
    video_stream_copied BOOLEAN NULL,
    output_duration_ms BIGINT NULL CHECK (output_duration_ms IS NULL OR output_duration_ms >= 0),
    source_fingerprint BYTEA NOT NULL CHECK (octet_length(source_fingerprint)=32),
    intervals_sha256 BYTEA NOT NULL CHECK (octet_length(intervals_sha256)=32),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ NULL,
    locked_by TEXT NULL,
    last_error_code TEXT NULL,
    last_error_message_safe TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ NULL,
    UNIQUE (call_uuid, variant, transcription_revision, intervals_sha256, processor_contract),
    CHECK (
        (status='ready' AND storage_path IS NOT NULL AND file_name IS NOT NULL
            AND mime_type IS NOT NULL AND size_bytes IS NOT NULL
            AND container IS NOT NULL AND audio_codec IS NOT NULL
            AND output_duration_ms IS NOT NULL AND completed_at IS NOT NULL)
        OR status <> 'ready'
    ),
    CHECK (
        (variant='redacted_audio' AND video_codec IS NULL AND video_stream_copied IS NULL)
        OR
        (variant='redacted_video' AND (
            status <> 'ready' OR (video_codec IS NOT NULL AND video_stream_copied IS NOT NULL)
        ))
    )
);

CREATE INDEX idx_media_variants_worker
    ON call_media_variants(status, available_at, media_variant_uuid)
    WHERE status IN ('pending','failed');
```

`source_fingerprint` вычисляется потоково из исходного файла и его размера. Path
сам по себе не является fingerprint. Изменение filter graph, padding, codec matrix
или validation rules требует нового `processor_contract`; готовый artifact старой
версии не переиспользуется как результат новой.

### 8.8. `privacy_mutation_idempotency`

```sql
CREATE TABLE privacy_mutation_idempotency (
    actor_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 16 AND 200),
    request_sha256 BYTEA NOT NULL CHECK (octet_length(request_sha256)=32),
    state TEXT NOT NULL CHECK (state IN ('processing','completed','failed_retryable')),
    response_status INTEGER NULL CHECK (response_status BETWEEN 200 AND 599),
    response_body JSONB NULL,
    resource_uuid UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (actor_user_uuid, operation, idempotency_key),
    CHECK ((state='completed') = (response_status IS NOT NULL AND response_body IS NOT NULL))
);
```

Repository захватывает key до side effect. Тот же key и тот же exact canonical
body возвращает сохранённый status/body; другой body получает
`409 idempotency_key_reused`. Retryable failure не кэширует provider error body.
TTL — не меньше максимального retry window операции и задаётся конфигурацией.

### 8.9. `media_access_sessions`

```sql
CREATE TABLE media_access_sessions (
    media_access_session_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    actor_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    variant TEXT NOT NULL CHECK (variant IN ('original','redacted')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_access_session_expiry
    ON media_access_sessions(expires_at);
```

Session UUID не является bearer-token и не заменяет обычную авторизацию. Каждый
Range request заново проверяет login, actor binding, call ACL, variant capability и
expiry. Сессия нужна только для дедупликации аудита множества Range requests.

### 8.10. `privacy_audit_events`

```sql
CREATE TABLE privacy_audit_events (
    privacy_audit_uuid UUID PRIMARY KEY,
    scope_type TEXT NOT NULL,
    scope_uuid UUID NOT NULL,
    call_uuid UUID NULL REFERENCES calls(call_uuid) ON DELETE SET NULL,
    actor_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user','service_account','system','support_grant')),
    event_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_uuid UUID NULL,
    metadata_redacted JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(metadata_redacted)='object'),
    deduplication_key TEXT NULL,
    request_id TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_privacy_audit_scope_time
    ON privacy_audit_events(scope_type, scope_uuid, created_at DESC, privacy_audit_uuid DESC);
CREATE INDEX idx_privacy_audit_call_time
    ON privacy_audit_events(call_uuid, created_at DESC)
    WHERE call_uuid IS NOT NULL;
CREATE UNIQUE INDEX uq_privacy_audit_deduplication
    ON privacy_audit_events(event_type, deduplication_key)
    WHERE deduplication_key IS NOT NULL;
```

Audit events: `policy_published`, `redaction_started`, `redaction_completed`,
`redaction_failed`, `mask_added`, `mask_corrected`, `original_media_opened`,
`sanitized_media_requested`, `sanitized_media_ready`, `sanitized_media_failed`,
`redacted_export_created`, `provider_delete_requested`, `provider_delete_completed`.

Media player выполняет несколько Range requests. Audit `original_media_opened`
получает `deduplication_key` из `(call, actor, media_access_session_uuid)` и не
создаётся на каждый range.

## 9. Backend provider contract

### 9.1. Provider-neutral request

Текущие `Transcribe`/`TranscribeForMode` заменяются единым внутренним контрактом:

```go
type TranscriptionRequest struct {
    File              File
    Mode              TranscriptionMode
    SpeakerCandidates []SpeakerCandidate
    Privacy           *TranscriptionPrivacyRequest
}

type TranscriptionPrivacyRequest struct {
    MarkerContract string
    EntityTypes    []RedactionEntityType
}

type TranscriptionResult struct {
    ProviderJobID string
    Text          string
    Segments      []TranscriptionSegment
    Words         []TranscriptionWord
    Language      *string
    Redactions    []TranscriptionRedaction
}
```

Mock, AssemblyAI и будущие providers обязаны реализовать одинаковые invariants.
Privacy config не прячется в `models.File` и не зависит от HTTP DTO.

### 9.2. AssemblyAI request

При активной policy adapter отправляет:

```json
{
  "redact_pii": true,
  "redact_pii_policies": ["person_name", "phone_number"],
  "redact_pii_sub": "entity_name",
  "redact_pii_return_unredacted": false
}
```

`redact_pii_audio` в v1 не отправляется. Остальные transcription, language,
diarization и speaker identification параметры сохраняются.

Privacy-enabled request не включает provider features, способные вернуть
дополнительный немаскированный текст (`summary`, entity outputs и аналогичные
auxiliary properties). Adapter декодирует allowlist полей `text`, `words`,
`utterances`, language, status и safe diagnostics; неизвестные content-bearing
поля не сохраняются и не логируются. Нужное speech understanding выполняется уже
в VerbaTrace над русской маскированной транскрипцией.

Немаскированные `text`, `words`, `utterances` намеренно не запрашиваются и не
сохраняются. Оригинал доступен только как media согласно ACL.

### 9.3. Нормализация результата

Adapter выполняет до возврата `TranscriptionResult`:

1. принимает provider response только после terminal `completed`;
2. проверяет, что `text`, `words` и diarized `utterances` присутствуют согласно
   выбранному transcription mode;
3. распознаёт только allowlisted provider markers для реально запрошенных policy;
4. заменяет provider marker на marker `ru-v1` во всех трёх представлениях;
5. создаёт structured redaction spans из provider words и timestamps;
6. объединяет соседние words одной entity, если gap не больше 250 ms;
7. пересобирает segments так, чтобы marker и word indexes совпадали;
8. проверяет `0 <= start <= end <= duration + tolerance`;
9. отклоняет неизвестный provider marker, marker без timestamps и расхождение
   marker counts;
10. не пишет provider response body в error/log.

Marker detection не выполняется общим regexp по пользовательскому тексту после
сохранения. Structured flag создаётся внутри adapter-а только в ответе на
privacy-enabled provider request.

### 9.4. Ошибки provider contract

- unknown marker: `privacy_unknown_provider_marker`;
- missing word timings: `privacy_marker_timing_missing`;
- inconsistent result: `privacy_provider_result_inconsistent`;
- unsupported language/model: `privacy_provider_unsupported`;
- transient provider failure: `privacy_provider_temporarily_unavailable`;
- permanent invalid policy: `privacy_policy_not_supported`.

Transient ошибки повторяются existing job policy. Permanent ошибки не retry-ятся
до изменения config/code. При `required` call не переходит в `transcribed`.

### 9.5. Provider cleanup

После атомарного сохранения local transcription и spans backend ставит
успешную `transcription_provider_attempts` в `delete_pending`. Отдельный worker
вызывает provider DELETE, повторяет transient ошибки с exponential backoff и
сохраняет только safe code.

Локальный успех не откатывается из-за временной ошибки удаления у provider, но
alert остаётся активным до `deleted`. Удаление call также ставит cleanup request,
если provider artifact ещё существует.

## 10. Processing lifecycle

### 10.1. Admission

В одной транзакции создания call:

1. определяется placement;
2. выбирается active privacy policy version;
3. проверяется entitlement;
4. сохраняется `call_privacy_states` с canonical snapshot;
5. создаётся transcription job с immutable policy reference.

Если active policy требует недоступную тарифу функцию, HTTP upload возвращает 402
`privacy_feature_unavailable`. Integration item получает non-retryable safe error и
не создаёт незамаскированную транскрипцию. Silent downgrade запрещён.

### 10.2. Transcription

```text
new
  -> privacy queued
  -> call processing + privacy processing
  -> provider completed
  -> normalize Russian markers + validate spans
  -> transaction: transcription content + revision + spans + privacy ready
  -> call transcribed
  -> enqueue analysis
```

Транзакция публикации включает `MarkTranscribed`, revision content, spans и
`call_privacy_states.ready`. Если любой insert/check не проходит, ни одна рабочая
транскрипция не становится доступной.

### 10.3. Retry и crash recovery

- existing processing job остаётся source of truth для transcription retry;
- до provider submit worker создаёт `transcription_provider_attempts` в статусе
  `submitting`; полученный job ID сохраняется до первого poll;
- при сохранённом provider job ID worker продолжает poll той же attempt;
- timeout после отправки request, но до получения job ID считается ambiguous
  submit: автоматическая повторная отправка запрещена, пока adapter не умеет
  проверить или дедуплицировать запрос; attempt попадает в manual reconciliation;
- новый provider job создаётся только после terminal retryable failure предыдущей
  attempt либо подтверждённого отсутствия ambiguous job;
- повторная нормализация детерминирована;
- ready state с совпадающим revision/hash возвращает success;
- ready state с другим snapshot/hash возвращает 409 и требует reconciliation;
- worker claim использует `FOR UPDATE SKIP LOCKED`, lease и `available_at`;
- stale lease возвращается в queue, backoff использует jitter и bounded attempts;
- stuck `processing` обнаруживается monitoring по lease/started_at.

### 10.4. Policy change

Новая policy version действует только для calls, созданных после publish commit.
In-flight calls используют старый snapshot. Изменение policy не делает старую
транскрипцию misleading: call detail показывает номер и дату применённой версии.

## 11. Контракт анализа

### 11.1. Analysis input

`models.AnalysisRequest` расширяется:

```go
type AnalysisRedactionContext struct {
    MarkerContract string
    PresentMarkers []AnalysisRedactionMarker
}

type AnalysisRedactionMarker struct {
    Marker     string
    EntityType RedactionEntityType
    Count      int
}
```

Raw values отсутствуют. Список markers формируется из persisted spans той же
transcription revision, а не повторным поиском строки.

### 11.2. Обязательный системный prompt

Перед инструкциями пользователя backend добавляет неизменяемый блок:

```text
В расшифровке могут встречаться русские маркеры скрытых данных.
Маркер подтверждает, что соответствующее значение было произнесено, но само
значение недоступно модели.

[ИМЯ] означает произнесённое имя человека.
[ТЕЛЕФОН] означает произнесённый номер телефона.
[ЭЛЕКТРОННАЯ_ПОЧТА] означает произнесённый адрес электронной почты.

Не считай маркер пропуском распознавания и не восстанавливай скрытое значение.
Если критерий проверяет сам факт упоминания, маркер является подтверждением.
Если критерий требует точного скрытого значения, используй status not_evaluable,
points_awarded 0, points_max 0 и русское объяснение «Точное значение скрыто
политикой защиты данных».
```

В prompt перечисляются только marker types, реально присутствующие в звонке.
Additional instructions не могут отменить эти правила.

### 11.3. Strict analysis schema

`criteria_results[].status` расширяется значением `not_evaluable`.

Инварианты server normalization:

- `not_evaluable` всегда имеет `points_awarded=0`, `points_max=0`;
- такой критерий исключается из denominator как `not_applicable`, но в UI и
  аналитике учитывается отдельно;
- `issue` и `recommendation` остаются русскими;
- `evidence_quotes` может содержать marker и соседний подтверждённый контекст;
- модель не может вернуть raw value, которого не было в redacted prompt;
- invalid combination нормализуется к `not_evaluable`, а не к `missed`.

Analysis response получает `analysis_schema_version: 2` и новый
`not_evaluable_count`. Само поле count additive, но расширение enum `status`
является контрактно значимым изменением. Поэтому frontend с поддержкой schema v2
выкатывается до включения privacy flag; неизвестный status старый клиент обязан
показывать как «Статус недоступен», а не считать `missed`. Public API schema и SDK
версионируются одновременно.

### 11.4. Примеры

- «Меня зовут `[ИМЯ]`» подтверждает представление сотрудника;
- «Позвоните по номеру `[ТЕЛЕФОН]`» подтверждает передачу номера;
- `[ДЕНЕЖНАЯ_СУММА]` подтверждает факт называния суммы, но не позволяет проверить
  лимит; такой exact-value criterion становится `not_evaluable`;
- поскольку money redaction выключен по умолчанию, обычная проверка цены продолжает
  видеть «150 000 рублей».

## 12. Редактирование и ручная коррекция

### 12.1. Обычный transcription editor

`TranscriptionWordResponse` получает:

```json
{
  "redaction": {
    "span_uuid": "uuid",
    "entity_type": "person_name",
    "label": "Имя",
    "marker": "[ИМЯ]"
  }
}
```

Frontend отображает marker как non-editable chip. Backend возвращает 422
`redacted_word_edit_forbidden`, если обычный PATCH пытается изменить word внутри
активного span.

### 12.2. Privacy correction workflow

Отдельный endpoint позволяет:

- `add_mask` — заменить выбранный непрерывный word range русским marker;
С 05.09.2026 существующие маски неизменяемы: `change_category` и `remove_mask`
отклоняются сервером, в интерфейсе этих действий нет. Выбор старой версии также
запрещён, если он снимает или меняет хотя бы одну текущую маску.

Каждая операция требует:

- `expected_revision`;
- reason 10–500 символов;
- capability `can_review_redactions`;
- preview ответа без commit;
- новую transcription revision и privacy audit event;
- безопасный reanalysis через существующий revision-aware flow.

Span indexes/timestamps вычисляет backend. Клиент не может прислать произвольные
seconds. `add_mask` принимает word indexes и entity type.

## 13. Очищенные media-копии

### 13.1. Когда создаются

Sanitized media не создаётся при каждом звонке. Job создаётся, когда:

- пользователь с правом экспорта запрашивает «Скачать очищенную запись»;
- пользователь может читать call, но policy запрещает ему original media;
- manager заранее выбирает подготовку конкретной записи.

Повторный запрос возвращает существующий artifact или его текущий status.

### 13.2. Интервалы

Worker получает spans активной transcription revision, сортирует, объединяет
пересекающиеся интервалы и добавляет по 80 ms с каждой стороны, не выходя за
`[0,duration]`. Padding является versioned processor setting.

Если spans отсутствуют, sanitized artifact всё равно создаётся как проверенная
копия без изменения звука и получает `intervals_sha256` пустого canonical list.
Это позволяет restricted user открыть запись без fallback на original endpoint.

### 13.3. FFmpeg pipeline

- source всегда `calls.audio_path`, не ASR cache;
- временный файл создаётся рядом с target с suffix `.partial`;
- publish выполняется atomic rename после ffprobe validation;
- для audio interval заменяется коротким нейтральным сигналом, а не бесшумным
  пропуском: пользователь понимает, что фрагмент скрыт;
- privacy-изменение применяется только к audio stream; visual stream не получает
  масок или вырезаний и копируется без перекодирования, когда codec совместим;
- output duration отличается от source не более чем на 100 ms;
- ни один interval не удаляется из timeline;
- worker имеет timeout, lease, bounded stderr и process cancellation;
- command arguments передаются массивом, не shell string.

Output matrix v1:

| Input | Sanitized output | Правило видеопотока |
|---|---|---|
| MP3/WAV/M4A/OGG | `audio/mpeg`, `.mp3`, 128 kbit/s | — |
| MP4/MOV с H.264 | `video/mp4`, `.mp4`, AAC 128 kbit/s | stream copy |
| WebM с VP8/VP9/AV1 | `video/webm`, `.webm`, Opus | stream copy |
| MKV с H.264 | `video/mp4`, `.mp4`, AAC 128 kbit/s | remux, stream copy |
| Иной поддерживаемый ffmpeg video codec | `video/mp4`, `.mp4`, H.264/AAC | bounded transcode |

Цель matrix — отдать браузеру MP4 или WebM, а не исходный MKV, который может не
воспроизводиться на frontend. Видеоряд никогда не размывается, не вырезается и не
получает privacy-фильтры. В редком fallback он может быть перекодирован только для
совместимого контейнера; metadata явно возвращает `video_stream_copied=false`.
Transcode выполняется отдельной ограниченной worker queue с CPU/memory/time limits.
`privacy_media_codec_unsupported` допустим только если установленный ffmpeg не
может безопасно декодировать source, а не просто потому, что stream copy невозможен.

### 13.4. Проверка результата

Перед ready worker проверяет:

- regular non-empty file;
- ожидаемый container/MIME;
- наличие audio stream;
- для video — наличие video stream;
- browser playback probe для итогового MP4/WebM codec profile;
- duration tolerance;
- число каналов и sample rate в допустимом диапазоне;
- отсутствие `.partial` после publish;
- возможность `OpenReadSeeker`.

## 14. ACL и capabilities

### 14.1. Разделение прав

Backend вычисляет независимо:

- `can_read_call`;
- `can_read_redacted_transcription`;
- `can_read_original_media`;
- `can_request_sanitized_media`;
- `can_export_redacted`;
- `can_export_original`;
- `can_manage_privacy_policy`;
- `can_review_redactions`.

Наличие одного права не подразумевает другое.

### 14.2. Правила v1

- personal policy управляет владелец;
- company policy управляет active company manager;
- department leader не меняет company policy;
- uploader может слушать original своего call, если company policy явно не выбрала
  более строгий режим;
- `call_acl` сохраняет current call visibility;
- support access требует отдельный resource `privacy_original_media` и command
  allowlist; обычный admin permission недостаточен;
- service account получает только redacted transcription через existing
  `calls:read`;
- original transcription API для service accounts в v1 отсутствует.

### 14.3. Fail closed

Если restricted user запрашивает original:

- backend возвращает 403 `original_media_forbidden`;
- response не содержит storage path или original filename;
- frontend не показывает кнопку и не делает probe request;
- missing sanitized artifact ставится в job, UI показывает processing;
- ошибка sanitized job не вызывает fallback на original.

## 15. HTTP API

Все mutable endpoints используют JSON, stable error envelope, `If-Match` там, где
есть `lock_version`, и `Idempotency-Key` для create/publish/generate операций.

Общие правила контракта:

- mutation request содержит `schema_version`; в v1 допустимо только значение `1`;
- неизвестные поля mutation request отклоняются с 422, read response может
  получать только additive optional fields;
- enum в write request строгий; frontend имеет явный safe fallback для неизвестного
  enum в read response;
- UUID передаются как lowercase canonical strings, время — UTC RFC 3339;
- `null`, отсутствующее поле и пустой список не считаются взаимозаменяемыми;
- error envelope всегда имеет форму
  `{"error":{"code":"stable_code","message":"русский безопасный текст","request_id":"...","details":{}}}`;
- `details` не содержит provider payload, path или скрытое значение.

### 15.1. Policy

```text
GET  /api/v1/privacy-policies/personal
PUT  /api/v1/privacy-policies/personal/draft
POST /api/v1/privacy-policies/personal/preview
POST /api/v1/privacy-policies/personal/publish

GET  /api/v1/companies/{company_uuid}/privacy-policy
PUT  /api/v1/companies/{company_uuid}/privacy-policy/draft
POST /api/v1/companies/{company_uuid}/privacy-policy/preview
POST /api/v1/companies/{company_uuid}/privacy-policy/publish
GET  /api/v1/companies/{company_uuid}/privacy-policy/versions
GET  /api/v1/companies/{company_uuid}/privacy-policy/versions/{version}
```

GET response содержит active version, draft, capabilities и только русские
display labels.

`PUT draft` принимает полный config, а не JSON Merge Patch. Response возвращает
полный draft, `lock_version` и ETag. `preview_hash` привязан к canonical config,
scope, active version и server-side marker catalog version; после любого их
изменения publish получает `409 privacy_preview_stale`.

Preview response:

```json
{
  "preview_hash": "base64url-sha256",
  "effective_config": {},
  "warnings": [
    {
      "code": "money_analysis_limited",
      "title": "Денежные суммы будут скрыты",
      "message": "Анализ не сможет проверять точные цены и лимиты."
    }
  ],
  "sample": {
    "before": "Меня зовут Анна, телефон +7 900 000-00-00, стоимость 150 000 рублей.",
    "after": "Меня зовут [ИМЯ], телефон [ТЕЛЕФОН], стоимость 150 000 рублей."
  }
}
```

### 15.2. Call privacy

```text
GET  /api/v1/calls/{call_uuid}/privacy
POST /api/v1/calls/{call_uuid}/privacy-corrections/preview
POST /api/v1/calls/{call_uuid}/privacy-corrections
GET  /api/v1/calls/{call_uuid}/privacy-audit
```

`CallResponse` получает additive block:

```json
{
  "privacy": {
    "status": "ready",
    "protected": true,
    "marker_contract": "ru-v1",
    "policy_version": 3,
    "policy_source_label": "Политика компании",
    "detected_spans": 4,
    "recommended_media_variant": "original",
    "sanitized_media_status": "not_requested",
    "capabilities": {
      "can_read_original_media": true,
      "can_request_sanitized_media": true,
      "can_review_redactions": true
    }
  }
}
```

Frontend не переводит backend enum напрямую; все видимые labels определены русским
presentation map и покрыты exhaustive tests.

### 15.3. Media

```text
POST /api/v1/calls/{call_uuid}/media-variants/redacted
GET  /api/v1/calls/{call_uuid}/media-variants/redacted
POST /api/v1/calls/{call_uuid}/media-access-sessions
GET  /api/v1/calls/{call_uuid}/media?variant=recommended
GET  /api/v1/calls/{call_uuid}/media?variant=original
GET  /api/v1/calls/{call_uuid}/media?variant=redacted
```

- `recommended` выбирает backend по ACL и policy;
- old `/audio` alias использует `recommended`, а не безусловный original;
- `original` требует отдельной capability;
- `POST media-variants/redacted` идемпотентно создаёт job и возвращает 202 с
  metadata, а при готовом artifact — 200 с той же response shape;
- `GET media-variants/redacted` всегда возвращает 200 metadata со статусом
  `not_requested|pending|processing|ready|failed`;
- media `GET ...?variant=redacted` возвращает только bytes готового artifact; до
  готовности возвращает 409 `redacted_media_not_ready` и никогда не запускает job;
- frontend перед первым воспроизведением вызывает `POST media-access-sessions` с
  `{"variant":"original|redacted"}`, получает UUID с TTL 15 минут и добавляет его
  как `access_session` к media URL; UUID не даёт доступ без session auth;
- Range semantics сохраняются для ready artifacts;
- `Cache-Control: private, no-store`;
- URL не содержит storage path;
- filename для redacted variant заканчивается на `-очищено`.

### 15.4. Transcription

Existing `GET /calls/{uuid}/transcription` остаётся основным и возвращает redacted
content. Additive fields:

```json
{
  "redaction": {
    "status": "ready",
    "marker_contract": "ru-v1",
    "spans_count": 4,
    "entity_counts": [
      {"entity_type": "person_name", "label": "Имя", "marker": "[ИМЯ]", "count": 2}
    ]
  }
}
```

`words[].redaction` заполняется по structured spans. Для legacy transcription
`redaction.status=legacy_unprotected`, а не `ready` с нулевым count.

### 15.5. Reports and exports

`POST /calls/{uuid}/reports` получает optional:

```json
{"format":"pdf","privacy_variant":"redacted"}
```

- default всегда `redacted`;
- `original` доступен только если policy разрешает и actor имеет capability;
- в v1 original report не включает unredacted transcript, потому что он не
  хранится; значение `original` относится только к вложенному media, если такой
  тип экспорта появится. Для текущих PDF/DOCX/MD/XLSX допустим только `redacted`;
- duplicate key `call_report_exports` расширяется `privacy_variant` и
  `transcription_revision`;
- report metadata показывает «Транскрипция защищена, политика vN»;
- aggregate reports используют только redacted call data.

### 15.6. Stable error codes

| HTTP | Code | Значение |
|---:|---|---|
| 402 | `privacy_feature_unavailable` | тариф не допускает policy |
| 403 | `privacy_policy_forbidden` | нет права управления |
| 403 | `original_media_forbidden` | нет права на оригинал |
| 409 | `privacy_policy_revision_conflict` | устаревший If-Match |
| 409 | `privacy_preview_stale` | config изменился после preview |
| 409 | `idempotency_key_reused` | key повторён с другим request body |
| 409 | `redacted_media_not_ready` | artifact ещё не готов |
| 409 | `privacy_reanalysis_required` | correction сохранена, analysis устарел |
| 422 | `privacy_policy_invalid` | invalid config |
| 422 | `redacted_word_edit_forbidden` | обход через обычный editor |
| 422 | `privacy_media_codec_unsupported` | нет безопасного media pipeline |
| 503 | `privacy_provider_temporarily_unavailable` | transient provider failure |

Пользовательские messages на frontend только русские и не содержат provider name,
если это не diagnostic page manager-а.

## 16. Frontend UX

### 16.1. Навигация

Добавить `settingsPrivacy` и маршрут `/app/settings/privacy`.

В `Настройки` появляется карточка:

```text
Защита данных
Маскирование транскрипций, доступ к оригиналам и безопасный экспорт.
```

Страница использует текущие `app-page-heading`, `glass-panel`, `InfoCard`,
`SelectControl`, `ConfirmDialog`, semantic theme tokens и существующую шкалу
отступов. Новая визуальная система не вводится.

### 16.2. Страница «Защита данных»

Верхний scope switcher:

- «Личные звонки»;
- доступные компании;
- company policy read-only для не-менеджера.

Блоки в порядке:

1. **Состояние** — включено/выключено, active version, дата, кто опубликовал.
2. **Что скрывать** — русские checkbox labels, сгруппированные как «Контакты и
   личность», «Документы и финансы», «Дополнительные категории».
3. **Денежные суммы** — отдельный danger-aware switch, по умолчанию выключен, с
   явным текстом о потере проверки цены.
4. **Оригинальная запись** — selector доступа и объяснение, что запись не
   изменяется.
5. **Очищенная копия** — on-demand, только экспорт/restricted users.
6. **Предпросмотр** — русское before/after без реальных данных пользователя.
7. **Публикация** — summary dialog, reason и warnings.

Draft auto-save не публикует изменения. Unsaved navigation показывает confirm.
409 conflict предлагает «Обновить настройки», не перезаписывает чужой draft.

### 16.3. Call detail

В карточке транскрипции:

- chip «Личные данные скрыты»;
- tooltip «Скрыто: имена — 2, телефоны — 1»;
- markers выделены нейтральным privacy-chip, а не error/warning цветом;
- hover/focus показывает русское название категории, но не исходное значение;
- screen reader получает `aria-label="Скрыто: имя"`;
- legacy звонок показывает «Маскирование не применялось к этому звонку»;
- failed required policy показывает blocking state, а не пустую транскрипцию.

В media card:

- authorized user по умолчанию слушает «Оригинал»;
- если ready sanitized copy существует, появляется segmented control
  «Оригинал / Очищенная копия»;
- restricted user видит только «Очищенная копия»;
- processing state показывает progress text без fake percentage;
- failure показывает retry только при capability;
- переключение variant сохраняет текущий playback time в пределах duration.

### 16.4. Transcription editor

- marker рендерится как chip внутри word flow;
- ordinary input disabled для marker;
- context action «Проверить маску» видна только reviewer capability;
- add mask работает через выделение непрерывного диапазона;
- remove mask требует typed replacement, reason и confirm;
- после correction UI явно предлагает безопасный повторный анализ;
- revision compare различает «текст исправлен» и «маска изменена».

### 16.5. Upload и integrations

Upload page показывает effective policy рядом с placement:

```text
Для этого звонка будут скрыты имена, телефоны, email и реквизиты.
Денежные суммы останутся доступны анализу.
```

Пользователь не может ослабить required company policy на одном upload.
Integration page показывает policy source у connection placement. API caller не
передаёт произвольный override в v1.

### 16.6. Responsive и accessibility

- desktop content max-width соответствует текущим settings pages;
- на ширине до 720 px scope switcher горизонтально прокручивается, cards идут в
  одну колонку;
- focus order совпадает с visual order;
- все dialogs имеют focus trap, `Esc`, restore focus и labelled title;
- state не передаётся только цветом;
- animation учитывает `prefers-reduced-motion`;
- light/dark используют существующие variables, hardcoded белые поверхности
  запрещены.

## 17. Безопасность

- provider API key остаётся server-side;
- provider job ID не виден обычному клиенту;
- unredacted transcript не запрашивается (`redact_pii_return_unredacted=false`);
- original media path не возвращается API;
- marker normalization uses exact allowlist, не dynamic labels;
- policy JSON имеет size/depth/type limits;
- entity arrays unique, bounded и canonicalized;
- ffmpeg получает validated absolute paths внутри storage root;
- temporary files имеют restrictive permissions и удаляются после error/cancel;
- no shell interpolation;
- audit metadata allowlisted;
- original media access audit дедуплицируется, но не отключается;
- support access использует expiry, resource/command allowlist и revoke;
- sanitized artifact не считается доказательством отсутствия PII: detector может
  ошибаться, UI не обещает абсолютное обезличивание.

## 18. Retention и удаление

Existing retention manifest расширяется:

- `call_privacy_states`;
- `call_transcription_redaction_spans`;
- `call_media_variants`;
- privacy-aware report variants;
- provider cleanup state.

Порядок удаления:

1. запретить новые media variant jobs;
2. удалить DB graph в call purge transaction;
3. удалить original, ASR cache, sanitized artifacts и reports отдельными
   idempotent storage steps;
4. выполнить/дождаться provider delete request;
5. записать redacted completion audit без title, transcript и path.

Ошибка удаления одного sanitized artifact не блокирует попытки остальных. Call
purge остаётся incomplete, пока все известные storage artifacts не удалены либо не
помечены `not_found` после безопасной проверки.

Policy version не удаляется, пока на неё ссылается call snapshot. При удалении
company она становится read-only tombstone до исчезновения последней ссылки.

## 19. Billing и entitlement

Добавить plan capabilities:

- `pii_redaction_enabled`;
- `sanitized_media_enabled`;
- optional monthly `sanitized_media_minutes_limit`.

Стоимость provider PII feature должна входить в transcription reservation до
вызова provider. Local sanitized media учитывает длительность и CPU отдельно, если
для неё вводится лимит.

Нельзя:

- вызвать provider, а затем обнаружить отсутствие credits;
- при окончании подписки тихо транскрибировать без required policy;
- повторно списывать credits за idempotent retry того же provider job;
- показывать sanitized media как готовое до storage validation.

## 20. Наблюдаемость

Metrics без company/call UUID labels:

- `privacy_redaction_jobs_total{status,error_code}`;
- `privacy_redaction_duration_seconds`;
- `privacy_redaction_spans_total{entity_type}`;
- `privacy_provider_delete_jobs_total{status,error_code}`;
- `privacy_media_jobs_total{variant,status,error_code}`;
- `privacy_media_duration_seconds{variant}`;
- `privacy_policy_publish_total{scope_type}`;
- `privacy_analysis_not_evaluable_total{criterion_code}` с bounded code allowlist;
- `privacy_original_media_access_total{scope_type}`.

Alerts:

- required redaction failure rate выше согласованного threshold;
- provider deletion oldest pending age выше SLO;
- sanitized media stuck processing;
- unknown provider marker больше нуля;
- privacy-ready transcription без spans row при marker в text;
- restricted original access denial unexpectedly падает до нуля после rollout.

Structured logs содержат request/job/call UUID, status, safe code, duration и count,
но не marker context, filename, transcript, phone/email/name или provider body.

## 21. Миграция и rollout

### 21.1. Compatibility migration

1. Создать новые таблицы без включения policy.
2. Для существующих calls создать `call_privacy_states` со статусом
   `legacy_unprotected` и source `none` малыми батчами.
3. Добавить nullable additive DTO fields.
4. Обновить report uniqueness с online-safe index transition.
5. Deploy read path до write path.

Старые транскрипции не сканируются regexp и не получают ложный статус ready.

### 21.2. Feature flags

- `PRIVACY_REDACTION_ENABLED` — глобальный kill switch admission-а;
- `PRIVACY_POLICY_UI_ENABLED` — показ settings page;
- `PRIVACY_SANITIZED_MEDIA_ENABLED` — worker/create endpoints;
- `PRIVACY_PROVIDER_DELETE_ENABLED` — cleanup worker.

Kill switch не отключает чтение уже готовых redacted artifacts. При active required
policy и выключенном admission feature новые uploads блокируются safe error.

### 21.3. Rollout sequence

1. migrations + read compatibility;
2. provider request/normalization contract tests;
3. shadow benchmark на synthetic/redacted fixtures без публикации пользователю;
4. personal opt-in internal users;
5. одна test company;
6. sanitized media audio formats;
7. sanitized media video formats;
8. company opt-in;
9. provider cleanup worker;
10. production SLO review.

### 21.4. Existing calls

Автоматический backfill запрещён. Отдельный последующий flow должен:

- показать число звонков, минут, ожидаемый credit cost и затрагиваемые analyses;
- создать immutable batch;
- повторно транскрибировать original media под выбранной policy;
- не заменять активную transcription/analysis до полного успеха;
- дать preview и atomic promotion;
- поддерживать cancel только до provider submission;
- иметь собственный audit/reconciliation.

До этого legacy calls остаются явно помеченными.

## 22. Тестирование

### 22.1. Backend unit

- mapping каждой provider policy в русский marker;
- money/organization отсутствуют в default set;
- unknown marker fail closed;
- text/segments/words normalization consistent;
- adjacent span merge и time padding boundaries;
- ordinary edit redacted word rejected;
- manual add/change/remove mask creates revision;
- prompt содержит только present markers;
- `not_evaluable` нормализуется в 0/0 и исключается из denominator;
- policy canonicalization/hash/idempotency;
- ACL capability matrix;
- no original fallback;
- report always uses redacted transcription;
- provider cleanup retry classification.

### 22.2. Repository/integration PostgreSQL

- scope ownership constraints;
- immutable version and active pointer;
- concurrent publish conflict;
- atomic transcription+spans+privacy ready;
- idempotent media job uniqueness;
- call cascade and policy RESTRICT behavior;
- retention manifest includes all artifacts;
- audit contains no raw PII fixtures.

### 22.3. Provider HTTP contract

Test server проверяет exact request:

- `redact_pii=true`;
- exact sorted policies;
- `redact_pii_sub=entity_name`;
- `redact_pii_return_unredacted=false`;
- no `redact_pii_audio` in v1;
- diarization/identification fields preserved;
- `ProviderJobID` persisted в отдельной provider attempt;
- DELETE invoked after local commit;
- timeout resumes polling same provider job.

Live paid provider test обязателен перед production flag. Mock test не подтверждает
русское качество detection.

### 22.4. FFmpeg integration

Fixtures: MP3, WAV, M4A, OGG, MP4, MOV, WebM, MKV.

Проверить:

- intervals реально заменены сигналом;
- соседний звук сохранён;
- duration drift <= 100 ms;
- video frames/stream присутствуют;
- retry reuses ready artifact;
- cancel removes `.partial`;
- unsupported codec получает stable code;
- filenames and paths cannot escape storage root;
- Range requests работают.

### 22.5. Frontend component/browser

- settings route/card и scope switcher;
- все visible labels/markers русские;
- money warning и publish confirmation;
- read-only company policy для employee;
- protected/legacy/failed call states;
- marker chip keyboard/screen reader;
- original/sanitized switch keeps playback time;
- restricted user never requests original URL;
- processing/error/retry states;
- editor cannot type into marker;
- correction preview/confirm/conflict;
- light/dark/mobile/reduced motion;
- no layout shift при загрузке privacy metadata.

### 22.6. Security regression

- IDOR original media;
- service account cannot request original;
- admin without support grant cannot read business original;
- malicious entity label rejected;
- transcript marker cannot inject prompt instruction;
- provider error body not logged;
- raw value absent from audit, metrics, webhook and report;
- original path absent from API;
- restricted media error never redirects to original.

## 23. Критерии готовности

Фича считается готовой только если одновременно выполнено:

1. Для нового privacy-enabled audio и video call provider получает exact policy.
2. API, frontend, analysis и reports используют одинаковые русские markers.
3. Money remains visible under default policy.
4. Analysis treats marker as presence and exact hidden value as `not_evaluable`
   without score penalty.
5. Ordinary editor cannot remove marker.
6. Manual privacy correction creates auditable revision and safe reanalysis.
7. Authorized user hears original by default.
8. Restricted user receives only validated sanitized media and never fallback.
9. Video sanitized variant masks only audio; video receives no privacy filters or
   cuts, and codec transcode is allowed only for browser compatibility.
10. All supported formats pass duration/stream/Range tests.
11. Provider artifact deletion is retried and observable.
12. Retention deletes every local derivative.
13. Legacy calls are visibly unprotected.
14. Backend CI, frontend build/tests, migration up/down on clean DB, integration
    tests, browser tests and `git diff --check` pass.
15. Live paid Russian AssemblyAI test confirms actual `text`, `words`, `utterances`
    marker shape before production enablement.

## 24. Порядок реализации

1. Data model, policy validator и compatibility read fields.
2. Provider-neutral request/result contract.
3. AssemblyAI flags, Russian marker normalization и spans.
4. Atomic processing lifecycle и provider cleanup.
5. Analysis prompt/schema/denominator changes.
6. Editor protection и privacy correction service.
7. Policy API и settings frontend.
8. Call detail/transcript marker UI.
9. Sanitized media worker and endpoints.
10. Report/export integration.
11. Retention, audit, metrics and alerts.
12. Full local CI, browser verification and paid provider acceptance.

Начинать с media worker до policy snapshot и structured spans нельзя: иначе
очищенная копия не будет воспроизводимой и её невозможно будет честно связать с
конкретной transcription revision.

## 25. Официальные provider-ограничения

- AssemblyAI PII redaction поддерживает `entity_name`, при котором значения
  заменяются type markers, и позволяет выбирать policies:
  <https://www.assemblyai.com/docs/guardrails/redact-pii-from-transcripts>.
- При обычном redaction provider возвращает redacted `text`, `words` и
  `utterances`; unredacted variants появляются только при явном
  `redact_pii_return_unredacted=true`. VerbaTrace его не включает.
- Документация отдельно предупреждает, что результаты других provider features
  могут содержать PII. Поэтому privacy-enabled adapter использует allowlist ответа
  и не заказывает auxiliary content; одного `redact_pii=true` для произвольного
  provider payload недостаточно.
- Provider redacted audio является отдельным artifact с ограниченным временем
  получения; поэтому VerbaTrace строит on-demand media локально из persisted
  spans и original media.
- Provider поддерживает удаление transcript data через API; VerbaTrace выполняет
  это durable worker-ом после локального commit:
  <https://www.assemblyai.com/docs/delete-transcripts>.
- Фактическая provider retention зависит от настроек аккаунта и договора; наличие
  cleanup endpoint не является обещанием мгновенного физического удаления:
  <https://www.assemblyai.com/docs/data-retention-and-model-training>.

## 26. Неопределённости, которые нельзя скрывать

- Технический paid acceptance на синтетическом русском звонке пройден, но качество
  PII detection на репрезентативной выборке реальных продаж ещё не измерено;
  перед массовым rollout нужен отдельный paid benchmark с precision/recall review.
- Автоматическая маскировка может иметь false positive и false negative, поэтому
  продукт предоставляет correction/audit, но не обещает абсолютное обезличивание.
- Exact provider pricing для PII feature должен быть проверен перед изменением
  credit reservation; спецификация не придумывает стоимость.
- Юридическая достаточность маскирования и право хранить original media зависят от
  принятой политики и консультации специалиста; техническая фича сама по себе это
  не подтверждает.
