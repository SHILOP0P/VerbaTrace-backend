# Спецификация: действия по итогам звонка, напоминания и создание компании

Статус: production design
Дата: 2026-08-14
Backend: `C:\projects\VerbaTrace\Monolit`
Frontend: `C:\projects\VerbaTrace-frontend`

## 1. Цель

VerbaTrace должен замкнуть рабочий цикл:

`звонок → проверяемый вывод → действие или явное отсутствие действия → назначение → напоминания → выполнение/отмена/просрочка → неизменяемая история`.

Функция не является ещё одним текстовым полем анализа. Действие — самостоятельный
объект с ответственным, сроком, организационной областью, доказательствами,
контролем доступа и жизненным циклом.

Одновременно поставка должна:

- дать пользователю без собственной компании явный путь её создания;
- перенести «Контакты» и «Настройки» из левого sidebar в меню профиля;
- встроить действия в существующие уведомления и административный контур;
- одинаково качественно работать в светлой и тёмной темах;
- сохранять сервер как единственный источник полномочий и состояния.

## 2. Проверенное исходное состояние

На момент написания спецификации:

- backend уже предоставляет `POST /api/v1/companies`;
- frontend уже имеет `api.createCompany` и встроенную `CreateCompanyForm`, но нет
  отдельного маршрута создания компании;
- роли организации уже называются `company_manager`, `department_leader` и
  `employee`;
- пользователь может состоять в нескольких отделах;
- backend имеет таблицу `notifications`, однако её CHECK разрешает только
  существующие четыре типа;
- frontend уже умеет показывать matched evidence, перематывать media и подсвечивать
  диапазон слов;
- кнопка «Выполнить действие» в карточке звонка не имеет обработчика;
- отдельной таблицы, API и lifecycle действий нет;
- sidebar содержит «Контакты», «Компании» и «Настройки»; меню профиля содержит
  только «Профиль» и «Выйти».

Спецификация расширяет эти контракты, а не создаёт параллельные роли, уведомления
или механизм evidence.

## 3. Границы поставки

### 3.1. Входит

- отдельная страница создания компании;
- условная кнопка создания компании для пользователя без собственной компании;
- меню профиля: «Профиль», «Контакты», «Настройки», «Выйти»;
- удаление «Контактов» и «Настроек» из sidebar;
- создание действия вручную из завершённого анализа звонка;
- явная фиксация «дальнейших действий не требуется»;
- поиск текста транскрипции и ручной выбор 0..N цитат;
- назначение по `@username` или поиску участника компании;
- автоматическое определение отделов выбранного ответственного;
- явный выбор исходного и целевого отделов при неоднозначности;
- статусный lifecycle, optimistic locking и append-only audit;
- запрос на передачу ответственности и его согласование;
- изменение срока, отмена и закрытие с серверным RBAC;
- уведомления обо всех значимых изменениях;
- напоминания за 7, 5, 2 и 1 день;
- 24-часовое окно подтверждения после срока;
- отдельный почасовой worker перевода в `overdue`;
- пользовательский список и deep-linkable страница действия;
- административный список и карточка действия;
- responsive UI и visual QA обеих тем.

### 3.2. Не входит в первую поставку

- автоматическое создание задач без подтверждения человеком;
- двусторонняя синхронизация с Bitrix24, amoCRM или task trackers;
- email, push и мессенджеры — используются существующие внутренние уведомления;
- рекуррентные задачи;
- подзадачи и граф зависимостей;
- автоматическое продление срока;
- изменение текста исходного анализа;
- персональные действия вне компании.

Эти ограничения не должны закрывать будущую интеграцию: идентификаторы,
идемпотентность и события проектируются расширяемо.

## 4. Термины и инварианты

### 4.1. Термины

- **Действие** — подтверждённая человеком работа по результату звонка.
- **Disposition звонка** — `action_created` или `no_action_required` для конкретной
  версии анализа.
- **Исходный отдел** — отдел, из контекста которого действие создано.
- **Целевой отдел** — отдел, которому передана работа; может совпадать с исходным.
- **Ответственный** — один активный участник компании, обязанный выполнить действие.
- **Передача** — согласуемая замена ответственного.
- **Основной срок** — `due_at`.
- **Grace-период** — 24 часа `[due_at, due_at + 24h)` для фиксации фактически
  выполненной, но не закрытой работы.
- **Просрочка** — серверный статус после `due_at + 24h`, если действие не закрыто.

### 4.2. Главные инварианты

1. Действие принадлежит ровно одной компании.
2. Исходный и целевой отделы принадлежат этой же компании.
3. Ответственный является активным участником компании и активным участником
   целевого отдела на момент назначения.
