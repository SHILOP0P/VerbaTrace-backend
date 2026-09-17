-- +goose Up
-- Three tables that no line of code reads or writes. They were left behind by
-- ideas that went a different way: chat drafts moved to assistant_scope_drafts,
-- quality-review evidence was never wired up, and enterprise quotes never
-- happened.
DROP TABLE IF EXISTS assistant_chat_drafts;
DROP TABLE IF EXISTS call_quality_review_evidence;
DROP TABLE IF EXISTS enterprise_quotes;

-- +goose Down
CREATE TABLE IF NOT EXISTS assistant_chat_drafts (
    assistant_chat_uuid UUID PRIMARY KEY REFERENCES assistant_chats(assistant_chat_uuid) ON DELETE CASCADE,
    owner_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    draft_text TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS call_quality_review_evidence (
    evidence_uuid UUID PRIMARY KEY,
    revision_uuid UUID NOT NULL REFERENCES call_quality_review_revisions(revision_uuid) ON DELETE CASCADE,
    criterion_uuid UUID NOT NULL REFERENCES call_quality_review_criteria(criterion_uuid) ON DELETE CASCADE,
    quote TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS enterprise_quotes (
    enterprise_quote_uuid UUID PRIMARY KEY,
    owner_company_uuid UUID REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
