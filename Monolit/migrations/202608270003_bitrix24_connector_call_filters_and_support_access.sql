-- +goose Up
ALTER TABLE integration_connections DROP CONSTRAINT IF EXISTS integration_connections_status_check;
ALTER TABLE integration_connections ADD CONSTRAINT integration_connections_status_check CHECK (status IN (
    'draft','authorizing','testing','active','degraded','paused','disabled','reconnect_required','revoked'
));
CREATE UNIQUE INDEX uq_bitrix24_portal_member_active
    ON integration_connections(provider,(settings->>'portal_member_id'))
    WHERE provider='bitrix24' AND status<>'revoked' AND NULLIF(settings->>'portal_member_id','') IS NOT NULL;

ALTER TABLE calls ADD COLUMN occurred_at TIMESTAMPTZ;
UPDATE calls c
   SET occurred_at=i.occurred_at
  FROM ingest_items i
 WHERE c.ingest_item_uuid=i.ingest_item_uuid
   AND i.occurred_at IS NOT NULL;
CREATE INDEX idx_calls_company_occurred
    ON calls(company_uuid,occurred_at DESC,call_uuid) WHERE occurred_at IS NOT NULL;
CREATE INDEX idx_calls_department_occurred
    ON calls(department_uuid,occurred_at DESC,call_uuid) WHERE occurred_at IS NOT NULL;
CREATE INDEX idx_ingest_connection_occurred
    ON ingest_items(connection_uuid,occurred_at DESC,ingest_item_uuid) WHERE occurred_at IS NOT NULL;