4. Если у пользователя несколько активных отделов, backend не угадывает целевой
   отдел: frontend требует выбор, backend валидирует его.
5. Действие связано с неизменяемым snapshot: `call_uuid`, `analysis_uuid`,
   `analysis_attempt_uuid` при наличии и `transcription_revision`.
6. Evidence с таймкодом принимается только как диапазон слов указанной transcription
   revision. Произвольные клиентские timestamps не являются источником истины.
7. Автор действия не может расширить видимость звонка. Создание разрешено только
   при наличии доступа к звонку; последующий доступ к действию не предоставляет
   media-доступ пользователю, которому он запрещён.
8. Любое состояние и полномочие проверяются backend. Скрытая кнопка не считается
   защитой.
9. История событий append-only. Отмена не удаляет действие.
10. Один idempotency key создаёт не более одного действия/команды/уведомления.
11. Время хранится в UTC (`TIMESTAMPTZ`), отображается в timezone пользователя.
12. Изменения конкурентных клиентов разрешаются через `lock_version`.

## 5. Продуктовые сценарии

### 5.1. Компания отсутствует

«Собственная компания» означает активное членство пользователя с ролью
`company_manager`, а не просто отсутствие любых memberships.

Если управляемых компаний нет, `/app/companies` показывает empty state и кнопку
«Создать компанию». Кнопка ведёт на `/app/companies/new`.

Страница содержит:

- название компании;
- серверные ошибки и состояние отправки;
- «Создать компанию» и «Отмена»;
- объяснение, что создатель станет менеджером.

После успеха frontend обновляет workspace, делает `replaceState` на
`/app/companies/{uuid}` и показывает success toast. Двойная отправка блокируется UI
и idempotency key на сервере. Если лимит тарифа исчерпан, backend возвращает
стабильный код, форма сохраняет введённое название и ведёт к тарифам.

Пользователь, состоящий в чужой компании как employee, всё равно видит создание,
если тариф и серверные правила разрешают собственную компанию.

### 5.2. Выбор результата после звонка

Для завершённого анализа показываются две команды:

- «Создать действие»;
- «Действий не требуется».

`no_action_required` фиксируется отдельно для текущего `analysis_uuid` и требует
подтверждения. При новом активном анализе старый disposition остаётся в истории, но
не применяется автоматически. Пользователь может отменить disposition и создать
действие, если всё ещё имеет права.

Наличие одного действия не запрещает создать дополнительные действия. Disposition
становится `action_created`, когда существует хотя бы одно не удалённое действие
для snapshot. Отмена всех действий не превращает результат автоматически в
`no_action_required`.

### 5.3. Создание действия

Форма открывается drawer/modal поверх звонка на desktop и full-screen sheet на
mobile. Поля:

- `title` — обязательное, 1..200 Unicode code points;
- `description` — необязательное, до 10 000;
- ответственный;
- исходный отдел;
- целевой отдел;
- `due_at` — обязательный будущий момент;
- 0..20 evidence-фрагментов;
- client idempotency key.

ИИ-текст `next_step` используется только как редактируемая подсказка. Пользователь
явно подтверждает создание. Нельзя сохранять скрытый или автоматически созданный
черновик как активное действие.

### 5.4. Поиск и выбор цитат

Кнопка «Добавить цитату» открывает transcript picker:

- строка поиска с семантикой, привычной по `Ctrl+F`;
- literal case-insensitive поиск по нормализованному тексту;
- счётчик `текущее / всего` и переходы «предыдущее/следующее»;
- `Enter`/`Shift+Enter`, `Esc`, доступные aria-label;
- поиск не меняет исходный текст и не отправляет запрос на каждый символ;
- выбор последовательного диапазона слов мышью или клавиатурой;
- preview speaker, текста и времени;
- несколько непересекающихся или пересекающихся фрагментов допускаются, но точные
  дубликаты backend отклоняет/дедуплицирует детерминированно.

Frontend отправляет `transcription_revision`, `word_start_index` и включительный
`word_end_index`. Backend загружает revision, проверяет границы, сам вычисляет quote,
speaker, `start_seconds`, `end_seconds` и snapshot JSON. Для legacy-транскрипции без
words разрешена текстовая цитата без seek target, явно отмеченная как `legacy_text`.

Смена активной transcription revision не переписывает evidence существующего
действия: карточка показывает, что источник не является текущей версией, но
сохранённый snapshot остаётся проверяемым через историю revision.

### 5.5. Поиск ответственного и отделы

