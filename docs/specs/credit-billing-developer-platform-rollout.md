# Поэтапная спецификация: кредитный биллинг, developer platform и интеграции

Статус: production design, реализация не начата
Дата: 2026-08-22
Backend: `C:\projects\VerbaTrace\Monolit`
Frontend: `C:\projects\VerbaTrace-frontend`

Связанные документы:

- [Integration platform](./integration-ingest-platform.md)
- [Backend integration contract](./integration-ingest-platform-backend.md)
- [Frontend integration contract](./integration-ingest-platform-frontend.md)

## 1. Цель и порядок поставки

Поставка выполняется только в таком порядке:

`измерение себестоимости -> pricing catalog -> ledger -> allowance и wallet ->
reserve/settle -> UI лимита и активности -> mock purchase -> admin reset ->
developer applications/keys -> sandbox mock/real -> Generic API -> Bitrix24 pilot`.

Следующий этап нельзя включать для production-трафика, пока предыдущий не прошёл
свои data, security, operational и rollback gates. Backend-ready, frontend-ready,
billing-verified, sandbox-verified, connector-verified и production-ready являются
разными статусами.

## 2. Неподвижные продуктовые решения

1. Пользовательская единица называется **кредит VerbaTrace**. Кредит не является
   валютой, не обменивается обратно на деньги и не обещает постоянного соответствия
   минутам или provider tokens.
2. Расчётный номинал: `1 credit = 10 microUSD retail reference`, то есть
   `100 000 credits = $1` расчётной розничной стоимости. Денежная цена пакета в
   валюте продажи хранится отдельно и версионируется.
3. Единый pricing multiplier первой версии — `3.5`, в финансовой арифметике
   представленный точной rational pair `7/2`. Его изменение возможно только новой
   versioned pricing policy после cost telemetry, но не является частью первой
   версии.
5. Операция имеет одну credit rate независимо от источника средств. Скидки задаются
   ценой пакета или allowance, а не другим списанием за ту же операцию.
6. Подписка предоставляет периодический allowance в кредитах. До исчерпания UI
   показывает процент остатка и дни до сброса, но не количество allowance credits.
7. После исчерпания allowance точные credits показываются только при наличии
   доступного wallet balance. Кошелёк компании видит только `company_manager`.
8. Сначала расходуется allowance, затем expiring promo, затем purchased credits.
9. Платёжный провайдер пока mock, но ledger и webhook semantics должны быть
   совместимы с последующим реальным эквайрингом.
10. Superadmin выполняет точечный или массовый reset только allowance подписки без
    удаления истории. Purchased/promotional wallet balance такой командой не
    изменяется. Массовая операция требует dry-run и durable batch.
11. API key secret показывается один раз. После этого его не может прочитать ни
    другой сотрудник, ни manager, ни superadmin.
12. Sandbox mock расходует только sandbox credits и не вызывает платный AI.
    Sandbox real хранит данные в sandbox, но списывает реальные credits за выбранную
    модель; тариф подписки не ограничивает модель.

## 3. Формулы и финансовые инварианты

В расчётах запрещён `float`. Provider cost хранится в `nanoUSD bigint`, credits и
minor currency units — `bigint`. Округление вверх производится один раз на итог
операции.

Коэффициент хранится как сокращённая rational pair `(7, 2)` и вычисляется checked
integer arithmetic с явной единицей поля. Decimal/float `3.5` в финансовом коде не
используется. Conversion `nanoUSD -> credits` имеет golden tests на unit scale;
overflow/negative usage закрывает операцию в `reconciling`.

```text
pricing_multiplier = 7 / 2
credits = ceil(actual_provider_cost_microUSD * 7 / (10 * 2))
```

Cost telemetry дополнительно считает gateway fee, retry loss, CPU, storage и
traffic для контроля фактической маржи. Эти компоненты не меняют завершённую
операцию и не подключают другой коэффициент автоматически: изменение формулы
требует новой pricing policy, dry-run и rollout.

Начальные reference rates текущих провайдеров:

| Операция | Provider cost | Начальное списание |
|---|---:|---:|
| Universal-2 Standard | $0.002500/мин | 875 credits/мин |
| Universal-2 + diarization | ~$0.002833/мин | 992 credits/мин |
| Universal-2 + diarization + identification | ~$0.003167/мин | 1 109 credits/мин |
| GPT-5 Mini, 10k input + 2k output без cache | $0.0065 | 2 275 credits |

