# Developer integrations API v1

Interactive Swagger UI is served by the backend at `/docs/integrations`. The raw
OpenAPI 3.1 document is available at `/docs/integrations/openapi` (with
`/docs/integrations/openapi.yaml` kept as an alias) for client
generation and import into API tools.

All management routes use the authenticated VerbaTrace session. Public ingest routes
use `Authorization: Bearer <key>` or `X-API-Key`. Secrets are returned once with
`Cache-Control: no-store`; subsequent list responses contain metadata only.

## Environments

- Test keys start with `vt_test_` and work only under `/api/sandbox/v1`.
- Live keys start with `vt_live_` and work only under `/api/production/v1`.
- A mismatch returns stable error `key_environment_mismatch`; there is no redirect.
- Sandbox defaults to `ai_mode=mock`. `ai_mode=real` requires scope `ai:real` and uses
  the production billing wallet. Production accepts only `real`.

## Management routes

```text
GET|POST  /api/v1/developer/applications
GET|PATCH /api/v1/developer/applications/{application_uuid}
POST      /api/v1/developer/applications/{application_uuid}/{enable|disable|revoke}
POST      /api/v1/developer/applications/{application_uuid}/sandbox-wallet

GET|POST  /api/v1/developer/applications/{application_uuid}/connections
GET|PATCH /api/v1/integrations/{connection_uuid}
POST      /api/v1/integrations/{connection_uuid}/{enable|disable}
DELETE    /api/v1/integrations/{connection_uuid}

GET|POST  /api/v1/integrations/{connection_uuid}/service-accounts
DELETE    /api/v1/service-accounts/{service_account_uuid}
GET|POST  /api/v1/service-accounts/{service_account_uuid}/keys
POST      /api/v1/developer/keys/{key_uuid}/rotate
DELETE    /api/v1/developer/keys/{key_uuid}

GET|POST  /api/v1/integrations/{connection_uuid}/webhooks
DELETE    /api/v1/integration-webhooks/{webhook_uuid}
POST      /api/v1/integrations/{connection_uuid}/webhook/test
GET       /api/v1/integrations/{connection_uuid}/webhook-deliveries
POST      /api/v1/webhook-deliveries/{delivery_uuid}/replay
GET       /api/v1/integrations/{connection_uuid}/audit-events
```

`PATCH` and connection status changes require `If-Match: <lock_version>`. A stale
version returns HTTP 409. Creation/commands that can be retried use `Idempotency-Key`.

## Public ingest

```text
POST /api/{sandbox|production}/v1/ingest/calls
POST /api/{sandbox|production}/v1/ingest/calls/upload
GET  /api/{sandbox|production}/v1/ingest/items/{ingest_item_uuid}
GET  /api/{sandbox|production}/v2/calls
GET  /api/{sandbox|production}/v2/calls/by-source-ref/{source_ref}
GET  /api/{sandbox|production}/v2/usage
```

URL ingest accepts schema version 1, stable external event/call IDs, title, HTTPS
recording URL, participants and bounded metadata. Upload ingest is multipart with a
500 MiB hard cap. Reusing an idempotency key or external call ID with a different
payload returns 409. The accepted response contains a status URL; processing is
asynchronous.

An accepted call whose owner has no credits left is not rejected: it is parked
with status `awaiting_credits` and starts on its own once the limit renews. The
depth of that queue comes from the plan, so an upload can still be refused with
`409 pending_credit_queue_full` when too many calls are already waiting. A call
whose processing was stopped on purpose reports `cancelled`; clients polling a
status URL must treat both as ordinary states rather than failures.

Version 2 returns a stable `source_ref` for each accepted source call. It is namespaced
by the server-side connection identity, so equal external IDs from different users,
companies or providers cannot collide. Clients should persist `source_ref` and use it
for recovery after a timeout. The calls collection supports `updated_since`, `from`,
`to`, `status`, `limit` and opaque cursor pagination.

When a key is created, the request may set a cumulative permanent credit ceiling and
a cumulative credit ceiling active during an explicit UTC time window. These values
are immutable after creation, enforced in addition to application limits, and copied
when the key is rotated.

URLs are resolved and fetched with SSRF protection: private, loopback, link-local and
metadata networks are rejected, redirects are revalidated, and body/time limits are
enforced. Uploaded source locators and external secrets are encrypted at rest.

## Webhook contract

Each event has a stable event ID and timestamp. Delivery signs the raw body with the
endpoint secret using HMAC-SHA256. Consumers must verify the signature in constant
time, enforce a timestamp window and deduplicate event IDs. Retried deliveries keep
the same event ID. A receiver should return 2xx only after durably recording the event.

Supported result-ready events include `transcription.completed` and
`analysis.completed`. A failed delivery can be placed onto a fresh delivery cycle with
the replay route; the replay action is audited.

Analysis results use `schema_version: 3`. Within a schema version fields are only
added, never renamed or removed, so consumers must ignore fields they do not know.
Since pipeline `universal-staged-v7` (`prompt_version: universal-v3.2`) a result may
carry `scorecard_mode` (`fixed`, `partial`, `adhoc`, `none`), `scorecards` and
`scorecard_limit_applied`, and a requirement item may carry `criterion_key`,
`also_criterion_keys`, `scorecard_uuid` and `is_critical`. `criterion_key` is stable
across calls and instruction versions, so it is the key to aggregate requirement
results by; items without it were scored ad hoc and are not comparable between calls.

## Error envelope

```json
{
  "error": {
    "code": "stable_machine_code",
    "message": "safe message",
    "request_id": "trace identifier",
    "retryable": false
  }
}
```

Credentials, raw provider responses and source URLs are never included in errors or
audit metadata.