Autocomplete принимает `@username` и обычный текст. Поиск ограничивается company,
активными memberships и серверным лимитом 20 результатов. Он ищет username,
имя/фамилию и должность, но не раскрывает участников других компаний.

После выбора пользователя:

- 0 активных отделов: назначение запрещено;
- 1 активный отдел: он автоматически становится целевым;
- несколько отделов: пользователь обязан выбрать целевой;
- если выбранный отдел совпадает с отделом контекста звонка, действие внутреннее;
- иначе действие межотдельское, и оба отдела сохраняются явно.

Исходный отдел по умолчанию берётся из department-scoped звонка. Для company-scoped
звонка автор обязан выбрать исходный отдел, участником/лидером которого он является,
либо менеджер выбирает любой активный отдел компании. Personal calls исключены.

### 5.6. Выполнение

Статусы:

- `open` — создано и назначено;
- `in_progress` — ответственный начал работу;
- `completed` — выполнено;
- `cancelled` — отменено;
- `overdue` — grace-период истёк без закрытия.

`overdue` не терминален: ответственный или вышестоящая роль может позднее завершить
задачу. Завершение требует необязательного комментария до 5 000 символов и фиксирует
`completed_at`, `completed_by_user_uuid`. Повторное завершение идемпотентно.

Терминальные статусы `completed` и `cancelled` не редактируются обычными командами.
Возобновление — отдельная команда менеджера компании с обязательной причиной и
новым сроком; она возвращает `open`, увеличивает `lock_version` и создаёт audit event.

### 5.7. Передача ответственности

Ответственный не снимает себя напрямую. Он создаёт transfer request:

- обязательная причина 10..2 000 символов;
- необязательный предлагаемый пользователь;
- если кандидат указан, указывается его целевой отдел;
- одновременно допускается только один `pending` request.

До одобрения ответственный не меняется. Одобрить/отклонить могут:

- активный лидер текущего целевого отдела;
- при межотдельской передаче — лидер нового целевого отдела;
- менеджер компании.

Если новый исполнитель относится к другому отделу, одобрение требует лидера нового
целевого отдела или менеджера компании. Сам инициатор не может одобрить запрос, даже
если одновременно лидер; решение принимает другой допустимый approver или менеджер.

При одобрении одним transaction:

1. блокируется действие и pending request;
2. повторно проверяются memberships/роли;
3. меняются assignee и target department;
4. request становится `approved`;
5. создаются события и outbox-уведомления;
6. увеличивается `lock_version`.

Отклонение требует комментария. Запрос автоматически `withdrawn`, если действие
закрыто, отменено, переназначено другим полномочным лицом или membership кандидата
перестал быть активным. Автор может отозвать pending request.

### 5.8. Изменение срока, прямое переназначение и отмена

Лидер исходного/целевого отдела может изменять срок и назначать только активного
сотрудника своего отдела. Для переноса в другой отдел требуется лидер целевого
отдела либо менеджер компании. Менеджер может выполнять эти действия по всей
компании.

Администратор действует только при отдельном capability и не получает продуктовый
доступ из одного факта глобальной роли. Административное действие требует reason и
попадает в audit.

Отменить могут:

- автор;
- текущий ответственный;
- лидер исходного или целевого отдела;
- менеджер компании;
- администратор с capability.

Отмена всегда требует причины 10..2 000 символов. При прямом переназначении также
требуется причина. Изменение срока на более ранний момент не может поставить срок в
прошлом; для уже просроченной задачи задаётся новый будущий срок и статус становится
`open` отдельной командой reschedule.

### 5.9. Изменение memberships

- Ответственный suspended/left/removed: действие не теряется, получает
  `assignment_invalid`, уведомляются лидеры целевого отдела и менеджеры; worker не
  шлёт deadline reminders бывшему участнику.
- Отдел удалён soft-delete: открытые действия блокируют hard semantics удаления;
  UI требует перенести/закрыть их. Историческая ссылка сохраняется.
- Лидер потерял роль: новые решения запрещены немедленно; старые события сохраняются.
- Последний менеджер не может покинуть компанию при наличии открытых действий и без
  передачи управления согласно существующим правилам компании.
- Username изменён: связь остаётся по UUID, UI показывает актуальное имя и snapshot
  имени в audit event.

## 6. Матрица доступа

