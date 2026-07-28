-- +goose Up
CREATE TABLE call_speaker_hints (
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    participant_name TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('self', 'manager', 'client', 'other')),
    note TEXT NOT NULL DEFAULT '',
    position SMALLINT NOT NULL,
    PRIMARY KEY (call_uuid, user_uuid)
);

-- +goose Down
DROP TABLE call_speaker_hints;
