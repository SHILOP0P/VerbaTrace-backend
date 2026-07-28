-- +goose Up
CREATE TABLE user_contacts (
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    contact_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_uuid, contact_user_uuid),
    CONSTRAINT user_contacts_not_self CHECK (user_uuid <> contact_user_uuid)
);

CREATE INDEX user_contacts_by_user_created_at_idx ON user_contacts (user_uuid, created_at DESC);

CREATE TABLE user_favorite_calls (
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_uuid, call_uuid)
);

CREATE INDEX user_favorite_calls_by_user_created_at_idx ON user_favorite_calls (user_uuid, created_at DESC);

-- +goose Down
DROP TABLE user_favorite_calls;
DROP TABLE user_contacts;