| Операция | Автор | Ответственный | Лидер исходного отдела | Лидер целевого отдела | Менеджер компании | Admin capability |
|---|---:|---:|---:|---:|---:|---:|
| Читать действие | да | да | да | да | да | `actions.read` |
| Создать | при доступе к call и source dept | — | да | — | да | нет по умолчанию |
| Начать/завершить | нет* | да | да | да | да | `actions.manage` |
| Запросить передачу | — | да | — | — | — | — |
| Одобрить передачу | нет | нет | по правилам | по правилам | да | `actions.manage` |
| Изменить срок | нет* | запрос | да | да | да | `actions.manage` |
| Прямо переназначить | нет* | нет | в свой отдел | в свой отдел | да | `actions.manage` |
| Отменить | да | да | да | да | да | `actions.manage` |
| Возобновить | нет | нет | нет | нет | да | `actions.manage` |

`*` — если пользователь одновременно имеет более сильную роль, применяется она.

Для чтения лидером достаточно, чтобы его активный отдел совпадал с source или target.
Лидер постороннего отдела не видит действие даже внутри той же компании. Все list
queries обязаны применять этот predicate в SQL до pagination.

## 7. Модель данных

### 7.1. `call_action_dispositions`

```sql
disposition_uuid       uuid primary key
company_uuid           uuid not null references companies
call_uuid              uuid not null references calls
analysis_uuid          uuid not null references call_analyses
analysis_attempt_uuid  uuid null
transcription_revision integer not null
kind                    text not null check (kind in ('action_created','no_action_required'))
reason                  text null
created_by_user_uuid    uuid not null references users
created_at              timestamptz not null
superseded_at           timestamptz null
```

Unique partial index: один активный disposition на `analysis_uuid`.

### 7.2. `call_actions`

```sql
action_uuid                 uuid primary key
company_uuid                uuid not null references companies
source_department_uuid      uuid not null references departments
target_department_uuid      uuid not null references departments
call_uuid                   uuid not null references calls
analysis_uuid               uuid not null references call_analyses
analysis_attempt_uuid       uuid null
transcription_revision      integer not null
title                       text not null
description                 text not null default ''
status                      text not null
assignment_state            text not null default 'valid'
assignee_user_uuid          uuid not null references users
due_at                      timestamptz not null
grace_expires_at            timestamptz generated always as (due_at + interval '24 hours') stored
lock_version                bigint not null default 1
created_by_user_uuid        uuid not null references users
created_at                  timestamptz not null
updated_at                  timestamptz not null
started_at                  timestamptz null
completed_at                timestamptz null
completed_by_user_uuid      uuid null references users
cancelled_at                timestamptz null
cancelled_by_user_uuid      uuid null references users
cancel_reason               text null
```

Checks обеспечивают согласованность terminal timestamps/reasons. FK отделов не
доказывают принадлежность компании, поэтому service валидирует её transactionally;
дополнительно предпочтительны composite unique `(department_uuid, company_uuid)` и
composite FK.

Индексы:

- `(assignee_user_uuid, status, due_at, action_uuid)` для «Мои»;
- `(company_uuid, status, updated_at desc, action_uuid)`;
- `(source_department_uuid, status, updated_at desc, action_uuid)`;
- `(target_department_uuid, status, updated_at desc, action_uuid)`;
- `(grace_expires_at, action_uuid) WHERE status IN ('open','in_progress')`;
- `(due_at, action_uuid) WHERE status IN ('open','in_progress')`;
- `(call_uuid, created_at desc)`.

### 7.3. `call_action_evidence`

```sql
evidence_uuid          uuid primary key
action_uuid            uuid not null references call_actions on delete cascade
position               integer not null
kind                   text not null check (kind in ('word_range','legacy_text'))
word_start_index       integer null
word_end_index         integer null
quote_snapshot         text not null
speaker_snapshot       text null
start_seconds          double precision null
end_seconds            double precision null
created_at             timestamptz not null
unique(action_uuid, position)
```

`word_end_index` включителен. Для `word_range` обязательны индексы и timestamps;
для `legacy_text` они NULL. Exact duplicate word ranges запрещены unique expression
index либо сервисной проверкой под lock.

### 7.4. `call_action_transfer_requests`

Содержит request UUID, action UUID, requester, proposed assignee/department,
reason, status `pending/approved/rejected/withdrawn`, resolver, resolution comment,
timestamps и `action_lock_version_at_create`. Partial unique index допускает один
pending request на действие.

### 7.5. `call_action_events`

Append-only журнал: action, monotonically increasing sequence, event type, actor,
actor role snapshot, reason, old/new JSONB с минимально необходимыми полями,
request/correlation ID и timestamp. Unique `(action_uuid, sequence)`.

Типы включают create, start, complete, cancel, reopen, reschedule, reassign,
transfer_requested/approved/rejected/withdrawn, assignment_invalid, reminder_sent,
grace_started и overdue.

### 7.6. `call_action_notification_deliveries`

