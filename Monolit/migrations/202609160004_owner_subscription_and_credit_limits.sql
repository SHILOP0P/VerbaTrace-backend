-- +goose Up
-- A business subscription is bought by a person, not by a company: one plan
-- covers one company on the lower tiers and three on the top one, and the
-- credits are shared across them.
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS chk_subscriptions_owner;
ALTER TABLE subscriptions ADD CONSTRAINT chk_subscriptions_owner CHECK (
    (user_uuid IS NOT NULL AND company_uuid IS NULL)
    OR (user_uuid IS NULL AND company_uuid IS NOT NULL)
);

-- A company lives through states rather than disappearing at once: it freezes
-- when it is no longer covered, then waits out a soft deletion, then is purged.
ALTER TABLE companies
    ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN frozen_at TIMESTAMPTZ,
    ADD COLUMN soft_deleted_at TIMESTAMPTZ,
    ADD COLUMN purge_after TIMESTAMPTZ,
    ADD COLUMN restore_used BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT chk_companies_lifecycle
        CHECK (lifecycle_state IN ('active','frozen','soft_deleted'));

CREATE INDEX idx_companies_lifecycle ON companies (lifecycle_state, purge_after);

-- Existing per-company subscriptions move to the owner of the company. When an
-- owner had several, the most generous plan wins and the rest are canceled.
WITH ranked AS (
    SELECT s.subscription_uuid,
           c.manager_user_uuid,
           row_number() OVER (
               PARTITION BY c.manager_user_uuid
               ORDER BY p.monthly_credit_allowance DESC, s.starts_at
           ) AS position
    FROM subscriptions s
    JOIN companies c ON c.company_uuid = s.company_uuid
    JOIN plans p ON p.plan_uuid = s.plan_uuid
    WHERE s.type = 'business' AND s.status = 'active'
)
UPDATE subscriptions s
SET user_uuid = ranked.manager_user_uuid,
    company_uuid = NULL,
    status = CASE WHEN ranked.position = 1 THEN 'active' ELSE 'canceled' END,
    updated_at = now()
FROM ranked
WHERE s.subscription_uuid = ranked.subscription_uuid
  -- An owner who already has a personal business subscription keeps it.
  AND NOT EXISTS (
      SELECT 1 FROM subscriptions other
      WHERE other.type = 'business'
        AND other.status = 'active'
        AND other.user_uuid = ranked.manager_user_uuid
  );

-- Companies beyond what the owner's plan covers start out frozen: the owner
-- chooses which ones to keep working.
WITH covered AS (
    SELECT c.company_uuid,
           row_number() OVER (PARTITION BY c.manager_user_uuid ORDER BY c.created_at) AS position,
           COALESCE(p.company_limit, 1) AS company_limit
    FROM companies c
    LEFT JOIN subscriptions s ON s.user_uuid = c.manager_user_uuid AND s.type = 'business' AND s.status = 'active'
    LEFT JOIN plans p ON p.plan_uuid = s.plan_uuid
    WHERE c.deleted_at IS NULL
)
UPDATE companies c
SET lifecycle_state = 'frozen', frozen_at = now()
FROM covered
WHERE c.company_uuid = covered.company_uuid
  AND covered.position > covered.company_limit;

-- The owner caps what each company may spend, the deputy splits that cap
-- between departments. NULL means no cap of its own, zero forbids spending.
CREATE TABLE company_credit_limits (
    company_uuid UUID PRIMARY KEY REFERENCES companies(company_uuid) ON DELETE CASCADE,
    limit_credits BIGINT NULL CHECK (limit_credits IS NULL OR limit_credits >= 0),
    updated_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE department_credit_limits (
    department_uuid UUID PRIMARY KEY REFERENCES departments(department_uuid) ON DELETE CASCADE,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    limit_credits BIGINT NULL CHECK (limit_credits IS NULL OR limit_credits >= 0),
    updated_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_department_credit_limits_company ON department_credit_limits (company_uuid);

-- Spending is attributed to the company and the department that caused it, so
-- a limit can be compared against real consumption and a forecast can be drawn
-- from the pace of the current period.
ALTER TABLE usage_operations
    ADD COLUMN company_uuid UUID REFERENCES companies(company_uuid) ON DELETE SET NULL,
    ADD COLUMN department_uuid UUID REFERENCES departments(department_uuid) ON DELETE SET NULL;

UPDATE usage_operations o
SET company_uuid = c.company_uuid,
    department_uuid = c.department_uuid
FROM calls c
WHERE o.call_uuid = c.call_uuid;

CREATE INDEX idx_usage_operations_company_period
    ON usage_operations (company_uuid, completed_at DESC) WHERE company_uuid IS NOT NULL;
CREATE INDEX idx_usage_operations_department_period
    ON usage_operations (department_uuid, completed_at DESC) WHERE department_uuid IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_usage_operations_department_period;
DROP INDEX IF EXISTS idx_usage_operations_company_period;
ALTER TABLE usage_operations
    DROP COLUMN IF EXISTS department_uuid,
    DROP COLUMN IF EXISTS company_uuid;
DROP TABLE IF EXISTS department_credit_limits;
DROP TABLE IF EXISTS company_credit_limits;

DROP INDEX IF EXISTS idx_companies_lifecycle;
ALTER TABLE companies
    DROP CONSTRAINT IF EXISTS chk_companies_lifecycle,
    DROP COLUMN IF EXISTS restore_used,
    DROP COLUMN IF EXISTS purge_after,
    DROP COLUMN IF EXISTS soft_deleted_at,
    DROP COLUMN IF EXISTS frozen_at,
    DROP COLUMN IF EXISTS lifecycle_state;

ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS chk_subscriptions_owner;
ALTER TABLE subscriptions ADD CONSTRAINT chk_subscriptions_owner CHECK (
    (user_uuid IS NOT NULL AND company_uuid IS NULL)
    OR (user_uuid IS NULL AND company_uuid IS NOT NULL)
);