Таблица является initial catalog snapshot, а не runtime-константой. Для LLM
источник финансовой истины — фактический provider/gateway cost. Prompt, cached,
completion и reasoning usage сохраняются для объяснения, но reasoning не
начисляется второй раз, если он уже включён в provider cost.

Pricing version и полный snapshot закрепляются за `usage_operation` до reserve.
Изменение каталога не пересчитывает завершённые операции.

## 4. Этап 0 — cost telemetry и shadow metering

### Backend

- сохранять provider request/generation ID, модель, ASR duration/mode;
- расширить OpenRouter usage: prompt, cached, completion, reasoning, actual cost;
- записывать успешные, неуспешные и повторные provider attempts;
- отделить customer-billable cost от internal loss;
- учитывать gateway fee, storage, CPU/media processing и traffic в доступных
  измеряемых единицах;
- ежедневный reconciliation с provider usage/invoice;
- dashboard P50/P90/P99: cost/call, cost/minute, cost/analysis, retry loss,
  cache benefit и расхождение ledger/provider.

### Gate

- shadow path не изменяет существующий minute enforcement;
- 100% платных provider calls получают usage operation или reconciliation alert;
- duplicate/retry не смешиваются с уникальными customer operations;
- проверены отсутствие PII в cost telemetry и rollback feature flag.

## 5. Этап 1 — versioned pricing catalog

Таблицы: `pricing_catalog_versions`, `pricing_rates`, `pricing_policy_versions`.
Rate идентифицируется operation/provider/model/mode/effective interval. Каталог
immutable после activation. Исправление создаёт новую версию.

Activation: draft -> validated -> scheduled -> active -> retired. Одновременно
активна ровно одна совместимая версия. Activation требует dry-run на последних
операциях, расчёта ожидаемого revenue/margin и audit actor/reason.

### Gate

- golden tests на приведённые выше расчёты и границы округления;
- property tests на overflow/negative/zero/extreme usage;
- replay старой операции использует старый snapshot;
- provider price change не меняет исторический ledger.

## 6. Этап 2 — billing account, grants и append-only ledger

Финансовый owner: personal user, company, позднее partner/reseller. Company wallet
управляется только `company_manager`.

Минимальные сущности:

```text
billing_accounts
credit_grants
credit_ledger_accounts
credit_ledger_transactions
credit_ledger_postings
usage_operations
billing_reconciliation_runs
```

Grant types: `subscription_allowance`, `promotional`, `purchased`, `sandbox`.
Ledger events: `grant`, `reserve`, `settle`, `release`, `refund`, `expire`,
`adjustment`, `allowance_opened`, `allowance_reset`, `allowance_closed`.

Ledger является double-entry и append-only. Каждая финансовая транзакция содержит
не менее двух postings с одинаковой `transaction_uuid`; сумма signed amounts по
одной credit commodity равна нулю. Исправление — новая reversing transaction с
`reversal_of_transaction_uuid`, исходные postings не изменяются. Прямой
`UPDATE balance` и каскадное удаление финансовой истории запрещены. Balance cache
является производным и восстанавливается replay/reconciliation.

Минимальный chart of accounts:

```text
customer_available:{grant_uuid}   liability, доступно клиенту
customer_reserved:{operation_uuid} liability, зарезервировано
customer_consumed:{billing_account_uuid} revenue clearing
verbatrace_promotional_pool        contra/reward funding
verbatrace_internal_loss           expense clearing
```

Операции проводятся сбалансированными транзакциями:

```text
grant/purchase: funding/source -> customer_available
reserve:        customer_available -> customer_reserved
settle:         customer_reserved -> customer_consumed
release:        customer_reserved -> customer_available
expire:         customer_available -> expiry/source
refund:         customer_consumed/source -> исходный customer bucket
pricing loss:   provider cost reconciliation -> verbatrace_internal_loss
```

Credit ledger не заменяет денежную бухгалтерию: fiat payment/refund имеет свой
double-entry money ledger в minor units и связывается с credit transaction через
order/payment reference. Credits разных commodity/environment не балансируются
между собой и не переводятся неявно.

Posting содержит ledger account, grant bucket, operation, pricing/currency version,
signed amount, causal event и monotonic account sequence. Transaction содержит
idempotency key, actor/reason и timestamps. Инвариант conservation проверяет
grant/reserve/settle/release/refund/expire/reversal. Grant consumption фиксирует
allocation по buckets: refund возвращает исходный bucket с исходным expiry, а не
создаёт вечный balance. Refund/chargeback после расходования может создать bounded
payment dispute, но обычный credit wallet не уходит в debt: до отдельной postpaid
версии новые операции блокируются, а финансовое расхождение учитывается вне
available balance.

