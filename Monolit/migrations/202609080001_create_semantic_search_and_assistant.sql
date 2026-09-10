-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE search_index_profiles (
    search_index_profile_uuid UUID PRIMARY KEY,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL CHECK (dimensions > 0),
    chunker_version TEXT NOT NULL,
    retrieval_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('building','active','retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, model, dimensions, chunker_version, retrieval_version)
);
CREATE UNIQUE INDEX uq_search_index_profiles_active ON search_index_profiles ((status)) WHERE status='active';

CREATE TABLE call_search_documents (
    call_search_document_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    content_sha256 BYTEA NOT NULL,
    privacy_revision INTEGER,
    profile_uuid UUID NOT NULL REFERENCES search_index_profiles(search_index_profile_uuid) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('pending','waiting_privacy','indexing','ready','failed','stale','deleting','deleted')),
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (call_uuid, transcription_revision, profile_uuid)
);

CREATE TABLE call_search_chunks (
    call_search_chunk_uuid UUID PRIMARY KEY,
    call_search_document_uuid UUID NOT NULL REFERENCES call_search_documents(call_search_document_uuid) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    text TEXT NOT NULL CHECK (length(trim(text)) > 0),
    speaker TEXT,
    start_seconds DOUBLE PRECISION,
    end_seconds DOUBLE PRECISION,
    text_search TSVECTOR GENERATED ALWAYS AS (to_tsvector('russian', text)) STORED,
    embedding vector(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (call_search_document_uuid, ordinal),
    CHECK (start_seconds IS NULL OR start_seconds >= 0),
    CHECK (end_seconds IS NULL OR (end_seconds >= 0 AND (start_seconds IS NULL OR end_seconds >= start_seconds)))
);
CREATE INDEX idx_call_search_documents_ready ON call_search_documents(call_uuid,profile_uuid) WHERE status='ready';
CREATE INDEX idx_call_search_chunks_fts ON call_search_chunks USING GIN(text_search);
CREATE INDEX idx_call_search_chunks_embedding ON call_search_chunks USING hnsw (embedding vector_cosine_ops) WHERE embedding IS NOT NULL;

CREATE TABLE assistant_chats (
    assistant_chat_uuid UUID PRIMARY KEY,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    owner_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 120),
    response_detail TEXT NOT NULL DEFAULT 'auto' CHECK (response_detail IN ('auto','brief','detailed')),
    archived_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_chats_owner ON assistant_chats(owner_user_uuid,company_uuid,updated_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE assistant_messages (
    assistant_message_uuid UUID PRIMARY KEY,
    assistant_chat_uuid UUID NOT NULL REFERENCES assistant_chats(assistant_chat_uuid) ON DELETE CASCADE,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    role TEXT NOT NULL CHECK (role IN ('user','assistant')),
    content_json JSONB NOT NULL,
    client_message_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending','completed','partial','failed','cancelled','unavailable')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    UNIQUE (assistant_chat_uuid,sequence),
    UNIQUE NULLS NOT DISTINCT (assistant_chat_uuid,client_message_id)
);

CREATE TABLE assistant_runs (
    assistant_run_uuid UUID PRIMARY KEY,
    assistant_chat_uuid UUID NOT NULL REFERENCES assistant_chats(assistant_chat_uuid) ON DELETE CASCADE,
    user_message_uuid UUID NOT NULL REFERENCES assistant_messages(assistant_message_uuid) ON DELETE CASCADE,
    actor_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    filter_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_manifest_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    response_detail TEXT NOT NULL CHECK (response_detail IN ('auto','brief','detailed')),
    state TEXT NOT NULL CHECK (state IN ('queued','preparing','retrieving','generating','validating','completed','partial','failed','cancelled','access_revoked')),
    error_code TEXT,
    provider TEXT,
    model TEXT,
    usage_operation_uuid UUID REFERENCES usage_operations(usage_operation_uuid) ON DELETE RESTRICT,
    request_received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    data_snapshot_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    UNIQUE (company_uuid,actor_user_uuid,idempotency_key)
);
CREATE UNIQUE INDEX uq_assistant_active_run_per_chat ON assistant_runs(assistant_chat_uuid)
WHERE state IN ('queued','preparing','retrieving','generating','validating');

CREATE TABLE assistant_citations (
    assistant_citation_uuid UUID PRIMARY KEY,
    assistant_message_uuid UUID NOT NULL REFERENCES assistant_messages(assistant_message_uuid) ON DELETE CASCADE,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    chunk_uuid UUID REFERENCES call_search_chunks(call_search_chunk_uuid) ON DELETE SET NULL,
    quote TEXT NOT NULL,
    start_seconds DOUBLE PRECISION,
    end_seconds DOUBLE PRECISION,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    UNIQUE (assistant_message_uuid,ordinal)
);

CREATE TABLE assistant_artifacts (
    assistant_artifact_uuid UUID PRIMARY KEY,
    assistant_message_uuid UUID NOT NULL REFERENCES assistant_messages(assistant_message_uuid) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL CHECK (artifact_type IN ('chart','table')),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    schema_version INTEGER NOT NULL DEFAULT 1,
    data_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE assistant_chat_drafts (
    assistant_chat_uuid UUID PRIMARY KEY REFERENCES assistant_chats(assistant_chat_uuid) ON DELETE CASCADE,
    owner_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    text TEXT NOT NULL DEFAULT '' CHECK (length(text) <= 12000),
    context_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS assistant_chat_drafts;
DROP TABLE IF EXISTS assistant_artifacts;
DROP TABLE IF EXISTS assistant_citations;
DROP INDEX IF EXISTS uq_assistant_active_run_per_chat;
DROP TABLE IF EXISTS assistant_runs;
DROP TABLE IF EXISTS assistant_messages;
DROP TABLE IF EXISTS assistant_chats;
DROP TABLE IF EXISTS call_search_chunks;
DROP TABLE IF EXISTS call_search_documents;
DROP INDEX IF EXISTS uq_search_index_profiles_active;
DROP TABLE IF EXISTS search_index_profiles;