CREATE TABLE integration_oauth_states (
    oauth_state_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    state_hash BYTEA NOT NULL UNIQUE,
    redirect_uri TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
CREATE INDEX idx_oauth_states_expiry ON integration_oauth_states(expires_at) WHERE consumed_at IS NULL;

CREATE TABLE integration_oauth_credentials (
    connection_uuid UUID PRIMARY KEY REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    access_token_ciphertext BYTEA NOT NULL,
    access_token_nonce BYTEA NOT NULL,
    refresh_token_ciphertext BYTEA,
    refresh_token_nonce BYTEA,
    key_version INTEGER NOT NULL CHECK (key_version > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    refresh_state TEXT NOT NULL DEFAULT 'ready' CHECK (refresh_state IN ('ready','refreshing','failed','revoked')),
    refresh_lease_until TIMESTAMPTZ,
    last_refreshed_at TIMESTAMPTZ,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((refresh_token_ciphertext IS NULL) = (refresh_token_nonce IS NULL))
);

CREATE TABLE integration_external_user_mappings (
    mapping_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    external_user_id TEXT NOT NULL,
    internal_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    department_uuid UUID REFERENCES departments(department_uuid) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'unmapped' CHECK (status IN ('unmapped','mapped','conflict','inactive','ignored')),
    external_display_snapshot TEXT NOT NULL DEFAULT '',
    external_active BOOLEAN NOT NULL DEFAULT TRUE,
    mapped_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    mapped_at TIMESTAMPTZ,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(connection_uuid,external_user_id),
    CHECK ((status='mapped') = (internal_user_uuid IS NOT NULL)),
    CHECK ((mapped_at IS NULL) = (mapped_by_user_uuid IS NULL))
);
CREATE INDEX idx_external_mappings_internal ON integration_external_user_mappings(internal_user_uuid,connection_uuid)
    WHERE internal_user_uuid IS NOT NULL;

CREATE TABLE integration_call_participants (
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    external_user_id TEXT NOT NULL,
    internal_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'participant',
    display_snapshot TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(call_uuid,connection_uuid,external_user_id)
);
CREATE INDEX idx_call_participants_user ON integration_call_participants(internal_user_uuid,call_uuid)
    WHERE internal_user_uuid IS NOT NULL;

CREATE TABLE integration_sync_checkpoints (
    connection_uuid UUID PRIMARY KEY REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    cursor_occurred_at TIMESTAMPTZ,
    cursor_external_id TEXT,
    overlap_seconds INTEGER NOT NULL DEFAULT 300 CHECK (overlap_seconds BETWEEN 0 AND 86400),
    provider_contract_version INTEGER NOT NULL DEFAULT 1 CHECK (provider_contract_version > 0),
    last_reconciliation_at TIMESTAMPTZ,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((cursor_occurred_at IS NULL) = (cursor_external_id IS NULL))
);

CREATE TABLE bitrix_call_candidates (
    candidate_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    external_call_id TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    external_user_id TEXT,
    call_direction TEXT,
    duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
    status TEXT NOT NULL CHECK (status IN ('waiting_for_recording','queued','imported','skipped_no_media','failed')),
    payload_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_hash BYTEA NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    recording_deadline_at TIMESTAMPTZ NOT NULL,
    ingest_item_uuid UUID REFERENCES ingest_items(ingest_item_uuid) ON DELETE SET NULL,
    error_code TEXT,
	attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
	max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts > 0),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    UNIQUE(connection_uuid,external_call_id)
);
CREATE INDEX idx_bitrix_candidates_waiting ON bitrix_call_candidates(status,last_seen_at,candidate_uuid)
    WHERE status IN ('waiting_for_recording','queued','failed');

CREATE TABLE integration_backfills (
    backfill_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    range_from TIMESTAMPTZ NOT NULL,
    range_to TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','paused','completed','cancelled','failed')),
    estimated_calls INTEGER CHECK (estimated_calls IS NULL OR estimated_calls >= 0),
    discovered_calls INTEGER NOT NULL DEFAULT 0 CHECK (discovered_calls >= 0),
    imported_calls INTEGER NOT NULL DEFAULT 0 CHECK (imported_calls >= 0),
    skipped_calls INTEGER NOT NULL DEFAULT 0 CHECK (skipped_calls >= 0),
    error_calls INTEGER NOT NULL DEFAULT 0 CHECK (error_calls >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    CHECK (range_from < range_to)
);
CREATE INDEX idx_integration_backfills_worker ON integration_backfills(status,available_at,created_at)
    WHERE status IN ('pending','running');

CREATE TABLE call_action_external_syncs (
    sync_uuid UUID PRIMARY KEY,
    action_uuid UUID NOT NULL REFERENCES call_actions(action_uuid) ON DELETE CASCADE,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    provider TEXT NOT NULL CHECK (provider IN ('bitrix24')),
    operation TEXT NOT NULL DEFAULT 'create_task' CHECK (operation IN ('create_task')),
    requester_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    approver_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'pending_approval' CHECK (state IN (
        'pending_approval','rejected','queued','sending','reconciling','needs_review','synced','failed','cancelled','unlinked'
    )),
    external_task_id TEXT,
    external_task_url TEXT,
    idempotency_marker TEXT NOT NULL UNIQUE,
    request_payload JSONB NOT NULL,
    request_payload_hash BYTEA NOT NULL,
    action_lock_version BIGINT NOT NULL CHECK (action_lock_version > 0),
    last_confirmed_external_version TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    last_error_code TEXT,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    rejected_at TIMESTAMPTZ,
    synced_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (approver_user_uuid IS NULL OR approver_user_uuid <> requester_user_uuid),
    CHECK ((state='synced') = (external_task_id IS NOT NULL))
);
CREATE UNIQUE INDEX uq_action_external_create_task
    ON call_action_external_syncs(action_uuid,connection_uuid,operation)
    WHERE state NOT IN ('rejected','cancelled','unlinked');
CREATE INDEX idx_action_external_sync_worker ON call_action_external_syncs(state,available_at,created_at)
    WHERE state IN ('queued','sending','reconciling');

CREATE TABLE support_access_requests (
    request_uuid UUID PRIMARY KEY,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    approver_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user','company')),
    subject_user_uuid UUID REFERENCES users(user_uuid) ON DELETE CASCADE,
    subject_company_uuid UUID REFERENCES companies(company_uuid) ON DELETE CASCADE,
    reason TEXT NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 2000),
    requested_resources TEXT[] NOT NULL CHECK (cardinality(requested_resources) > 0 AND requested_resources <@ ARRAY['calls','actions','integrations','billing_summary']::text[]),
    requested_commands TEXT[] NOT NULL DEFAULT '{}' CHECK (requested_commands <@ ARRAY['diagnose','retry_ingest','reconnect_integration']::text[]),
    requested_duration_minutes INTEGER NOT NULL CHECK (requested_duration_minutes BETWEEN 5 AND 1440),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied','expired','cancelled')),
    decision_comment TEXT,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ,
    CHECK (requested_by_user_uuid <> approver_user_uuid),
    CHECK ((subject_type='user' AND subject_user_uuid IS NOT NULL AND subject_company_uuid IS NULL) OR
           (subject_type='company' AND subject_company_uuid IS NOT NULL AND subject_user_uuid IS NULL)),
    CHECK (expires_at > created_at)
);
CREATE UNIQUE INDEX uq_support_access_pending_subject
    ON support_access_requests(requested_by_user_uuid,approver_user_uuid,subject_type,
        COALESCE(subject_user_uuid,'00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(subject_company_uuid,'00000000-0000-0000-0000-000000000000'::uuid))
    WHERE status='pending';

CREATE TABLE support_access_grants (
    grant_uuid UUID PRIMARY KEY,
    request_uuid UUID NOT NULL UNIQUE REFERENCES support_access_requests(request_uuid) ON DELETE RESTRICT,
    grantee_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    granted_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user','company')),
    subject_user_uuid UUID REFERENCES users(user_uuid) ON DELETE CASCADE,
    subject_company_uuid UUID REFERENCES companies(company_uuid) ON DELETE CASCADE,
    resource_allowlist TEXT[] NOT NULL CHECK (cardinality(resource_allowlist) > 0 AND resource_allowlist <@ ARRAY['calls','actions','integrations','billing_summary']::text[]),
    command_allowlist TEXT[] NOT NULL DEFAULT '{}' CHECK (command_allowlist <@ ARRAY['diagnose','retry_ingest','reconnect_integration']::text[]),
    valid_from TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    revoke_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (grantee_user_uuid <> granted_by_user_uuid),
    CHECK (expires_at > valid_from),
    CHECK ((subject_type='user' AND subject_user_uuid IS NOT NULL AND subject_company_uuid IS NULL) OR
           (subject_type='company' AND subject_company_uuid IS NOT NULL AND subject_user_uuid IS NULL)),
    CHECK ((revoked_at IS NULL) = (revoked_by_user_uuid IS NULL))
);
CREATE INDEX idx_support_grants_runtime ON support_access_grants(grantee_user_uuid,expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE support_access_events (
    event_uuid UUID PRIMARY KEY,
    request_uuid UUID REFERENCES support_access_requests(request_uuid) ON DELETE RESTRICT,
    grant_uuid UUID REFERENCES support_access_grants(grant_uuid) ON DELETE RESTRICT,
	actor_type TEXT NOT NULL DEFAULT 'user' CHECK (actor_type IN ('user','system')),
    actor_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    event_type TEXT NOT NULL,
    resource TEXT,
    command TEXT,
    metadata_safe JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	CHECK (request_uuid IS NOT NULL OR grant_uuid IS NOT NULL),
	CHECK ((actor_type='user') = (actor_user_uuid IS NOT NULL))
);
CREATE INDEX idx_support_access_events_request ON support_access_events(request_uuid,created_at,event_uuid);
CREATE TRIGGER trg_support_access_events_immutable
BEFORE UPDATE OR DELETE ON support_access_events
FOR EACH ROW EXECUTE FUNCTION reject_credit_ledger_mutation();

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid',
    'support_access_requested','support_access_decided','action_external_sync_requested','action_external_sync_decided'
));

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid'
));
DROP TABLE IF EXISTS support_access_events;
DROP TABLE IF EXISTS support_access_grants;
DROP TABLE IF EXISTS support_access_requests;
DROP TABLE IF EXISTS call_action_external_syncs;
DROP TABLE IF EXISTS integration_backfills;
DROP TABLE IF EXISTS bitrix_call_candidates;
DROP TABLE IF EXISTS integration_sync_checkpoints;
DROP TABLE IF EXISTS integration_call_participants;
DROP TABLE IF EXISTS integration_external_user_mappings;
DROP TABLE IF EXISTS integration_oauth_credentials;
DROP TABLE IF EXISTS integration_oauth_states;
DROP INDEX IF EXISTS idx_ingest_connection_occurred;
DROP INDEX IF EXISTS idx_calls_department_occurred;
DROP INDEX IF EXISTS idx_calls_company_occurred;
ALTER TABLE calls DROP COLUMN IF EXISTS occurred_at;
DROP INDEX IF EXISTS uq_bitrix24_portal_member_active;
UPDATE integration_connections SET status='draft' WHERE status IN ('authorizing','testing');
UPDATE integration_connections SET status='disabled' WHERE status='paused';
UPDATE integration_connections SET status='degraded' WHERE status='reconnect_required';
ALTER TABLE integration_connections DROP CONSTRAINT IF EXISTS integration_connections_status_check;
ALTER TABLE integration_connections ADD CONSTRAINT integration_connections_status_check
    CHECK (status IN ('draft','active','degraded','disabled','revoked'));
