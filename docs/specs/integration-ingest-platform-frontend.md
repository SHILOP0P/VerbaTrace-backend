# Frontend-спецификация: Integration Ingest Platform

Статус: production design, код не реализован
Дата: 2026-08-22
Родитель: [integration-ingest-platform.md](./integration-ingest-platform.md)
Billing и visual gates:
[credit-billing-developer-platform-rollout.md](./credit-billing-developer-platform-rollout.md)

## 1. Цель интерфейса

Frontend даёт менеджеру компании безопасно подключить источник, один раз получить
API key, проверить связь, наблюдать импорт и исправлять устранимые ошибки. UI не
угадывает backend status, ACL, retryability или успешность CRM.

Пользователь должен за минуту понять:

- работает ли источник сейчас;
- когда последний звонок успешно поступил;
- есть ли очередь/ошибка и что можно сделать;
- куда помещаются звонки и какие инструкции применяются;
- какой звонок создан из конкретного внешнего события.

## 2. Information architecture

Маршруты:

```text
/app/settings/integrations
/app/settings/integrations/new
/app/settings/integrations/{connection_uuid}
/app/settings/integrations/{connection_uuid}/imports
/app/settings/integrations/{connection_uuid}/audit
```

Раздел виден в настройках компании. `company_manager` получает управление;
read-only роли — только если backend capability это разрешает. Проверка роли в UI
управляет представлением, но не считается защитой.

Deep links сохраняют tab/filter/cursor в URL. Back/forward не теряет выбранный
workspace. Несуществующий или недоступный connection показывает корректный
non-disclosing state, а не бесконечный loader.

## 3. Страница списка

Карточка/строка connection:

- name и provider;
- `active/degraded/disabled/revoked/draft` с текстом, не только цветом;
- destination company/department/folder;
- последнее принятое событие и последний успешный звонок;
- queue/failed count;
- last safe error;
- меню действий согласно capabilities.

Есть search/provider/status filters, cursor pagination, refresh и empty state.
Порядок стабилен: degraded с проблемами, active, disabled/draft/revoked; внутри —
updated time. Если backend отдаёт иной canonical order, UI его не пересортировывает
скрыто.

Состояния отдельно проектируются: initial loading, background refresh, empty,
permission denied, partial data, retryable API failure и stale cached view.

## 4. Создание connection

Wizard не нужен, если шагов меньше четырёх; предпочтительна одна ясная страница с
секциями и итоговым preview.

Поля:

1. provider (`Generic API`, позднее `Bitrix24`);
2. name;
3. company (фиксирована текущим workspace);
4. department и optional folder;
5. instruction selection с effective preview;
6. optional outgoing webhook URL;
7. test connection / create.

Department/folder/instructions загружаются из реальных API. При смене department
несовместимый folder сбрасывается с объяснением. Backend validation errors
привязываются к полям по stable code; неизвестная ошибка остаётся общей.

Double submit предотвращается UI и idempotency key. После timeout повтор использует
тот же key и получает прежний connection. Успех ведёт через `replace` на detail.

## 5. API key UX

Создание ключа — отдельная подтверждаемая команда. Форма: name, scopes, expiry.
UI не предлагает scope, которого нет в backend capabilities.

После создания:

- полный key показывается ровно один раз в modal/drawer;
- copy button имеет явный success feedback и доступен с клавиатуры;
- закрытие требует checkbox «Я сохранил ключ» либо дополнительного подтверждения;
- screenshot/clipboard risk объясняется без показа ключа в toast/log/URL;
- после закрытия видны prefix, scopes, created/last used/expires/revoked;
- повторно получить secret нельзя — только создать/rotate.

Rotation показывает новый ключ и точный срок overlap старого. Revoke требует
подтверждения с названием/prefix. Optimistic UI для revoke запрещён: состояние
меняется после server confirmation.

В browser persistence, analytics, error monitoring и devtools-friendly state
plaintext key не сохраняется. React state очищается при закрытии/unmount.

One-time response имеет `Cache-Control: no-store, private`, не попадает в service
worker/HTTP cache и скрывается при `visibilitychange`, workspace switch и logout.
UI не обещает очистить OS clipboard или предотвратить screenshot — только
предупреждает. DOM secret не рендерится через HTML injection и удаляется после
закрытия; тест охватывает history, cache, breadcrumbs и crash reports.

## 6. Connection detail

Верхняя часть:

- provider/name/status;
- health summary;
- last event/success;
- destination и effective instructions;
- actions: test, edit, disable/enable, reconnect, revoke.

Tabs:

- **Обзор** — health, setup snippet, endpoint, recent activity;
- **Импорты** — operational timeline;
- **Ключи и webhooks** — secrets metadata/deliveries;
- **Аудит** — append-only events.

Status polling использует backoff и останавливается на hidden tab/unmount. Если
доступен SSE, reconnection имеет cursor/Last-Event-ID и fallback polling. Нельзя
открывать отдельный polling timer на каждую строку списка.

## 7. Generic API setup

Показываются:

