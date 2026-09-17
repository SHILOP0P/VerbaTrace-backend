-- +goose Up
-- A person may now work in several companies at once. The rule that they could
-- not was the product's founding assumption, and it was enforced here; the
-- owner reversed it, so the guarantee goes and everything built on top of it —
-- the transfer alert, the leave-the-previous-company step — goes with it.
--
-- Two guarantees stay, because they are about a single company rather than
-- about the person: one active department per company, and at most one active
-- deputy per company.
DROP INDEX IF EXISTS uq_company_members_single_active_employee;

-- A deputy no longer has to be an employee first, so an invitation can name the
-- deputy seat directly. Without this the role would be rejected by the CHECK the
-- moment the invitation is written.
ALTER TABLE membership_invitations
    DROP CONSTRAINT IF EXISTS membership_invitations_company_role_check;
ALTER TABLE membership_invitations
    ADD CONSTRAINT membership_invitations_company_role_check
    CHECK (company_role IN ('employee', 'company_deputy'));

-- +goose Down
-- Going back means one company per person again, so the conflicts this
-- migration allowed have to be resolved first: the most recent membership wins,
-- exactly as the migration that introduced the index decided.
UPDATE membership_invitations
SET status = 'canceled', updated_at = now()
WHERE company_role = 'company_deputy' AND status = 'pending';

ALTER TABLE membership_invitations
    DROP CONSTRAINT IF EXISTS membership_invitations_company_role_check;
ALTER TABLE membership_invitations
    ADD CONSTRAINT membership_invitations_company_role_check
    CHECK (company_role IN ('employee'));

UPDATE company_members cm
SET status = 'left'
WHERE cm.status = 'active'
  AND cm.role = 'employee'
  AND EXISTS (
      SELECT 1
      FROM company_members later
      WHERE later.user_uuid = cm.user_uuid
        AND later.status = 'active'
        AND later.role = 'employee'
        AND (later.created_at, later.company_uuid) > (cm.created_at, cm.company_uuid)
  );

UPDATE department_members dm
SET status = 'left'
WHERE dm.status = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM company_members cm
      WHERE cm.company_uuid = dm.company_uuid
        AND cm.user_uuid = dm.user_uuid
        AND cm.status = 'active'
  );

CREATE UNIQUE INDEX IF NOT EXISTS uq_company_members_single_active_employee
    ON company_members (user_uuid)
    WHERE status = 'active' AND role = 'employee';
