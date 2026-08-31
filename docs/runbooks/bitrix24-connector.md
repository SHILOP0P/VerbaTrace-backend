# Runbook: Bitrix24 connector

Дата проверки документа: 2026-08-28.

Этот runbook относится к company-scoped Bitrix24 connection. Он не является
доказательством работы на реальном портале: результат pilot фиксируется отдельно
по checklist в конце документа.

## Безопасная конфигурация

Обязательные переменные процесса: `INTEGRATION_MASTER_KEY_BASE64`,
`BITRIX24_CLIENT_ID`, `BITRIX24_CLIENT_SECRET`, `BITRIX24_REDIRECT_URI`,
`PUBLIC_APP_URL`. Для event endpoint дополнительно нужен
`BITRIX24_APPLICATION_TOKEN`. Значения токенов и client secret запрещено помещать
в тикеты, логи, audit metadata и screenshots.

Connection создаёт и подключает только активный manager компании. Остановка
connection выполняется через pause/revoke; удалять imported calls, actions,
provenance и audit при rollback нельзя. Platform support сначала создаёт заявку,
владелец или manager подтверждает точные ресурсы и команды, после чего доступ
автоматически истекает и может быть отозван раньше.

## Быстрая диагностика

1. Открыть `Настройки → Интеграции → Bitrix24` и проверить статус connection,
   время последней проверки, capabilities и код ошибки.
2. Если `reconnect_required`, не повторять ingest: manager заново запускает OAuth,
   затем выполняет проверку подключения.
3. Если отсутствуют calls/users/tasks capabilities, сверить выданные scopes и
   права OAuth-пользователя в портале. Наличие scope не гарантирует право
   конкретного пользователя.
4. Если растёт очередь, проверить активность connection, срок OAuth credentials,
   rate-limit/5xx портала, возраст checkpoint и самые старые candidates.
5. Если запись задержана, оставить candidate в `waiting_for_recording` до deadline;
   не создавать второй ingest вручную с другим external ID.
6. Для неоднозначного `tasks.task.add` проверить портал по idempotency marker.
   Связать найденную задачу по числовому ID, отменить отправку либо осознанно
   повторить её. Повтор без проверки может создать дубль.
7. При mapping conflict обновить внешний список, заново построить bulk preview и
   применить весь набор. Не обходить optimistic lock отдельными SQL updates.

## Контрольные SQL-запросы

Запросы выполняются read-only пользователем БД. UUID и внешние ID не должны
становиться labels метрик.

```sql
SELECT status, count(*)
FROM bitrix_call_candidates
GROUP BY status;

SELECT state, count(*), min(available_at) AS oldest_available_at
FROM call_action_external_syncs
GROUP BY state;

SELECT count(*) AS task_conflicts
FROM call_action_external_syncs
WHERE review_state = 'needs_review';

SELECT count(*) AS unmapped_users
FROM integration_external_user_mappings
WHERE status IN ('unmapped', 'conflict', 'inactive');

SELECT status, count(*), min(updated_at) AS oldest_update
FROM integration_backfills
GROUP BY status;
```

## Alert conditions до pilot

Численные пороги утверждаются только после измерений тестового портала. До этого
обязательны события без выдуманного SLO: OAuth refresh failure, scope/capability
loss, непрерывный рост queue age/depth, vendor 429/5xx, reconciliation lag,
candidate после recording deadline, duplicate alarm, task `needs_review`, failed
backfill, исчерпание credits, ошибка шифрования/хранилища и остановка worker.

Health одного портала не должен переводить весь VerbaTrace в unready. При массовом
сбое сначала pause affected connections или workers, сохраняя read path. После
восстановления запускать reconciliation/backfill небольшими диапазонами и
контролировать dedup/billing.

## Сценарии инцидентов

### OAuth истёк, приложение удалено или scope потерян

Pause новые claims, сохранить queued records, уведомить manager. Manager выполняет
reconnect и повторный capability test. После успеха сначала reconciliation, затем
обычная очередь. Старые refresh tokens не показывать и не копировать.

### Portal outage, timeout, 429 или 503

Не повторять внешнюю запись задачи после timeout с неизвестным результатом.
Перевести её в ручную проверку. Read/import операции оставить в retry с lease и
ограниченным backoff; не держать DB transaction во время сети. Если outage общий,
остановить новые backfills.

### Подозрение на duplicate call или повторное списание

Pause connection. Сопоставить `connection_uuid + external_call_id`, candidate,
ingest item, call provenance и billing operation. Не удалять одну запись до
выяснения источника. После подтверждения исправлять причину dedup/idempotency,
затем выполнять финансовую коррекцию штатной ledger-операцией.

### Mapping изменён параллельно

Получить свежий список и lock versions, повторить preview. Bulk command атомарна:
частично применённый результат считается дефектом и требует остановки mapping
updates до проверки транзакции и audit event `mapping.bulk_updated`.

### Task изменилась или удалена в Bitrix24

VerbaTrace не перезаписывает action автоматически. Manager/leader выбирает:
принять внешний snapshot, вернуть поля VerbaTrace или разорвать связь. Причина не
короче 10 символов обязательна; решение сохраняется в action history и audit.

## Rollback и восстановление

Остановить Bitrix event claims, reconciler, backfill и task write-back. Connection
перевести в pause/revoked согласно причине. Не откатывать forward migration до
проверки совместимости старого binary. Backup должен включать integration
connections/settings без plaintext secrets, encrypted credentials, checkpoints,
candidates, backfills, mappings, external syncs, support grants/events и основной
call/action/billing provenance. Restore проверяется в отдельной среде: foreign
keys, audit immutability, decryptability текущей key version и отсутствие повторной
обработки уже завершённых items.

## Real-portal checklist

Для каждого шага сохранить дату, portal/member ID в закрытом артефакте, исполнителя,
HTTP/result без secrets и ссылку на call/action/audit:

- OAuth install, capability test, refresh, reconnect, revoke и uninstall;
- один реальный звонок из `voximplant.statistic.get` и доступная запись;
- потерянный event, восстановленный reconciliation без второго call/charge;
- backfill preview, pause/resume и завершение;
- user mapping и атомарный bulk conflict;
- task approval/add/get/update, изменение/completion/deletion и ручное решение;
- timeout после remote commit с поиском marker без слепого повтора;
- light/dark/mobile/keyboard проверка основного пути;
- queue/lag/failure alerts и rollback exercise.

Только после прохождения всех пунктов разрешено выставить `connector_verified`.
