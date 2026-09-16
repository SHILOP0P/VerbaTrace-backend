-- +goose Up
-- A deleted call waits 30 days in the bin: an accidental deletion must be
-- recoverable, and only after that the data and files go away for good.
ALTER TABLE calls
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN deleted_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    ADD COLUMN purge_after TIMESTAMPTZ,
    ADD CONSTRAINT chk_calls_soft_delete CHECK ((deleted_at IS NULL) = (purge_after IS NULL));

CREATE INDEX idx_calls_deleted ON calls (purge_after) WHERE deleted_at IS NOT NULL;

-- Folder grants are gone: a folder is visible to the people who already see the
-- calls inside it, so a separate access list only created confusion.
DROP TABLE IF EXISTS call_folder_accesses;

-- +goose Down
CREATE TABLE call_folder_accesses (
    folder_uuid UUID NOT NULL REFERENCES call_folders(folder_uuid) ON DELETE CASCADE,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    granted_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_uuid, user_uuid)
);

DROP INDEX IF EXISTS idx_calls_deleted;

ALTER TABLE calls
    DROP CONSTRAINT IF EXISTS chk_calls_soft_delete,
    DROP COLUMN IF EXISTS purge_after,
    DROP COLUMN IF EXISTS deleted_by_user_uuid,
    DROP COLUMN IF EXISTS deleted_at;
