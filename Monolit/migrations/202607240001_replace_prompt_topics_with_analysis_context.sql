-- +goose Up
DROP TABLE IF EXISTS call_prompt_contexts;
DROP TABLE IF EXISTS prompt_user_topics;
DROP TABLE IF EXISTS prompt_user_industries;
DROP TABLE IF EXISTS prompt_profile_topics;
DROP TABLE IF EXISTS prompt_profiles;
DROP TABLE IF EXISTS prompt_topic_aliases;
DROP TABLE IF EXISTS prompt_topics;
DROP TABLE IF EXISTS prompt_industries;

CREATE TABLE analysis_personalizations (
    scope TEXT NOT NULL CHECK (scope IN ('personal', 'company', 'department')),
    owner_uuid UUID NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, owner_uuid),
    CHECK (char_length(content) <= 6000)
);

CREATE TABLE call_folder_instructions (
    folder_uuid UUID NOT NULL REFERENCES call_folders(folder_uuid) ON DELETE CASCADE,
    instruction_uuid UUID NOT NULL REFERENCES analysis_instructions(instruction_uuid) ON DELETE CASCADE,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_uuid, instruction_uuid)
);

CREATE INDEX idx_call_folder_instructions_instruction
    ON call_folder_instructions(instruction_uuid);

-- +goose Down
DROP TABLE IF EXISTS call_folder_instructions;
DROP TABLE IF EXISTS analysis_personalizations;
