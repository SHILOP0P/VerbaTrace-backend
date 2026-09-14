-- +goose Up
-- Repair installations where the integration/support migration had already
-- been recorded before its later columns were added to the migration file.
ALTER TABLE bitrix_call_candidates
    ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 8;

ALTER TABLE support_access_events
    ADD COLUMN IF NOT EXISTS actor_type TEXT;

UPDATE support_access_events
SET actor_type = 'user'
WHERE actor_type IS NULL;

ALTER TABLE support_access_events
    ALTER COLUMN actor_type SET DEFAULT 'user',
    ALTER COLUMN actor_type SET NOT NULL,
    ALTER COLUMN actor_user_uuid DROP NOT NULL;

ALTER TABLE support_access_events
    DROP CONSTRAINT IF EXISTS chk_support_access_events_actor;
ALTER TABLE support_access_events
    ADD CONSTRAINT chk_support_access_events_actor
    CHECK (
        actor_type IN ('user', 'service_account', 'system', 'support_grant')
        AND ((actor_type = 'system' AND actor_user_uuid IS NULL)
          OR (actor_type <> 'system' AND actor_user_uuid IS NOT NULL))
    );

-- +goose Down
ALTER TABLE support_access_events DROP CONSTRAINT IF EXISTS chk_support_access_events_actor;
ALTER TABLE support_access_events ALTER COLUMN actor_user_uuid SET NOT NULL;
ALTER TABLE support_access_events DROP COLUMN IF EXISTS actor_type;
ALTER TABLE bitrix_call_candidates
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS attempts;