- endpoint;
- auth header placeholder, не настоящий key после one-time modal;
- минимальный JSON example;
- curl example с `${VERBATRACE_API_KEY}`;
- ссылка на versioned API contract;
- кнопка «Отправить тестовое событие» через безопасный backend test command, а не
  прямой вызов внешнего endpoint из browser.

Пример не содержит реальных UUID/ссылок/PII. Copy feedback доступен screen reader.

## 8. Bitrix24 onboarding

До backend capability `bitrix24_verified=true` карточка помечена «Экспериментально»
или недоступна — не «Готово».

Flow:

1. объяснить, что нужен портал Bitrix24 и REST access;
2. показать, что бесплатный портал существует, но REST может потребовать demo,
   trial Marketplace, NFR или платный доступ;
3. выбрать company/destination до OAuth;
4. открыть server-generated OAuth URL в текущем окне или popup с безопасным
   fallback;
5. state callback обрабатывает backend; frontend не получает access/refresh token;
6. показать portal identity, permissions и test result;
7. missing permissions/reconnect — отдельный actionable state.

Popup close/blocked, callback error, state expired, user cancellation и connection
created-but-OAuth-failed имеют восстановимый сценарий. Повтор OAuth не создаёт
второй connection.

Для amoCRM UI не обещается до отдельного verified adapter. Можно показать roadmap,
но кнопка «Подключить» запрещена без работающего backend contract.

Popup callback принимает `postMessage` только от exact expected origin, проверяет
одноразовый flow ID и не передаёт code/token frontend. Внешнее окно открывается с
`noopener`; backend не принимает произвольный `return_to`. Full-page fallback
восстанавливает только несекретный flow state. Закрытие popup не считается успехом:
итог подтверждается повторным GET backend state.

## 9. Imports timeline

Таблица desktop и карточки mobile содержат:

- external call ID в безопасно усечённом виде;
- title, source, received/occurred time;
- stage/status;
- attempts и next retry;
- created call deep link;
- safe error code/message;
- permitted retry/cancel actions.

Фильтры: status/stage/date/error; search external ID/title только если backend API
это поддерживает. Cursor хранится в URL. Auto-refresh сохраняет scroll/selection и
не мигает всей таблицей.

Status timeline отображает подтверждённые server timestamps:

`Принято → Получение записи → Проверка → Создание звонка → Транскрипция → Анализ →
Готово`.

Будущий этап не рисуется завершённым. `processing` call использует ссылку на
существующую страницу звонка. `webhook_failed` показывается отдельно и не меняет
готовый analysis на failed.

## 10. Ошибки и действия

UI использует `error.code` и `retryable`, не парсит текст. Категории:

- source unavailable/rate limited — автоматический retry и время;
- auth expired — reconnect;
- forbidden URL/media — исправить источник, retry недоступен;
- billing limit — перейти к тарифу/обратиться к manager;
- destination removed — изменить connection;
- unsupported media/too large — объяснить limit;
- internal invariant — request ID и обращение в поддержку, без технических деталей.

Manual retry требует подтверждения, если может повторно скачать большой файл.
Кнопка блокируется только на время команды; повтор с тем же idempotency key
безопасен. Attempts не обнуляются визуально.

## 11. Редактирование и lifecycle

- смена destination/instructions влияет только на новые ingest items;
- pending items сохраняют settings snapshot либо явно следуют за versioned policy,
  возвращаемой backend; UI показывает выбранную политику;
- disable прекращает новый приём/claim согласно backend semantics, не удаляет calls;
- revoke необратимо отзывает keys/tokens и требует typed confirmation;
- destructive wording точно перечисляет, что будет и не будет удалено;
- concurrent edit возвращает 409/412 и предлагает reload/compare, не перезаписывает;
- unsaved form guard работает при navigation, refresh и workspace switch.

## 12. Audit и webhook deliveries

Audit показывает actor, действие, entity и time. Secret/payload/URL query не
рендерятся. Event details — allowlisted renderer по event type; неизвестный event
показывается безопасно и не ломает страницу.

Webhook tab: endpoint host/path без query secrets, status, attempts, next retry,
HTTP status и event ID. Response body не показывается. Test webhook создаёт
реальную backend delivery с маркировкой `test` и показывает подтверждённый итог.

## 13. Call provenance

На странице звонка отдельный блок:

- provider и connection name;
- external call ID;
- received/occurred timestamps;
- optional validated external deep link;
- link на ingest item для manager.

Frontend не строит external URL конкатенацией недоверенных metadata. Используется
готовый validated URL backend либо ссылка отсутствует. Provenance не расширяет
доступ к integration settings.

External link разрешает только `https`, открывается с `noopener,noreferrer` и явно
показывает внешний переход. Title, metadata, safe error и audit values рендерятся
как text, не HTML/Markdown; allowlisted rich content проходит единый sanitizer с
CSP-compatible policy. Spreadsheet formula injection учитывается при CSV export.

## 14. State management и API client

