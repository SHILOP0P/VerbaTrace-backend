-- +goose Up
ALTER TABLE call_actions
    ALTER COLUMN company_uuid DROP NOT NULL,
    ALTER COLUMN source_department_uuid DROP NOT NULL,
    ALTER COLUMN target_department_uuid DROP NOT NULL;

ALTER TABLE call_action_dispositions
    ALTER COLUMN company_uuid DROP NOT NULL;

ALTER TABLE call_actions ADD CONSTRAINT call_actions_scope_check CHECK (
    (company_uuid IS NULL AND source_department_uuid IS NULL AND target_department_uuid IS NULL)
    OR
    (company_uuid IS NOT NULL AND source_department_uuid IS NOT NULL AND target_department_uuid IS NOT NULL)
);

-- +goose Down
DELETE FROM call_actions WHERE company_uuid IS NULL;
DELETE FROM call_action_dispositions WHERE company_uuid IS NULL;
ALTER TABLE call_actions DROP CONSTRAINT IF EXISTS call_actions_scope_check;
ALTER TABLE call_actions
    ALTER COLUMN company_uuid SET NOT NULL,
    ALTER COLUMN source_department_uuid SET NOT NULL,
    ALTER COLUMN target_department_uuid SET NOT NULL;
ALTER TABLE call_action_dispositions ALTER COLUMN company_uuid SET NOT NULL;
