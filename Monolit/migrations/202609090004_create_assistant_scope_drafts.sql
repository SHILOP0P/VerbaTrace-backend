-- +goose Up
CREATE TABLE assistant_scope_drafts (
    assistant_scope_draft_uuid UUID PRIMARY KEY,
    owner_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    company_uuid UUID REFERENCES companies(company_uuid) ON DELETE CASCADE,
    assistant_chat_uuid UUID REFERENCES assistant_chats(assistant_chat_uuid) ON DELETE CASCADE,
    text TEXT NOT NULL DEFAULT '' CHECK (length(text) <= 12000),
    context_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT(owner_user_uuid,company_uuid,assistant_chat_uuid)
);

-- +goose Down
DROP TABLE IF EXISTS assistant_scope_drafts;