- типы генерируются/сверяются с OpenAPI;
- query keys включают company/connection/filter/cursor;
- mutations инвалидируют минимальные связанные queries;
- AbortController отменяет stale requests;
- polling/SSE не вызывает state update после unmount;
- idempotency key создаётся один раз на пользовательскую команду, сохраняется до
  terminal HTTP result/reconciliation;
- server timestamps/status/capabilities не вычисляются локально;
- sensitive response one-time key исключается из global cache/dev persistence.
- 401/403 или смена workspace очищают sensitive/local cache и закрывают SSE;
  permission downgrade не оставляет privileged controls из старого response;
- CSRF policy соответствует auth transport: cookie-команды требуют SameSite/CSRF
  token и Origin check, bearer token не хранится в localStorage;
- server-provided URLs не используются для navigation без route/origin allowlist.

## 15. Accessibility, responsive и visual language

- существующие компоненты/tokens VerbaTrace, без параллельной дизайн-системы;
- полная keyboard navigation и заметный focus;
- modal focus trap/restore, `Esc` с защитой несохранённого secret;
- status не только цветом, contrast WCAG AA;
- aria-live для copy/test/retry результата без чтения ключа вслух по умолчанию;
- tables превращаются в читаемые mobile cards;
- long IDs/errors не ломают layout, имеют безопасное копирование;
- light/dark и `prefers-reduced-motion`;
- loading skeleton сохраняет geometry; no layout jumps.

### 15.1. Геометрия и визуальная симметрия

- проверять light/dark на 360, 390, 768, 1024, 1280 и 1440 px;
- проверять normal/loading/empty/error/permission/long-content состояния;
- bounding rectangles соседних элементов не пересекаются, если overlap не является
  явной частью дизайна;
- document не имеет горизонтального overflow; текст не clipping и не прижат к
  border/icon/control ближе минимального design token 8 px;
- одинаковые карточки/колонки имеют одинаковые padding, gap, высоту header и
  baseline alignment; touch targets не меньше 44x44 px;
- focus/validation/help text не меняют layout непредсказуемо и не перекрывают
  соседние элементы;
- long Unicode, большие числа, zoom 200% и browser text scaling не ломают экран;
- screenshot regression дополняется ручной оптической проверкой; baseline нельзя
  обновлять без объяснённого visual change.

### 15.2. Темы

Все состояния компонентов используют semantic theme tokens. Для light и dark
отдельно проверяются surface/text/muted/border/accent/danger/focus, chart heatmap,
skeleton, hover, focus, active, disabled и error. Контраст соответствует WCAG AA;
status не кодируется только цветом.

## 16. Производительность и bottlenecks

- cursor pagination и virtualisation только после измерения;
- одна агрегированная health query вместо N+1;
- debounced server search, request cancellation;
- не загружать audit/import history до открытия tab;
- bounded recent items на overview;
- SSE events batch/coalesce, background tab throttling;
- connection details/code examples lazy-load;
- large raw payload никогда не передаётся browser;
- rate-limit responses показывают retry time и не создают request storm.

## 17. Analytics без утечки данных

Разрешены события: page viewed, connection create started/completed, test outcome по
bounded error category, key rotated, retry requested. Запрещены API key, OAuth
token, external IDs, titles, media URL, payload, company/user UUID в third-party
analytics без отдельного privacy contract.

Session replay выключен на integration/key/OAuth экранах; DOM masking не считается
гарантией для one-time secret. Analytics payload проходит runtime allowlist и
automated test, а не только соглашение по именам событий.

## 18. Тестовая стратегия

### Component/unit

- all status/error renderers;
- one-time secret never persists;
- capabilities/ACL presentation;
- cursor/filter URL state;
- idempotency reuse after timeout;
- timezones, long Unicode, empty/partial data.

### Integration/API mock

- create/edit/disable/revoke/rotate flows;
- optimistic conflict;
- 401/403/404/409/412/413/422/429/5xx;
- polling/SSE reconnect and cleanup;
- stale response ordering;
- webhook test/delivery history.

### Browser E2E against real backend

- create generic connection and key;
- copy/close proves secret unavailable again;
- emulator ingests one call, duplicate remains one call;
- failed item → retry → call;
- created call provenance and processing transition;
- revoke key blocks ingest;
- role matrix company manager/leader/employee;
- keyboard-only, mobile viewport, light/dark;
- screenshot matrix, geometry/no-overlap, spacing/symmetry и zoom 200%;
- refresh/deep link/back navigation.

### External connector

Bitrix24 OAuth/event/media test проводится отдельно на test portal. Screenshot UI
без реального backend/event не является проверкой интеграции.

## 19. Готовность frontend

- API types и stable errors согласованы с backend;
- production build/lint/tests проходят;
- browser E2E проходит на реальной БД/worker/emulator;
- visual QA обеих тем и mobile выполнена;
- критические экраны прошли screenshot diff, DOM geometry assertions и ручную
  проверку отсутствия наложений, текста впритык и асимметрии;
- no secret проверен через storage/network logs/error monitoring;
- partial backend capability не показывается как готовый connector;
- README/operator docs содержат setup и ограничения.

Frontend нельзя считать готовым только по наличию страниц или успешной сборке.
