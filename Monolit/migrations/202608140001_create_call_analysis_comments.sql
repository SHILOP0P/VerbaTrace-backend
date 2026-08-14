-- +goose Up
CREATE TABLE call_analysis_comments (
    comment_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    analysis_uuid UUID NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
    author_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at TIMESTAMPTZ NULL,
    lock_version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_call_analysis_comment_body CHECK (char_length(btrim(body)) BETWEEN 1 AND 4000),
    CONSTRAINT chk_call_analysis_comment_version CHECK (lock_version > 0)
);
CREATE INDEX idx_call_analysis_comments_analysis_created ON call_analysis_comments(analysis_uuid, created_at, comment_uuid);

CREATE TABLE call_analysis_comment_revisions (
    revision_uuid UUID PRIMARY KEY,
    comment_uuid UUID NOT NULL REFERENCES call_analysis_comments(comment_uuid) ON DELETE CASCADE,
    editor_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    previous_body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_call_analysis_comment_revisions_comment ON call_analysis_comment_revisions(comment_uuid, created_at);

-- +goose Down
DROP TABLE IF EXISTS call_analysis_comment_revisions;
DROP TABLE IF EXISTS call_analysis_comments;