Owner transfer, merge/split company, удаление последнего manager, suspension и
закрытие account не переносят credits автоматически. Нужен отдельный audited
workflow и решение о transferability каждого grant type.

### Gate

- конкурентные debit/reserve не дают отрицательный баланс;
- уникальные operation/idempotency/provider-event constraints;
- ledger replay совпадает с materialized balance;
- ACL/non-disclosure tests для employee/manager/superadmin;
- backup/restore и reconciliation проверены на disposable production-like DB.

## 7. Этап 3 — allowance epochs и миграция подписок

В первой версии allowance сбрасывается один раз в календарный месяц UTC. Каждый
месяц — immutable `allowance_epoch` с policy version,
`starts_at`, `ends_at`, total/reserved/settled и status. Schema поддерживает
другие будущие policy, но runtime первой версии принимает только
`calendar_month_utc`.

Миграция: shadow -> dual-read -> dual-write -> credit enforcement для внутренней
company -> limited pilot -> general rollout. Старые minute counters сохраняются как
история. Rollback возвращает enforcement, но не удаляет новый ledger.

API основного индикатора возвращает `remaining_percent`, `resets_at`,
`days_until_reset`, state/capabilities. До исчерпания allowance точное количество
period credits в employee response отсутствует.

`resets_at` — начало следующего календарного месяца UTC. `days_until_reset`
вычисляет backend как `max(0, ceil((resets_at - server_now_utc) / 24h))`; frontend
не пересчитывает это значение самостоятельно. После открытия новой epoch поле
сразу указывает следующий месяц.

### Gate

- timezone/boundary/leap-month/DST tests;
- plan change, cancellation, renewal, company membership change;
- одинаковый результат при retry renewal/reset job;
- dual-read расхождения ниже утверждённого порога в течение observation window.

Boundary вычисляется из закреплённой policy/timezone/tzdata version и server clock;
job опирается на DB uniqueness, а не cron timing. Late renewal, retroactive plan
change, grace, payment failure и одновременные reset/funding имеют deterministic
precedence. Открытая epoch не меняется после обновления tzdata или catalog.

## 8. Этап 4 — reserve, settle, release и failure semantics

```text
estimate -> atomic reserve -> provider call -> actual usage -> settle -> release
```

ASR reserve строится по ffprobe duration/mode. LLM reserve — по input estimate,
max output и pricing snapshot. Один `usage_operation_uuid` имеет не более одного
customer settlement.

До provider call backend рассчитывает `maximum_charge`, закрепляет pricing/model/
ASR mode, ограниченный `max_output_tokens` и резервирует всю сумму одной атомарной
транзакцией. Если полного резерва нет, операция не запускается. Дополнительный
reserve во время выполняющейся операции в первой версии запрещён.

- `actual_charge <= maximum_charge`: settle фактической суммы, остаток release в
  той же saga;
- `actual_charge > maximum_charge`: с клиента settle только `maximum_charge`,
  разница проводится как `internal_pricing_loss` VerbaTrace и создаёт alert;
- available/reserved balance обычного кошелька никогда не становится отрицательным;
- postpaid/debt разрешается только отдельным договорным клиентам и не входит в
  первую версию;
- provider result сохраняется даже при pricing anomaly; операция получает
  reconciliation marker.

Локальный timeout не доказывает отсутствие provider charge.

- ошибка до платного вызова: 0 credits;
- timeout/5xx с provider charge без результата: internal loss, customer 0;
- успешный ASR и неуспешный analysis: списывается только ASR;
- нехватка на analysis: transcript сохраняется, analysis становится
  `blocked_insufficient_credits` и продолжается после funding без нового ASR;
- cancel policy возвращается API заранее и фиксируется в operation snapshot;
- неизвестный provider outcome остаётся `reconciling`, а не угадывается.

### Gate

Fault injection в каждой точке до/после reserve, provider call, durable result,
settle и outbox commit; parallel duplicates; worker crash/reclaim; provider
reconciliation; доказательство exactly-once customer debit.

## 9. Этап 5 — billing frontend и activity

### Основной индикатор

До исчерпания: `Осталось 37%` и `До сброса лимита: 9 дней`. После исчерпания:
статус, дни до reset и, только если разрешено и wallet существует, точный wallet
balance. Employee не получает company wallet amount даже через API.

### Wallet

Личный owner/manager видит available/reserved, purchased и promo buckets, expiry,
ledger и mock purchases. Купленные credits не дублируются в основном индикаторе до
исчерпания allowance.

### Activity

