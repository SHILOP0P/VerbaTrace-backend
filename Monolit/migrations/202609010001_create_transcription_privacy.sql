-- Semantic PII redaction policies, immutable call snapshots and sanitized media.
-- +goose Up
CREATE TABLE transcription_privacy_policies (
    privacy_policy_uuid UUID PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('personal','company')),
    owner_user_uuid UUID REFERENCES users(user_uuid) ON DELETE CASCADE,
    company_uuid UUID REFERENCES companies(company_uuid) ON DELETE CASCADE,
    active_version_uuid UUID,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((scope_type='personal' AND owner_user_uuid IS NOT NULL AND company_uuid IS NULL)
        OR (scope_type='company' AND company_uuid IS NOT NULL AND owner_user_uuid IS NULL))
);

CREATE UNIQUE INDEX uq_privacy_policy_personal
    ON transcription_privacy_policies(owner_user_uuid) WHERE scope_type='personal';
CREATE UNIQUE INDEX uq_privacy_policy_company
    ON transcription_privacy_policies(company_uuid) WHERE scope_type='company';

CREATE TABLE transcription_privacy_policy_drafts (
    privacy_policy_uuid UUID PRIMARY KEY REFERENCES transcription_privacy_policies(privacy_policy_uuid) ON DELETE CASCADE,
    config_schema_version SMALLINT NOT NULL CHECK (config_schema_version = 1),
    config JSONB NOT NULL CHECK (jsonb_typeof(config)='object'),
    config_sha256 BYTEA NOT NULL CHECK (octet_length(config_sha256)=32),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    updated_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transcription_privacy_policy_versions (
    privacy_policy_version_uuid UUID PRIMARY KEY,
    privacy_policy_uuid UUID NOT NULL REFERENCES transcription_privacy_policies(privacy_policy_uuid) ON DELETE CASCADE,
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
    ADD CONSTRAINT fk_privacy_policy_active_version FOREIGN KEY (active_version_uuid)
    REFERENCES transcription_privacy_policy_versions(privacy_policy_version_uuid) ON DELETE RESTRICT;

CREATE TABLE call_privacy_states (
    call_uuid UUID PRIMARY KEY REFERENCES calls(call_uuid) ON DELETE CASCADE,
    privacy_policy_version_uuid UUID REFERENCES transcription_privacy_policy_versions(privacy_policy_version_uuid) ON DELETE RESTRICT,
    policy_source TEXT NOT NULL CHECK (policy_source IN ('none','personal','company','simulated')),
    policy_snapshot JSONB NOT NULL CHECK (jsonb_typeof(policy_snapshot)='object'),
    marker_contract TEXT NOT NULL DEFAULT 'ru-v1',
    status TEXT NOT NULL CHECK (status IN ('not_requested','queued','processing','ready','failed','legacy_unprotected')),
    transcription_revision INTEGER CHECK (transcription_revision > 0),
    detected_spans INTEGER NOT NULL DEFAULT 0 CHECK (detected_spans >= 0),
    last_error_code TEXT,
    last_error_message_safe TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    CHECK ((status='ready' AND transcription_revision IS NOT NULL AND completed_at IS NOT NULL AND last_error_code IS NULL)
        OR status <> 'ready')
);

CREATE INDEX idx_call_privacy_worker ON call_privacy_states(status,updated_at,call_uuid)
    WHERE status IN ('queued','processing','failed');

CREATE TABLE transcription_provider_attempts (
    provider_attempt_uuid UUID PRIMARY KEY,
    call_uuid UUID REFERENCES calls(call_uuid) ON DELETE SET NULL,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    provider TEXT NOT NULL,
    request_sha256 BYTEA NOT NULL CHECK (octet_length(request_sha256)=32),
    provider_job_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('submitting','polling','succeeded','failed','delete_pending','deleting','deleted','delete_failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    last_error_code TEXT,
    last_error_message_safe TEXT,
    submitted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (call_uuid, attempt_no)
);

CREATE UNIQUE INDEX uq_provider_attempt_job ON transcription_provider_attempts(provider,provider_job_id)
    WHERE provider_job_id IS NOT NULL;
CREATE INDEX idx_provider_attempt_cleanup ON transcription_provider_attempts(status,available_at,provider_attempt_uuid)
    WHERE status IN ('delete_pending','delete_failed');

CREATE TABLE call_transcription_redaction_spans (
    redaction_span_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision > 0),
    entity_type TEXT NOT NULL,
    marker TEXT NOT NULL CHECK (marker ~ '^\[[А-ЯЁ0-9_]+\]$'),
    word_start_index INTEGER NOT NULL CHECK (word_start_index >= 0),
    word_end_index INTEGER NOT NULL CHECK (word_end_index >= word_start_index),
    start_seconds NUMERIC(12,3) NOT NULL CHECK (start_seconds >= 0),
    end_seconds NUMERIC(12,3) NOT NULL CHECK (end_seconds >= start_seconds),
    source TEXT NOT NULL CHECK (source IN ('provider','manual')),
    provider_policy TEXT,
    created_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transcription_uuid,revision,word_start_index,word_end_index)
);

CREATE INDEX idx_redaction_spans_revision
    ON call_transcription_redaction_spans(transcription_uuid,revision,word_start_index);

CREATE TABLE call_media_variants (
    media_variant_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    variant TEXT NOT NULL CHECK (variant IN ('redacted_audio','redacted_video')),
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    privacy_policy_version_uuid UUID REFERENCES transcription_privacy_policy_versions(privacy_policy_version_uuid) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('pending','processing','ready','failed','deleting')),
    storage_path TEXT,
    file_name TEXT,
    mime_type TEXT,
    size_bytes BIGINT CHECK (size_bytes IS NULL OR size_bytes > 0),
    processor_contract TEXT NOT NULL DEFAULT 'ffmpeg-redaction-v1',
    container TEXT,
    audio_codec TEXT,
    video_codec TEXT,
    video_stream_copied BOOLEAN,
    output_duration_ms BIGINT CHECK (output_duration_ms IS NULL OR output_duration_ms >= 0),
    source_fingerprint BYTEA NOT NULL CHECK (octet_length(source_fingerprint)=32),
    intervals_sha256 BYTEA NOT NULL CHECK (octet_length(intervals_sha256)=32),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    last_error_code TEXT,
    last_error_message_safe TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    UNIQUE (call_uuid,variant,transcription_revision,intervals_sha256,processor_contract),
    CHECK ((status='ready' AND storage_path IS NOT NULL AND file_name IS NOT NULL AND mime_type IS NOT NULL
        AND size_bytes IS NOT NULL AND container IS NOT NULL AND audio_codec IS NOT NULL
        AND output_duration_ms IS NOT NULL AND completed_at IS NOT NULL) OR status <> 'ready'),
    CHECK ((variant='redacted_audio' AND video_codec IS NULL AND video_stream_copied IS NULL)
        OR (variant='redacted_video' AND (status <> 'ready' OR (video_codec IS NOT NULL AND video_stream_copied IS NOT NULL))))
);

