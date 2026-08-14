-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ensure_personal_start_subscription()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    start_plan_uuid UUID;
BEGIN
    SELECT plan_uuid
      INTO start_plan_uuid
     FROM plans
     WHERE code = 'personal_start'
       AND type = 'personal'
     LIMIT 1;

    IF start_plan_uuid IS NULL THEN
        RAISE EXCEPTION 'active personal_start plan is missing';
    END IF;

    INSERT INTO subscriptions (
        subscription_uuid,
        plan_uuid,
        type,
        user_uuid,
        status,
        starts_at
    ) VALUES (
        gen_random_uuid(),
        start_plan_uuid,
        'personal',
        NEW.user_uuid,
        'active',
        now()
    );

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS users_default_personal_start_subscription ON users;
CREATE TRIGGER users_default_personal_start_subscription
AFTER INSERT ON users
FOR EACH ROW
EXECUTE FUNCTION ensure_personal_start_subscription();

INSERT INTO subscriptions (
    subscription_uuid,
    plan_uuid,
    type,
    user_uuid,
    status,
    starts_at
)
SELECT
    gen_random_uuid(),
    p.plan_uuid,
    'personal',
    u.user_uuid,
    'active',
    now()
FROM users u
JOIN plans p
  ON p.code = 'personal_start'
 AND p.type = 'personal'
WHERE NOT EXISTS (
    SELECT 1
      FROM subscriptions s
     WHERE s.user_uuid = u.user_uuid
       AND s.type = 'personal'
       AND s.status = 'active'
       AND s.starts_at <= now()
       AND (s.ends_at IS NULL OR s.ends_at > now())
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS users_default_personal_start_subscription ON users;
DROP FUNCTION IF EXISTS ensure_personal_start_subscription();
-- Existing subscriptions are retained to avoid deleting user billing history.
-- +goose StatementEnd