```sql
delivery_uuid      uuid primary key
action_uuid        uuid not null references call_actions
recipient_uuid     uuid not null references users
kind               text not null
schedule_version   bigint not null
notification_uuid  uuid null references notifications
created_at         timestamptz not null
unique(action_uuid, recipient_uuid, kind, schedule_version)
```

`schedule_version` равен action `lock_version` последнего изменения срока/assignee,
имеющего влияние на расписание. Это позволяет корректно создать новый набор
напоминаний после переноса срока и не дублировать старый.

### 7.7. Transactional outbox

Значимые команды записывают action/event и outbox-сообщения одной транзакцией.
Dispatcher создаёт `notifications` идемпотентно. Нельзя сначала commit action, а
затем best-effort создавать уведомление в памяти.

Таблица outbox имеет status, attempts, available_at, locked_at/by, last_error и
unique idempotency key. Захват — `FOR UPDATE SKIP LOCKED` порциями.

## 8. Уведомления

CHECK и Go enum расширяются типами:

- `action_assigned`;
- `action_reassigned`;
- `action_due_changed`;
- `action_cancelled`;
- `action_completed`;
- `action_transfer_requested`;
- `action_transfer_approved`;
- `action_transfer_rejected`;
- `action_reminder`;
- `action_grace_started`;
- `action_overdue`;
- `action_assignment_invalid`.

`entity_type='call_action'`, `entity_uuid=action_uuid`. Клик открывает
`/app/actions/{uuid}`. Payload текста не является источником состояния.

Правила получателей:

- назначение: новый ответственный;
- переназначение: прежний и новый ответственные;
- изменение срока не самим ответственным: ответственный;
- отмена не самим ответственным: ответственный;
- завершение вышестоящим: ответственный;
- transfer request: допустимые лидеры/менеджеры без дублей;
- решение transfer: requester, прежний и новый assignee по смыслу события;
- overdue: ответственный и активные лидеры target department; если лидеров нет —
  company managers;
- assignment invalid: лидеры target и company managers.

Один пользователь получает одно уведомление на событие, даже если совпало несколько
ролей. Пользователи left/suspended не получают новые уведомления.

## 9. Workers

Workers могут жить в том же binary с отдельными config toggles, но являются
независимыми lifecycle-компонентами и не делят один бесконечный transaction.

### 9.1. Reminder worker

Раз в час обрабатывает пороговые события:

- `due_at - 7d`;
- `due_at - 5d`;
- `due_at - 2d`;
- `due_at - 1d`;
- `due_at` — начало grace;
- `due_at + 12h` — осталось 12 часов grace.

Для короткого срока worker отправляет только ещё актуальные будущие/наступившие
ступени; он не создаёт задним числом четыре уведомления сразу. Первая итерация после
старта использует окно `(last_successful_scan_at, now]`, а уникальный delivery key
защищает от повторов. Если downtime превысил период, отправляется максимум одно
наиболее актуальное reminder на action, но событие overdue не пропускается.

Обработка идёт keyset batches, например 500 строк, по индексам `due_at/action_uuid`.
Каждый batch имеет короткую транзакцию. Несколько replica безопасны за счёт unique
delivery и `SKIP LOCKED`/outbox; leader election не является единственной защитой.

### 9.2. Overdue worker

Это отдельный worker. Раз в час он запускает конечную итерацию и обходит **все**
кандидаты, у которых:

```sql
status IN ('open','in_progress')
AND assignment_state = 'valid'
AND grace_expires_at <= now()
```

Worker не загружает всю таблицу в память. Он выбирает строки индексированными
порциями с keyset pagination/`FOR UPDATE SKIP LOCKED`, переводит каждую ещё
подходящую запись в `overdue`, пишет event и outbox в одной транзакции и продолжает,
пока очередная выборка не станет пустой. После проверки всей таблицы итерация
завершается; следующий запуск — через час.

Перед update условие повторяется в SQL. Поэтому завершение/отмена параллельным
запросом выигрывает корректно и не может быть перезаписано в overdue. Два worker
instance не создают двойной transition благодаря conditional update, status и unique
event/delivery keys.

### 9.3. Эксплуатация workers

Отдельные env-настройки: enabled, interval (default 1h), batch size, statement
timeout, max iteration duration и instance ID. При превышении max duration worker
логирует incomplete scan и продолжает с durable cursor на следующем тике; готовность
не утверждается, пока метрика scan lag не вернулась в норму.

Метрики:

- scan duration/rows/batches/errors;
- oldest eligible reminder/overdue age;
- notifications created/deduplicated/failed;
- outbox lag and retry count;
- actions by status and overdue transition latency.

