-- +goose Up
-- A call imported from a portal belongs to the person who made it, not to the
-- administrator who connected the portal. The uploader is resolved when the
-- call is accepted and stays with the item.
ALTER TABLE ingest_items
    ADD COLUMN uploader_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL;

CREATE INDEX idx_ingest_items_uploader
    ON ingest_items (uploader_user_uuid) WHERE uploader_user_uuid IS NOT NULL;

-- A personal Pro plan may connect a portal to its own calls. It is the way to
-- try the integration before buying a business plan, so no user mapping is
-- involved: everything lands in the key owner's personal calls.
UPDATE plans SET api_access_enabled = true, updated_at = now() WHERE code = 'personal_pro';

-- +goose Down
UPDATE plans SET api_access_enabled = false, updated_at = now() WHERE code = 'personal_pro';
DROP INDEX IF EXISTS idx_ingest_items_uploader;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS uploader_user_uuid;
