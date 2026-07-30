-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users u
        LEFT JOIN user_profiles p ON p.user_uuid = u.user_uuid
        WHERE p.user_uuid IS NULL
    ) THEN
        RAISE EXCEPTION 'user without profile';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM user_profiles p
        LEFT JOIN users u ON u.user_uuid = p.user_uuid
        WHERE u.user_uuid IS NULL
    ) THEN
        RAISE EXCEPTION 'profile without user';
    END IF;
END $$;
-- +goose StatementEnd

DROP INDEX IF EXISTS idx_users_username_lower;

ALTER TABLE users
    DROP COLUMN full_name,
    DROP COLUMN full_surname,
    DROP COLUMN username,
    DROP COLUMN post,
    DROP COLUMN phone,
    DROP COLUMN timezone,
    DROP COLUMN avatar_path,
    DROP COLUMN avatar_mime_type,
    DROP COLUMN avatar_size_bytes,
    DROP COLUMN avatar_updated_at;

-- +goose Down
ALTER TABLE users
    ADD COLUMN full_name TEXT NULL,
    ADD COLUMN full_surname TEXT NULL,
    ADD COLUMN username TEXT NULL,
    ADD COLUMN post TEXT NULL,
    ADD COLUMN phone TEXT NULL,
    ADD COLUMN timezone TEXT NULL,
    ADD COLUMN avatar_path TEXT NULL,
    ADD COLUMN avatar_mime_type TEXT NULL,
    ADD COLUMN avatar_size_bytes BIGINT NULL,
    ADD COLUMN avatar_updated_at TIMESTAMPTZ NULL;

UPDATE users u
SET full_name = p.full_name,
    full_surname = p.full_surname,
    username = p.username,
    post = p.headline,
    phone = p.phone,
    timezone = p.timezone,
    avatar_path = p.avatar_path,
    avatar_mime_type = p.avatar_mime_type,
    avatar_size_bytes = p.avatar_size_bytes,
    avatar_updated_at = p.avatar_updated_at
FROM user_profiles p
WHERE p.user_uuid = u.user_uuid;

ALTER TABLE users
    ALTER COLUMN full_name SET NOT NULL,
    ALTER COLUMN full_surname SET NOT NULL,
    ALTER COLUMN username SET NOT NULL;

CREATE UNIQUE INDEX idx_users_username_lower
    ON users (lower(username));