Heatmap: день/неделя/период, account timezone, будущий день отличается от нулевого
usage. Tooltip/табличная альтернатива: total credits, transcription, analysis,
deep analysis, calls и reset markers. Прогноз исчерпания выпускается отдельным
feature flag только после достаточной истории; backend возвращает confidence и не
показывает прогноз для sparse/unstable series.

## 10. Этап 6 — mock purchase и lifecycle средств

Mock provider воспроизводит `created -> pending -> succeeded|failed`, затем
`refunded|chargeback`. Только идемпотентный verified `succeeded` event создаёт
purchased grant. Проверяются duplicates, reordered/delayed webhook, неправильные
amount/currency, partial/full refund, chargeback и reconciliation. Mock недоступен
обычному production tenant и визуально обозначен как тестовый.

Перед real payments проектируются order/payment/refund entities, webhook
authenticity, amount/currency/catalog binding, tax/VAT/receipt/invoice требования,
FX policy, fraud/velocity limits и settlement reconciliation. Credits не выдаются
по browser redirect или client-provided `succeeded`. Денежный rounding отделён от
credit rounding. Expiration/refundability показываются до покупки и проходят legal
review.

## 11. Этап 7 — superadmin reset

Reset закрывает текущую epoch и открывает новую; usage/heatmap/ledger не удаляются.
Точечный reset требует target, reason, idempotency и notification policy.

Массовый reset: server-side filter snapshot -> dry-run -> число и выборка targets
-> typed confirmation -> durable batch/items -> bounded chunks -> retry only failed
-> result report. Выше настраиваемого порога требуется dual approval. Batch можно
pause/resume; повтор команды не создаёт вторую epoch.

## 12. Этап 8 — applications, API keys и external secrets

Applications бывают sandbox/production и связаны с owner/billing account/environment.
`developer_application` является владельцем API keys, service accounts,
connections, webhook endpoints и usage operations.

Минимальная модель:

```text
application_uuid PK
owner_type user|company
owner_uuid
billing_account_uuid FK
name
environment sandbox|production
status active|disabled|revoked
capabilities text[]
daily_credit_limit bigint
monthly_credit_limit bigint
max_credits_per_operation bigint
created_at, updated_at
```

Application определяет environment, billing account, capabilities и budgets. API
key является только credential и не хранит независимые pricing rules. При каждом
запросе backend проверяет owner/billing consistency и принадлежность key,
connection и usage operation application.

Budget usage включает reserved + settled credits, чтобы параллельные запросы не
обходили limit. Daily window — календарные сутки UTC, monthly — календарный месяц
UTC; уменьшение limit не отменяет существующий reserve, но блокирует новый. `NULL`
означает policy default, а не unlimited; unlimited задаётся отдельной capability.

Ключи `vt_test_<prefix>.<secret>` и `vt_live_<prefix>.<secret>` содержат identity,
environment, scopes, rate limits, expiry/status. Secret — CSPRNG, в БД
только hash/prefix; response с plaintext имеет `Cache-Control: no-store` и
возвращается единожды. Rotation имеет bounded overlap, revoke — немедленный.

`vt_test_*` принимается только sandbox endpoint, `vt_live_*` — только production
endpoint. Несовпадение возвращает `key_environment_mismatch`; redirect/fallback в
другую среду запрещён. Budget checks application выполняются атомарно с полным
reserve.

`usage_operations` обязательно содержит `application_uuid`; application,
connection, API key и billing account фиксируются до reserve и после запуска не
переназначаются.

Создание live key, расширение scopes, смена webhook/redirect URI, reset allowance и
массовые admin actions требуют recent re-auth/MFA либо step-up policy. Key lookup
не раскрывает existence/status по timing или error. Budget checks атомарны с
reservation; forwarded IP доверяется только от allowlisted proxies. Application
deletion сначала revoke keys и drain/cancel operations по cut-off, но не удаляет
ledger/audit.

Секреты внешних CRM write-only: envelope encryption/KMS; GET возвращает только
configured/mask. Они исключаются из URL, browser persistence, logs, traces,
analytics, support exports и audit details.

## 13. Этап 9 — sandbox mock и sandbox real

Тестовые данные — загруженные разработчиком sandbox records и явно маркированные
fixtures VerbaTrace, а не production-данные сервера.

Mock mode: изолированные DB/storage, deterministic mock AI, sandbox ledger,
webhooks/idempotency/errors/retries. Developer может set/add/reset test balance в
ограниченных пределах; каждое действие остаётся ledger event.

