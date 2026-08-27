# Developer integrations API v1

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
GET       /api/v1/integrations/{connection_uuid}/audit-events
```

`PATCH` and connection status changes require `If-Match: <lock_version>`. A stale
version returns HTTP 409. Creation/commands that can be retried use `Idempotency-Key`.

## Public ingest

```text
POST /api/{sandbox|production}/v1/ingest/calls
POST /api/{sandbox|production}/v1/ingest/calls/upload
GET  /api/{sandbox|production}/v1/ingest/items/{ingest_item_uuid}
```

URL ingest accepts schema version 1, stable external event/call IDs, title, HTTPS
recording URL, participants and bounded metadata. Upload ingest is multipart with a
500 MiB hard cap. Reusing an idempotency key or external call ID with a different
payload returns 409. The accepted response contains a status URL; processing is
asynchronous.

URLs are resolved and fetched with SSRF protection: private, loopback, link-local and
metadata networks are rejected, redirects are revalidated, and body/time limits are
enforced. Uploaded source locators and external secrets are encrypted at rest.

## Webhook contract

Each event has a stable event ID and timestamp. Delivery signs the raw body with the
endpoint secret using HMAC-SHA256. Consumers must verify the signature in constant
time, enforce a timestamp window and deduplicate event IDs. Retried deliveries keep
the same event ID. A receiver should return 2xx only after durably recording the event.

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