Shutdown прекращает взятие новых batches и ждёт текущую короткую транзакцию.

## 10. API

Все mutation endpoints принимают `Idempotency-Key`; update-команды также принимают
`expected_lock_version`. Ошибки имеют стабильные codes.

### 10.1. Компании и поиск участников

- существующий `POST /companies` становится идемпотентным;
- `GET /companies/{company_uuid}/action-assignees?q=&department_uuid=&limit=` —
  ACL-aware активные участники с массивом активных отделов.

### 10.2. Disposition и actions

```text
PUT    /calls/{call_uuid}/analyses/{analysis_uuid}/action-disposition
POST   /calls/{call_uuid}/analyses/{analysis_uuid}/actions
GET    /actions
GET    /actions/{action_uuid}
PATCH  /actions/{action_uuid}
POST   /actions/{action_uuid}/start
POST   /actions/{action_uuid}/complete
POST   /actions/{action_uuid}/cancel
POST   /actions/{action_uuid}/reopen
POST   /actions/{action_uuid}/reschedule
POST   /actions/{action_uuid}/reassign
GET    /actions/{action_uuid}/events
```

List filters: company, source/target department, assignee, author, status,
assignment_state, due range, query, `mine`, `involving_my_department`, cursor, limit.
Cursor pagination использует стабильный `(updated_at, action_uuid)`; limit 1..100.

### 10.3. Transfer

```text
POST /actions/{uuid}/transfer-requests
POST /actions/{uuid}/transfer-requests/{request_uuid}/approve
POST /actions/{uuid}/transfer-requests/{request_uuid}/reject
POST /actions/{uuid}/transfer-requests/{request_uuid}/withdraw
```

### 10.4. Admin

```text
GET  /admin/actions
GET  /admin/actions/{uuid}
POST /admin/actions/{uuid}/reassign
POST /admin/actions/{uuid}/reschedule
POST /admin/actions/{uuid}/cancel
POST /admin/actions/{uuid}/complete
POST /admin/actions/{uuid}/reopen
```

Требуются granular permissions `admin.actions.read/manage`. Admin endpoints не
обходят tenant predicate без явно выданного capability. Каждая mutation требует
reason и попадает в общий admin audit.

### 10.5. Ошибки

Минимум:

- `action_not_found` (также для скрытого ресурса);
- `action_forbidden`;
- `action_conflict` (409, актуальная version);
- `action_terminal`;
- `action_invalid_transition`;
- `action_invalid_company_member`;
- `action_invalid_department`;
- `action_assignee_department_ambiguous`;
- `action_evidence_revision_mismatch`;
- `action_evidence_range_invalid`;
- `action_transfer_pending`;
- `action_transfer_approval_forbidden`;
- `action_due_at_invalid`;
- `company_limit_reached`.

Validation 422, conflict 409, forbidden/not found согласно существующей политике
неразглашения.

## 11. Frontend

### 11.1. Навигация

Sidebar после изменения:

- основные рабочие разделы, включая «Действия» и «Компании»;
- без «Контактов» и «Настроек».

Profile popover:

- «Профиль»;
- «Контакты»;
- «Настройки»;
- separator;
- «Выйти» как destructive action.

Popover закрывается по outside click, `Esc` и после навигации, удерживает корректный
focus, имеет menu semantics и работает на mobile без выхода за viewport. Старые URL
`/app/contacts` и `/app/settings` сохраняются; меняется только точка входа.

### 11.2. Маршруты

```text
/app/companies
/app/companies/new
/app/companies/{uuid}
/app/actions
/app/actions/{uuid}
/app/admin/actions
/app/admin/actions/{uuid}
```

Legacy `/app/settings/companies...` получает внутренний redirect/compatibility до
миграции ссылок.

### 11.3. Список действий

Вкладки/фильтры:

- «Мои»;
- «Созданные мной»;
- «Мои отделы»;
- «Межотдельские»;
- «Запросы передачи»;
- «Просроченные»;
- «Завершённые».

Карточка/строка показывает title, status, assignee, source → target department,
due/grace, call и непрочитанные изменения. Desktop допускает table, mobile — cards.
Никакой цвет не является единственным носителем статуса.

### 11.4. Страница действия

- header: title, status, company, departments;
- assignee, due/grace и capability-aware commands;
- описание;
- evidence cards с quote, speaker, time и переходом к media при наличии доступа;
- источник: call + analysis/revision;
- transfer request panel;
- comments/решение при закрытии;
- chronological audit timeline;
- loading, empty, stale, 403/404, conflict и retry states.

После 409 форма не перезаписывает сервер: показывает изменившиеся поля и предлагает
перезагрузить. Optimistic UI не меняет terminal state до ответа backend.

