# Credit billing and integration platform runbook

## Scope

This runbook covers monthly credit allowances, purchased/test wallets, reserve/settle,
developer applications, API keys, generic call ingest, outgoing webhooks and
reconciliation. Payment processing and vendor-specific Bitrix24/amoCRM OAuth are not
part of the current release.

## Required configuration

- `INTEGRATION_MASTER_KEY_BASE64`: base64-encoded 32-byte AES key. Production must use
  a secret manager. Rotation requires a planned re-encryption job before the old key
  is removed.
- OpenRouter/ASR provider credentials are required only for production and sandbox
  `ai_mode=real`. Sandbox `mock` never calls an external AI provider.
- `WORKER_ENABLED=true` enables processing, ingest, webhook and reconciliation workers.
- Upload staging must be a private persistent directory writable only by the service
  account. Do not mount it into a public web root.

The development key in `deploy/docker-compose.yaml` is intentionally non-secret and
must never be reused outside a local environment.

## Deployment order

1. Back up PostgreSQL and verify restore time.
2. Apply migrations through `202608230002` before exposing new routes.
3. Deploy backend with integration workers disabled; check `/health/ready`.
4. Enable reconciliation, webhook and ingest workers one pool at a time.
5. Deploy frontend only after backend routes are available.
6. Run a sandbox mock ingest and verify: accepted item, call creation, mock analysis,
   credit ledger postings, signed webhook and audit event.
7. Enable production applications gradually. Vendor connectors remain disabled until
   their official sandbox/trial acceptance suite has passed.

## Invariants to monitor

The following queries must always return zero rows:

```sql
SELECT credit_ledger_transaction_uuid, sum(amount_credits)
FROM credit_ledger_postings
GROUP BY credit_ledger_transaction_uuid
HAVING sum(amount_credits) <> 0;

SELECT a.credit_ledger_account_uuid, a.account_type, sum(p.amount_credits)
FROM credit_ledger_accounts a
JOIN credit_ledger_postings p USING (credit_ledger_account_uuid)
WHERE a.account_type IN ('customer_available', 'customer_reserved')
GROUP BY a.credit_ledger_account_uuid, a.account_type
HAVING sum(p.amount_credits) < 0;

SELECT usage_operation_uuid
FROM usage_operations
WHERE settled_credits > maximum_charge_credits;
```

Alert immediately on `internal_pricing_loss`, an unbalanced ledger, negative balance,
reconciliation failure, stale provider-running operation, webhook retry exhaustion,
KMS/decryption failure, ingest lease age above the worker threshold, or queue growth
for three consecutive polling windows.

Alerts land in `billing_alerts` and are read from the admin panel: **Журналы →
Алерты биллинга**. The same screen closes one, with a reason that goes into
`admin_audit_logs` (`POST /api/v1/admin/billing-alerts/{alert_uuid}/resolve`).
Closing an alert changes nothing about the operation behind it — settle or
release that first, then close the alert. Reconciliation reopens an alert whose
deduplication key matches, so a premature close is visible rather than lost.

The other append-only trails are on the same screen: admin actions, credit
reconciliation runs, retention, transcript edits and comment revisions. Before
this they could only be read by opening the database by hand.

## Incident procedures

### Credit reservation is stuck

1. Inspect `usage_operations.status` and provider request identifiers.
2. If status is `reserved`, the provider was never marked running and reconciliation
   may safely settle it at zero, releasing the full reservation.
3. If status is `provider_running`, do not guess the outcome. Move it to reconciling,
   compare against provider usage and keep the customer reservation until resolved.
4. Never edit balances directly. Every correction is a balanced ledger transaction
   with an idempotency key and audit reason.

### Webhook receiver is failing

1. Check deliveries by stable event ID, response code, latency and `Retry-After`.
2. Retries are capped at ten attempts and 24 hours for `Retry-After`.
3. Never log response bodies or signing secrets. Use the test-event endpoint after the
   receiver is repaired; consumers must deduplicate by event ID.

### Suspected key leak

1. Revoke the key or its service account immediately.
2. Confirm the old key fails authentication in both environments.
3. Issue a replacement key; its plaintext is displayed once only.
4. Review `last_used_at`, application budgets and audit events. Do not disclose hashes
   or encrypted credentials to operators.

### Queue growth or stuck ingest lease

1. Check worker health, oldest available item, attempts, lease owner and stage.
2. Separate source outage, media validation, billing block and provider outage.
3. Retry only terminal retryable items. Cancellation is allowed before a terminal
   completed state and must preserve audit history.
4. Upload locators are encrypted and terminal staging files are removed by cleanup;
   verify disk usage after recovery.

## Rollback

Disable new application creation and workers first. Keep read APIs and ledger history
available. Do not roll database migrations down after production postings exist.
Rollback the application binary to a version that tolerates the additive schema.
Re-enable workers only after replay/idempotency checks. Revoking an application or key
is not a data deletion operation; call history and audit remain immutable.

## Verification commands

```powershell
go test ./...
go vet ./...
go test -tags=integration ./internal/repository/billing ./internal/repository/integration ./internal/repository/admin
npm.cmd run build
```

Run `VerbaTrace-frontend/scripts/credit-ui-qa.ps1` with local API, preview and CDP
Chrome active. It creates isolated QA users and validates the credit page in light,
dark and mobile layouts plus integrations at 360, 390, 768, 1024, 1280 and 1440 px.

## Readiness boundary

Generic API sandbox/production and the local connector emulator can be accepted from
automated evidence. Bitrix24, amoCRM and telephony connectors cannot be called ready
until OAuth install/refresh/revoke, permissions, rate limits and a real vendor event
have passed on an official trial/sandbox portal. A local emulator does not prove that
vendor contract.
