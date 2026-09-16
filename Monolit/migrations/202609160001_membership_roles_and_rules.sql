-- +goose Up
-- Suspension is gone from the product: a member is either active or out.
UPDATE company_members SET status = 'left' WHERE status = 'suspended';
UPDATE department_members SET status = 'left' WHERE status = 'suspended';

ALTER TABLE company_members DROP CONSTRAINT chk_company_members_status;
ALTER TABLE company_members ADD CONSTRAINT chk_company_members_status
    CHECK (status IN ('active', 'left'));
ALTER TABLE department_members DROP CONSTRAINT chk_department_members_status;
ALTER TABLE department_members ADD CONSTRAINT chk_department_members_status
    CHECK (status IN ('active', 'left'));

-- A deputy runs one company on behalf of its owner and has the owner's rights
-- except the owner-only actions.
ALTER TABLE company_members DROP CONSTRAINT chk_company_members_role;
ALTER TABLE company_members ADD CONSTRAINT chk_company_members_role
    CHECK (role IN ('company_manager', 'company_deputy', 'employee'));

CREATE UNIQUE INDEX uq_company_members_active_deputy
    ON company_members (company_uuid)
    WHERE role = 'company_deputy' AND status = 'active';

-- An employee belongs to exactly one company. Owners and deputies are exempt.
-- Historical conflicts are resolved in favour of the most recent membership.
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

CREATE UNIQUE INDEX uq_company_members_single_active_employee
    ON company_members (user_uuid)
    WHERE status = 'active' AND role = 'employee';

-- A department membership cannot outlive the company membership it belongs to.
UPDATE department_members dm
SET status = 'left'
WHERE dm.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM company_members cm
      WHERE cm.company_uuid = dm.company_uuid
        AND cm.user_uuid = dm.user_uuid
        AND cm.status = 'active'
  );

-- Invitations a user never wants to receive.
ALTER TABLE user_preferences
    ADD COLUMN invitations_muted BOOLEAN NOT NULL DEFAULT false;

-- Exclusion by an owner or deputy must not be undone by a department leader
-- silently. The restriction expires so the list cannot grow forever.
CREATE TABLE company_membership_restrictions (
    restriction_uuid UUID PRIMARY KEY,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind = 'excluded_by_manager'),
    reason TEXT,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_company_membership_restriction_period CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX uq_company_membership_restriction
    ON company_membership_restrictions (company_uuid, user_uuid);

-- A leader invite for a restricted user waits for the deputy's approval and
-- must not be visible to the invited user until then.
ALTER TABLE membership_invitations
    ADD COLUMN approval_status TEXT NOT NULL DEFAULT 'not_required',
    ADD COLUMN approval_decided_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    ADD COLUMN approval_decided_at TIMESTAMPTZ,
    ADD CONSTRAINT chk_membership_invitations_approval
        CHECK (approval_status IN ('not_required', 'pending', 'approved', 'rejected'));

-- A leader cannot take people from another department directly; the move is
-- requested and performed by the deputy.
CREATE TABLE department_transfer_requests (
    request_uuid UUID PRIMARY KEY,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    from_department_uuid UUID REFERENCES departments(department_uuid) ON DELETE SET NULL,
    to_department_uuid UUID NOT NULL REFERENCES departments(department_uuid) ON DELETE CASCADE,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    reason TEXT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'canceled', 'expired')),
    decided_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    decided_at TIMESTAMPTZ,
    decision_comment TEXT,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_department_transfer_departments CHECK (from_department_uuid IS DISTINCT FROM to_department_uuid),
    CONSTRAINT chk_department_transfer_decision CHECK (
        (status = 'pending' AND decided_by_user_uuid IS NULL AND decided_at IS NULL)
        OR (status <> 'pending')
    )
);

CREATE UNIQUE INDEX uq_department_transfer_pending
    ON department_transfer_requests (company_uuid, user_uuid)
    WHERE status = 'pending';

-- Ownership changes hands only when the new owner accepts it.
CREATE TABLE company_ownership_transfers (
    transfer_uuid UUID PRIMARY KEY,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    from_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    to_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'declined', 'canceled', 'expired')),
    reason TEXT,
    decided_at TIMESTAMPTZ,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_company_ownership_transfer_users CHECK (from_user_uuid <> to_user_uuid)
);

CREATE UNIQUE INDEX uq_company_ownership_transfer_pending
    ON company_ownership_transfers (company_uuid)
    WHERE status = 'pending';

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid',
    'support_access_requested','support_access_decided',
    'action_external_sync_requested','action_external_sync_decided',
    'invitation_approval_requested','invitation_approval_decided',
    'department_transfer_requested','department_transfer_decided','department_member_moved',
    'company_owner_transfer_requested','company_owner_transfer_decided',
    'company_deputy_assigned','company_deputy_revoked','company_member_removed'
));

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid',
    'support_access_requested','support_access_decided',
    'action_external_sync_requested','action_external_sync_decided'
));

DROP TABLE company_ownership_transfers;
DROP TABLE department_transfer_requests;

ALTER TABLE membership_invitations
    DROP CONSTRAINT chk_membership_invitations_approval,
    DROP COLUMN approval_decided_at,
    DROP COLUMN approval_decided_by_user_uuid,
    DROP COLUMN approval_status;

DROP TABLE company_membership_restrictions;

ALTER TABLE user_preferences DROP COLUMN invitations_muted;

DROP INDEX uq_company_members_single_active_employee;
DROP INDEX uq_company_members_active_deputy;

ALTER TABLE company_members DROP CONSTRAINT chk_company_members_role;
ALTER TABLE company_members ADD CONSTRAINT chk_company_members_role
    CHECK (role IN ('company_manager', 'employee'));

ALTER TABLE department_members DROP CONSTRAINT chk_department_members_status;
ALTER TABLE department_members ADD CONSTRAINT chk_department_members_status
    CHECK (status IN ('active', 'suspended', 'left'));
ALTER TABLE company_members DROP CONSTRAINT chk_company_members_status;
ALTER TABLE company_members ADD CONSTRAINT chk_company_members_status
    CHECK (status IN ('active', 'suspended', 'left'));