### 11.5. Темы и визуальный стандарт

Новые компоненты используют существующие semantic tokens (`--app-*`, существующие
surface/text/border/accent variables), а не hard-coded цвета. Если токена не хватает,
он добавляется парой для light/dark и используется повторно.

Обязательно проверить:

- normal/hover/focus/disabled/error/success/warning/overdue;
- контраст текста, badges, borders и focus ring;
- popover, drawer, modal backdrop и scrollbars;
- evidence selected/active одновременно;
- длинные русские названия, username, отделы и timezone;
- 320, 768, 1024 и ≥1440 px;
- keyboard-only и reduced motion;
- отсутствие layout shift и горизонтального overflow.

«Ровно» означает совпадение spacing, radius, typography, icon size, controls и card
hierarchy с соседними страницами, а не создание отдельного визуального языка.

## 12. Безопасность и надёжность

- SQL tenant predicates до LIMIT/OFFSET;
- UUID parsing и payload/body limits;
- rate limit для member search и mutations;
- reason/comment sanitization при выводе, без HTML execution;
- media URL только через существующий ACL и short-lived access;
- evidence snapshot не расширяет права на transcript;
- CSRF/session политика соответствует текущему auth transport;
- request ID/correlation ID проходит action → event → outbox → notification;
- PII не пишется целиком в logs;
- DB constraints защищают инварианты независимо от UI;
- transaction isolation и row locks покрывают competing complete/cancel/reassign;
- migrations backward compatible: сначала schema/types, затем код, затем cleanup;
- worker failure не блокирует HTTP API;
- retry имеет exponential backoff и dead-letter/операторскую видимость;
- backup/restore включает новые таблицы; rollback migration не удаляет production
  data без отдельного решения.

## 13. Производительность

- никакого hourly `SELECT *` всей таблицы в память;
- partial indexes только по активным состояниям;
- keyset pagination для больших списков и worker scans;
- batch size конфигурируемый, default 500;
- N+1 запрещён: assignee/departments загружаются join/batched lookup;
- transcript search локальный по уже загруженным words; для очень больших transcript
  нормализованный поисковый индекс строится один раз через `useMemo`/worker thread;
- autocomplete debounced 250–350 ms и отменяет устаревшие requests;
- audit payload bounded, без копирования полной транскрипции;
- list endpoint возвращает summary DTO; evidence/events грузятся detail endpoint;
- explain-анализ фиксирует использование action due/status indexes на объёме,
  превышающем ожидаемый production минимум в 10 раз.

Цели: p95 list API <300 ms при тёплой БД, detail <300 ms без media, worker transition
lag не более 65 минут при штатной нагрузке. Это acceptance SLO, а не обещание без
нагрузочного теста.

## 14. Наблюдаемость

Structured logs: action UUID, company UUID, operation, actor UUID, result, latency,
lock conflict, worker batch; без title/description/quote.

Metrics:

- commands по operation/result;
- action counts по status и company (company только в logs/traces, не high-cardinality
  metric label);
- conflicts и forbidden;
- reminder/overdue scan lag;
- outbox depth/age/retries/dead letters;
- notification deduplication;
- frontend route errors и failed mutations.

Alerts: overdue lag >75 min, outbox oldest >15 min, repeated worker iteration errors,
dead-letter growth, action API error-rate. Health endpoint различает liveness и
degraded background processing.

## 15. Тестирование

### 15.1. Backend unit

- все status transitions и запрещённые переходы;
- RBAC каждой строки матрицы, включая пользователя с несколькими ролями;
- лидер постороннего отдела не читает/не изменяет действие;
- company manager scope;
- admin без capability и с capability;
- assignee 0/1/N departments;
- evidence validation и revision mismatch;
- transfer approve/reject/withdraw races;
- change due/reassign/cancel notifications;
- UTC/grace boundary, DST пользовательского отображения не влияет на backend;
- idempotency и optimistic lock.

### 15.2. Repository/integration PostgreSQL

- migrations up/down на совместимом snapshot;
- constraints и partial unique indexes;
- list ACL predicate до pagination;
- concurrent complete vs overdue, cancel vs reminder, two approvals;
- два worker instances не дублируют события/notifications;
- worker обходит больше одного batch и завершает только после пустой выборки;
- downtime/catch-up;
- reschedule создаёт новую schedule version;
- membership suspension между select и update;
- EXPLAIN для list/reminder/overdue queries.

Database-sharing integration tests запускаются serially.

### 15.3. API contract

