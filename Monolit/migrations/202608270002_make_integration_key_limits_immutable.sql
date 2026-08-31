-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION reject_integration_key_limit_update() RETURNS trigger AS $$
BEGIN
    IF OLD.permanent_credit_limit IS DISTINCT FROM NEW.permanent_credit_limit
       OR OLD.temporary_credit_limit IS DISTINCT FROM NEW.temporary_credit_limit
       OR OLD.temporary_limit_starts_at IS DISTINCT FROM NEW.temporary_limit_starts_at
       OR OLD.temporary_limit_ends_at IS DISTINCT FROM NEW.temporary_limit_ends_at THEN
        RAISE EXCEPTION 'integration key limits are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_integration_key_limits_immutable
BEFORE UPDATE ON integration_api_keys
FOR EACH ROW EXECUTE FUNCTION reject_integration_key_limit_update();

-- +goose Down
DROP TRIGGER IF EXISTS trg_integration_key_limits_immutable ON integration_api_keys;
DROP FUNCTION IF EXISTS reject_integration_key_limit_update();
