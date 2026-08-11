-- +goose Up
CREATE TABLE call_quality_reviews (
    review_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    analysis_uuid UUID NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
    analysis_attempt_uuid UUID NULL REFERENCES call_analysis_attempts(analysis_attempt_uuid) ON DELETE SET NULL,
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    department_uuid UUID NULL REFERENCES departments(department_uuid) ON DELETE SET NULL,
    reviewed_subject_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    assignee_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    status VARCHAR(24) NOT NULL CHECK (status IN ('unassigned','assigned','in_review','published','appealed','resolved','canceled')),
    active_revision_uuid UUID NULL,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    due_at TIMESTAMPTZ NULL,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX uq_call_quality_reviews_active_analysis
    ON call_quality_reviews (analysis_uuid) WHERE status <> 'canceled';
CREATE INDEX idx_call_quality_reviews_company_queue
    ON call_quality_reviews (company_uuid, status, updated_at DESC, review_uuid);
CREATE INDEX idx_call_quality_reviews_department_queue
    ON call_quality_reviews (department_uuid, status, updated_at DESC, review_uuid);
CREATE INDEX idx_call_quality_reviews_assignee_queue
    ON call_quality_reviews (assignee_user_uuid, status, updated_at DESC, review_uuid);

CREATE TABLE call_quality_review_revisions (
    revision_uuid UUID PRIMARY KEY,
    review_uuid UUID NOT NULL REFERENCES call_quality_reviews(review_uuid) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL CHECK (revision_number > 0),
    base_revision_uuid UUID NULL REFERENCES call_quality_review_revisions(revision_uuid),
    author_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    status VARCHAR(16) NOT NULL CHECK (status IN ('draft','published','superseded','voided')),
    overall_comment TEXT NULL,
    human_score NUMERIC NULL,
    score_max NUMERIC NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ NULL,
    UNIQUE (review_uuid, revision_number)
);

CREATE UNIQUE INDEX uq_call_quality_review_author_draft
    ON call_quality_review_revisions (review_uuid, author_user_uuid) WHERE status = 'draft';
CREATE INDEX idx_call_quality_review_revisions_review
    ON call_quality_review_revisions (review_uuid, revision_number DESC);

ALTER TABLE call_quality_reviews
    ADD CONSTRAINT fk_call_quality_reviews_active_revision
    FOREIGN KEY (active_revision_uuid) REFERENCES call_quality_review_revisions(revision_uuid);

CREATE TABLE call_quality_review_criteria (
    criterion_uuid UUID PRIMARY KEY,
    revision_uuid UUID NOT NULL REFERENCES call_quality_review_revisions(revision_uuid) ON DELETE CASCADE,
    criterion_key VARCHAR(160) NOT NULL,
    title_snapshot TEXT NOT NULL,
    ai_score NUMERIC NULL,
    human_score NUMERIC NULL,
    score_min NUMERIC NULL,
    score_max NUMERIC NULL,
    weight NUMERIC NOT NULL DEFAULT 1 CHECK (weight >= 0),
    decision VARCHAR(20) NOT NULL CHECK (decision IN ('confirmed','overridden','not_applicable','unscored')),
    comment TEXT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    UNIQUE (revision_uuid, criterion_key)
);

CREATE TABLE call_quality_review_evidence (
    evidence_uuid UUID PRIMARY KEY,
    revision_uuid UUID NOT NULL REFERENCES call_quality_review_revisions(revision_uuid) ON DELETE CASCADE,
    criterion_uuid UUID NULL REFERENCES call_quality_review_criteria(criterion_uuid) ON DELETE CASCADE,
    field_key VARCHAR(160) NULL,
    quote_snapshot TEXT NOT NULL,
    speaker_snapshot TEXT NULL,
    word_start_index INTEGER NOT NULL CHECK (word_start_index >= 0),
    word_end_index INTEGER NOT NULL CHECK (word_end_index >= word_start_index),
    start_seconds NUMERIC NOT NULL CHECK (start_seconds >= 0),
    end_seconds NUMERIC NOT NULL CHECK (end_seconds >= start_seconds),
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE call_quality_review_appeals (
    appeal_uuid UUID PRIMARY KEY,
    review_uuid UUID NOT NULL REFERENCES call_quality_reviews(review_uuid) ON DELETE CASCADE,
    revision_uuid UUID NOT NULL REFERENCES call_quality_review_revisions(revision_uuid),
    author_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    status VARCHAR(24) NOT NULL CHECK (status IN ('open','in_review','accepted','partially_accepted','rejected','withdrawn')),
    reason TEXT NOT NULL,
    resolution_comment TEXT NULL,
    resolved_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ NULL,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0)
);

CREATE UNIQUE INDEX uq_call_quality_review_open_appeal
    ON call_quality_review_appeals (revision_uuid, author_user_uuid)
    WHERE status IN ('open','in_review');

CREATE TABLE call_quality_review_events (
    event_uuid UUID PRIMARY KEY,
    review_uuid UUID NOT NULL REFERENCES call_quality_reviews(review_uuid) ON DELETE CASCADE,
    revision_uuid UUID NULL REFERENCES call_quality_review_revisions(revision_uuid),
    appeal_uuid UUID NULL REFERENCES call_quality_review_appeals(appeal_uuid),
    actor_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    actor_company_role_snapshot VARCHAR(32) NULL,
    actor_department_role_snapshot VARCHAR(32) NULL,
    event_type VARCHAR(40) NOT NULL,
    before_json JSONB NULL,
    after_json JSONB NULL,
    reason TEXT NULL,
    request_id VARCHAR(160) NULL,
    ip_address INET NULL,
    user_agent TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_call_quality_review_events_review
    ON call_quality_review_events (review_uuid, created_at DESC, event_uuid);

-- +goose Down
DROP TABLE IF EXISTS call_quality_review_events;
DROP TABLE IF EXISTS call_quality_review_appeals;
DROP TABLE IF EXISTS call_quality_review_evidence;
DROP TABLE IF EXISTS call_quality_review_criteria;
ALTER TABLE call_quality_reviews DROP CONSTRAINT IF EXISTS fk_call_quality_reviews_active_revision;
DROP TABLE IF EXISTS call_quality_review_revisions;
DROP TABLE IF EXISTS call_quality_reviews;
