-- +goose Up
CREATE TABLE call_diarization_roles (
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position SMALLINT NOT NULL,
    PRIMARY KEY (call_uuid, position)
);

-- +goose Down
DROP TABLE call_diarization_roles;