- success DTO и стабильные error codes;
- hidden resource не раскрывается;
- malformed UUID/body/oversized text;
- pagination/cursor/filter combinations;
- Idempotency-Key replay с тем же и отличающимся payload;
- 409 содержит актуальную lock version без лишних данных;
- notification deep link.

### 15.4. Frontend unit/component

- routing и legacy paths;
- company empty state → separate creation page;
- profile menu и удалённые sidebar entries;
- assignee search, @username и ambiguous departments;
- transcript Ctrl+F navigation/selection;
- form validation/double submit;
- capability-driven controls без подмены backend security;
- transfer request states;
- due/grace/overdue labels;
- conflict refresh;
- notification click.

### 15.5. Browser E2E

Минимальные реальные сценарии:

1. Пользователь без управляемой компании создаёт её на отдельной странице.
2. Создаёт действие из company/department call, выбирает matched quote, assignee и
   срок; reload подтверждает persisted payload.
3. Фиксирует `no_action_required` и отменяет решение.
4. Assignee просит передачу; лидер target department одобряет; посторонний лидер
   получает 404/forbidden и не видит задачу в списке.
5. Manager меняет срок/assignee; прежний и новый исполнители видят уведомления.
6. Ответственный отменяет с причиной; запись остаётся в audit.
7. Clock-controlled тест проходит due, +12h и +24h; notifications уникальны,
   вышестоящий может complete в grace, иначе worker ставит overdue.
8. Admin capabilities читают/изменяют, admin без capability не может.
9. Evidence click перематывает реальный media к сохранённому timestamp.
10. Profile popover ведёт в профиль/контакты/настройки и корректно выходит.

Каждый сценарий выполняется в light и dark theme на desktop; критические создание,
detail, profile menu и transcript picker — дополнительно на mobile viewport.
Скриншоты сравниваются с соседними экранами, но snapshot не заменяет ручную visual
QA геометрии, контраста и переполнения.

### 15.6. Общие gates

Backend: format, unit, serial integration, race-relevant tests, lint, vet, migration,
Docker build, `git diff --check`. Frontend: typecheck/build, lint при наличии, unit,
E2E, `git diff --check`, UTF-8/U+FFFD scan. Проверяется реальный API payload и БД,
а не только успешная компиляция.

## 16. Порядок реализации и rollout

1. Миграции actions/events/evidence/transfer/delivery/outbox и notification types.
2. Domain service, repository, ACL predicates, idempotency, unit/integration tests.
3. API и admin capabilities.
4. Outbox dispatcher, reminder worker, отдельный overdue worker, metrics/alerts.
5. Frontend types/API/routes и profile/sidebar navigation.
6. Company creation page.
7. Action create drawer + transcript picker.
8. User list/detail/transfer flows.
9. Admin list/detail.
10. Light/dark/responsive/accessibility pass.
11. E2E с test clock и controlled worker execution.
12. Shadow rollout workers: сначала scan-only metrics, затем notifications для test
    company, затем staged companies, затем общий rollout.

Feature flags разделяют action UI, reminders и overdue transitions. Отключение UI
не удаляет данные и не останавливает обязательную обработку уже созданных actions.

## 17. Definition of Done

Работа считается готовой только если одновременно выполнено всё:

- реализованы все пункты 3.1 и нет заглушек/неработающих кнопок;
- backend enforce’ит модель, transitions, ACL и membership cases;
- создание компании доступно отдельной страницей по условию управляемой компании;
- контакты/настройки перенесены в profile menu без поломки deep links;
- действие создаётся с реальным persisted snapshot и выбранным evidence;
- `no_action_required` version-bound и не скрывает новый анализ;
- leader видит только source/target departments;
- transfer требует согласования и корректно уведомляет стороны;
- изменения/отмена/закрытие вышестоящим уведомляют ответственного;
- reminder worker выдаёт уникальные 7/5/2/1d, due и +12h уведомления;
- отдельный hourly overdue worker проверяет все eligible rows batches и ставит
  overdue после +24h без гонок и дублей;
- audit append-only и покрывает каждую mutation/worker transition;
- admin работает только через capabilities и пишет reason/audit;
- backend/frontend automated gates зелёные;
- authenticated browser E2E подтверждает БД/API/rendered behavior;
- light и dark theme вручную проверены на desktop/mobile, включая все состояния;
- observability dashboards/alerts и runbook готовы;
- migrations и rollout/rollback отрепетированы на копии production-like данных;
- Graphify обновлён успешно либо честно зафиксирована невозможность обновления.

Компиляция, отдельный happy-path или визуально похожий mock не являются 100%
реализацией. Готовность утверждается только по доказательствам каждого пункта этого
Definition of Done.
