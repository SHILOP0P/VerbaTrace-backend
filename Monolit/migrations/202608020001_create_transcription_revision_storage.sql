-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE call_transcription_contents (
    transcription_content_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    content_sha256 BYTEA NOT NULL,
    canonical_size_bytes BIGINT NOT NULL CHECK (canonical_size_bytes > 0),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (transcription_uuid, transcription_content_uuid)
);

CREATE INDEX idx_transcription_contents_hash
    ON call_transcription_contents (transcription_uuid, content_sha256, canonical_size_bytes);

CREATE TABLE call_transcription_revisions (
    transcription_revision_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    transcription_content_uuid UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    reason TEXT NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 3 AND 500),
    changed_word_indexes JSONB NOT NULL CHECK (jsonb_typeof(changed_word_indexes) = 'array'),
    restored_from_revision INTEGER NULL,
    created_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (transcription_uuid, revision),
    FOREIGN KEY (transcription_uuid, transcription_content_uuid)
        REFERENCES call_transcription_contents(transcription_uuid, transcription_content_uuid)
        ON DELETE RESTRICT
);

CREATE INDEX idx_transcription_revisions_history
    ON call_transcription_revisions (transcription_uuid, revision DESC);

CREATE TABLE call_transcription_edit_audit (
    audit_uuid UUID PRIMARY KEY,
    transcription_uuid UUID NOT NULL REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    actor_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    operation VARCHAR(32) NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_transcription_edit_audit_lookup
    ON call_transcription_edit_audit (transcription_uuid, created_at DESC);

INSERT INTO call_transcription_contents (
    transcription_content_uuid, transcription_uuid, content_sha256, canonical_size_bytes, payload, created_at
)
SELECT gen_random_uuid(), transcription_uuid,
       digest(jsonb_build_object('text', text, 'segments', COALESCE(segments, '[]'::jsonb), 'words', COALESCE(words, '[]'::jsonb))::text, 'sha256'),
       octet_length(jsonb_build_object('text', text, 'segments', COALESCE(segments, '[]'::jsonb), 'words', COALESCE(words, '[]'::jsonb))::text),
       jsonb_build_object('text', text, 'segments', COALESCE(segments, '[]'::jsonb), 'words', COALESCE(words, '[]'::jsonb)),
       COALESCE(updated_at, now())
FROM call_transcriptions
WHERE status = 'transcribed'
  AND NOT EXISTS (
      SELECT 1 FROM call_transcription_revisions r WHERE r.transcription_uuid = call_transcriptions.transcription_uuid
  );

INSERT INTO call_transcription_revisions (
    transcription_revision_uuid, transcription_uuid, transcription_content_uuid, revision,
    reason, changed_word_indexes, created_at
)
SELECT gen_random_uuid(), c.transcription_uuid, c.transcription_content_uuid, 1,
       'Исходная транскрипция ASR', '[]'::jsonb, c.created_at
FROM call_transcription_contents c
WHERE NOT EXISTS (
    SELECT 1 FROM call_transcription_revisions r WHERE r.transcription_uuid = c.transcription_uuid
);

-- +goose Down
DROP TABLE IF EXISTS call_transcription_edit_audit;
DROP TABLE IF EXISTS call_transcription_revisions;
DROP TABLE IF EXISTS call_transcription_contents;