CREATE INDEX idx_media_variants_worker ON call_media_variants(status,available_at,media_variant_uuid)
    WHERE status IN ('pending','failed');

CREATE TABLE privacy_mutation_idempotency (
    actor_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 16 AND 200),
    request_sha256 BYTEA NOT NULL CHECK (octet_length(request_sha256)=32),
    state TEXT NOT NULL CHECK (state IN ('processing','completed','failed_retryable')),
    response_status INTEGER CHECK (response_status BETWEEN 200 AND 599),
    response_body JSONB,
    resource_uuid UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (actor_user_uuid,operation,idempotency_key),
    CHECK ((state='completed') = (response_status IS NOT NULL AND response_body IS NOT NULL))
);

CREATE TABLE media_access_sessions (
    media_access_session_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    actor_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    variant TEXT NOT NULL CHECK (variant IN ('original','redacted')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_access_session_expiry ON media_access_sessions(expires_at);

CREATE TABLE privacy_audit_events (
    privacy_audit_uuid UUID PRIMARY KEY,
    scope_type TEXT NOT NULL,
    scope_uuid UUID NOT NULL,
    call_uuid UUID REFERENCES calls(call_uuid) ON DELETE SET NULL,
    actor_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user','service_account','system','support_grant')),
    event_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_uuid UUID,
    metadata_redacted JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata_redacted)='object'),
    deduplication_key TEXT,
    request_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_privacy_audit_scope_time ON privacy_audit_events(scope_type,scope_uuid,created_at DESC,privacy_audit_uuid DESC);
CREATE INDEX idx_privacy_audit_call_time ON privacy_audit_events(call_uuid,created_at DESC) WHERE call_uuid IS NOT NULL;
CREATE UNIQUE INDEX uq_privacy_audit_deduplication ON privacy_audit_events(event_type,deduplication_key)
    WHERE deduplication_key IS NOT NULL;

INSERT INTO call_privacy_states(call_uuid,policy_source,policy_snapshot,status)
SELECT call_uuid,'none','{}'::jsonb,'legacy_unprotected' FROM calls
ON CONFLICT (call_uuid) DO NOTHING;

ALTER TABLE call_analyses DROP CONSTRAINT chk_call_analyses_status;
ALTER TABLE call_analyses DROP CONSTRAINT chk_call_analyses_status_data;
ALTER TABLE call_analyses ADD CONSTRAINT chk_call_analyses_status
    CHECK (status IN ('pending','processing','done','failed','stale'));
ALTER TABLE call_analyses ADD CONSTRAINT chk_call_analyses_status_data CHECK (
    (status IN ('pending','processing') AND error_message IS NULL)
    OR (status IN ('done','stale') AND result_json IS NOT NULL AND error_message IS NULL)
    OR (status='failed' AND error_message IS NOT NULL)
);

-- +goose Down
UPDATE call_analyses SET status='failed',error_message='Требуется повторный анализ после изменения транскрипции' WHERE status='stale';
ALTER TABLE call_analyses DROP CONSTRAINT chk_call_analyses_status_data;
ALTER TABLE call_analyses DROP CONSTRAINT chk_call_analyses_status;
ALTER TABLE call_analyses ADD CONSTRAINT chk_call_analyses_status
    CHECK (status IN ('pending','processing','done','failed'));
ALTER TABLE call_analyses ADD CONSTRAINT chk_call_analyses_status_data CHECK (
    (status IN ('pending','processing') AND error_message IS NULL)
    OR (status='done' AND result_json IS NOT NULL AND error_message IS NULL)
    OR (status='failed' AND error_message IS NOT NULL)
);
DROP TABLE IF EXISTS privacy_audit_events;
DROP TABLE IF EXISTS media_access_sessions;
DROP TABLE IF EXISTS privacy_mutation_idempotency;
DROP TABLE IF EXISTS call_media_variants;
DROP TABLE IF EXISTS call_transcription_redaction_spans;
DROP TABLE IF EXISTS transcription_provider_attempts;
DROP TABLE IF EXISTS call_privacy_states;
ALTER TABLE transcription_privacy_policies DROP CONSTRAINT IF EXISTS fk_privacy_policy_active_version;
DROP TABLE IF EXISTS transcription_privacy_policy_versions;
DROP TABLE IF EXISTS transcription_privacy_policy_drafts;
DROP TABLE IF EXISTS transcription_privacy_policies;
