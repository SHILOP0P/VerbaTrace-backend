-- +goose Up
CREATE TABLE user_profiles (
    user_uuid UUID PRIMARY KEY REFERENCES users(user_uuid) ON DELETE CASCADE,
    full_name TEXT NOT NULL,
    full_surname TEXT NOT NULL,
    username TEXT NOT NULL,
    headline TEXT NULL,
    phone TEXT NULL,
    timezone TEXT NULL,
    avatar_path TEXT NULL,
    avatar_mime_type TEXT NULL,
    avatar_size_bytes BIGINT NULL,
    avatar_updated_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_user_profiles_full_name_not_blank CHECK (btrim(full_name) <> ''),
    CONSTRAINT chk_user_profiles_full_surname_not_blank CHECK (btrim(full_surname) <> ''),
    CONSTRAINT chk_user_profiles_username_not_blank CHECK (btrim(username) <> ''),
    CONSTRAINT chk_user_profiles_avatar_metadata CHECK (
        (
            avatar_path IS NULL
            AND avatar_mime_type IS NULL
            AND avatar_size_bytes IS NULL
            AND avatar_updated_at IS NULL
        )
        OR
        (
            avatar_path IS NOT NULL
            AND avatar_mime_type IS NOT NULL
            AND avatar_size_bytes IS NOT NULL
            AND avatar_size_bytes > 0
            AND avatar_updated_at IS NOT NULL
        )
    )
);

INSERT INTO user_profiles (
    user_uuid,
    full_name,
    full_surname,
    username,
    headline,
    phone,
    timezone,
    avatar_path,
    avatar_mime_type,
    avatar_size_bytes,
    avatar_updated_at
)
SELECT
    user_uuid,
    full_name,
    full_surname,
    username,
    post,
    phone,
    timezone,
    avatar_path,
    avatar_mime_type,
    avatar_size_bytes,
    avatar_updated_at
FROM users;

-- +goose StatementBegin
DO $$
BEGIN
    IF (SELECT count(*) FROM users) <> (SELECT count(*) FROM user_profiles) THEN
        RAISE EXCEPTION 'user profile backfill count mismatch';
    END IF;
END $$;
-- +goose StatementEnd

CREATE UNIQUE INDEX idx_user_profiles_username_lower
    ON user_profiles (lower(username));

ALTER TABLE users
    ALTER COLUMN full_name DROP NOT NULL,
    ALTER COLUMN full_surname DROP NOT NULL,
    ALTER COLUMN username DROP NOT NULL;

ALTER TABLE company_members
    ADD COLUMN job_title TEXT NULL,
    ADD CONSTRAINT chk_company_members_job_title_not_blank
        CHECK (job_title IS NULL OR btrim(job_title) <> '');

-- +goose Down
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

ALTER TABLE company_members
    DROP CONSTRAINT IF EXISTS chk_company_members_job_title_not_blank,
    DROP COLUMN IF EXISTS job_title;

DROP INDEX IF EXISTS idx_user_profiles_username_lower;
DROP TABLE IF EXISTS user_profiles;

ALTER TABLE users
    ALTER COLUMN full_name SET NOT NULL,
    ALTER COLUMN full_surname SET NOT NULL,
    ALTER COLUMN username SET NOT NULL;