Real mode: данные остаются sandbox, но настоящий AI оплачивается real wallet.
Требуются явный request intent, `ai:real`, kill switch, balance, operation/daily/
monthly budgets и уведомления 50/80/90/100%. Выбор модели не зависит от тарифа.
Sandbox data plane не получает прямого доступа к production DB; billing вызывается
через узкий control-plane API.

## 14. Этап 10 — Generic API и внешние интеграции

Только после billing/sandbox gates реализуются durable ingest, safe media fetch,
idempotency, outbox, webhooks и connector emulator согласно связанным integration
спекам. Каждый request однозначно связан с application, tenant и billing account.

Bitrix24 становится connector-verified только после OAuth, реального event/media,
permissions, refresh/revoke, rate limit и reconciliation на test portal/NFR/pilot.
Fixtures не заменяют vendor verification. amoCRM и телефония получают отдельный
adapter contract после успешного общего pilot.

## 15. Обязательный frontend visual quality gate

Visual QA выполняется для каждого нового/изменённого экрана, а не один раз в конце.
Матрица: light/dark x 360/390/768/1024/1280/1440 px x normal/loading/empty/error/
long-content/permission states. Для billing дополнительно проверяются 0/1/99/100%,
очень большой wallet balance, reset today и локали с длинным текстом.

Автоматические проверки:

- browser E2E собирает screenshot baseline по матрице критических экранов;
- axe/эквивалент: contrast, labels, landmarks, focus order;
- DOM geometry assertion для ключевых блоков: bounding rectangles не пересекаются,
  если overlap не предусмотрен дизайном;
- у видимого текста нет clipping/ellipsis без явного UX и нет горизонтального
  overflow документа;
- минимальный внутренний отступ текстовых containers — design token не меньше 8 px,
  между независимыми controls — не меньше 8 px, touch target — не меньше 44x44 px;
- symmetrical groups используют одинаковые container padding/gap и выравнивание;
- skeleton и loaded state сохраняют основные размеры без заметного layout shift;
- focus ring не обрезается overflow-container.

Ручная проверка обязательна после automation: попарное сравнение light/dark,
оптическая симметрия, отсутствие наложений, текста «впритык», orphan labels,
непредусмотренной разницы высот, скачков при hover/focus/error и нечитаемых disabled
states. Допуск screenshot diff утверждается человеком; baseline нельзя обновлять
только ради прохождения теста.

Темы используют общие semantic tokens (`surface`, `text`, `muted`, `border`,
`accent`, `danger`, `focus`), а не локальные hex. Проверяются default/hover/focus/
active/disabled/error/skeleton/chart intensities в обеих темах. Status передаётся
текстом/иконкой, не одним цветом. Минимум WCAG AA: 4.5:1 для обычного текста и
3:1 для крупного текста/UI-компонентов.

## 16. Сквозные production gates

- migrations up/down/forward compatibility на production-like PostgreSQL;
- unit/property/fuzz/integration/race/contract/browser E2E;
- ACL и negative disclosure matrix;
- append-only audit, retention inventory, backup/restore;
- metrics/alerts/dashboard/runbook и capacity limits;
- feature flags и kill switches account/provider/global;
- staged rollout с observation window и документированным rollback;
- численные SLO, capacity envelope, RPO/RTO, clock/NTP alerting и disaster restore
  согласованной пары ledger/object/domain data;
- privacy/data-residency/subprocessor inventory для provider usage, prompts, media,
  sandbox и support exports; sandbox не анонимен по умолчанию;
- abuse/fraud model: stolen keys, credit farming, promo/reset abuse, webhook replay,
  tenant burst и intentional high-cost prompts/media;
- OpenAPI/frontend types синхронны;
- frontend build/lint/tests плюс visual matrix;
- connector claims соответствуют уровню реальной проверки.

Этап считается завершённым только при наличии ссылок на миграции, тесты,
dashboard/runbook и verification evidence. Компиляция, screenshot одного состояния
или mock-only connector не являются доказательством production readiness.

## 17. Решения, которые принимаются по данным, а не заранее

- allowance и денежная цена пакетов по планам;
- доля non-provider variable cost и подтверждение/изменение multiplier `7/2`;
- expiration purchased/promo grants в рамках применимого права;
- forecasting window/confidence threshold;
- mass-reset dual-approval threshold;
- sandbox real budgets и rate limits;
- SLO/capacity/pilot limits;
- конкретные CRM permissions и коммерческая доступность тестовых порталов.

Каждое решение оформляется новой versioned policy с исходными метриками,
утверждающим actor, датой вступления, rollout и rollback plan.
