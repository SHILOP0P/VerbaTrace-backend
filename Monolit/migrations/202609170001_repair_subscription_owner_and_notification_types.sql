-- +goose Up
-- Migration 202609160004 moved the business plan from the company to its owner,
-- but the administrator's grant kept writing the old shape. Rows created that
-- way are invisible to the application, so move them onto the owner. When the
-- owner already has an active business plan the stray row is canceled instead:
-- one active business plan per person is what the unique index allows.
UPDATE subscriptions s
SET user_uuid = c.manager_user_uuid,
    company_uuid = NULL,
    updated_at = now()
FROM companies c
WHERE s.company_uuid = c.company_uuid
  AND s.type = 'business'
  AND s.status = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM subscriptions other
      WHERE other.type = 'business'
        AND other.status = 'active'
        AND other.user_uuid = c.manager_user_uuid
  );

UPDATE subscriptions
SET status = 'canceled',
    ends_at = COALESCE(ends_at, GREATEST(starts_at + INTERVAL '1 second', now())),
    updated_at = now()
WHERE type = 'business'
  AND status = 'active'
  AND company_uuid IS NOT NULL;

-- The Bitrix24 reconciler writes this type when an external task drifts away
-- from its action. It was never added to the constraint, so the insert failed
-- silently and nobody learned about the conflict.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid','action_status_reverted',
    'support_access_requested','support_access_decided',
    'action_external_sync_requested','action_external_sync_decided',
    'action_external_sync_conflict',
    'invitation_approval_requested','invitation_approval_decided',
    'department_transfer_requested','department_transfer_decided','department_member_moved',
    'company_owner_transfer_requested','company_owner_transfer_decided',
    'company_deputy_assigned','company_deputy_revoked','company_member_removed',
    'analysis_rerun_requested','analysis_rerun_decided'
));

-- Purging a company must be able to finish. The credit ledger is append-only by
-- design — a trigger rejects DELETE on its postings — so a billing account the
-- ledger points at cannot be removed with the company. Let such an account keep
-- its history while losing the company it no longer belongs to.
ALTER TABLE billing_accounts DROP CONSTRAINT IF EXISTS chk_billing_account_owner;
ALTER TABLE billing_accounts ADD CONSTRAINT chk_billing_account_owner CHECK (
    (owner_type = 'user' AND user_uuid IS NOT NULL AND company_uuid IS NULL)
    OR (owner_type = 'company' AND user_uuid IS NULL)
);

-- A developer application is kept for the same reason: metered usage and the
-- integration audit point at it, and both outlive the company.
ALTER TABLE developer_applications DROP CONSTRAINT IF EXISTS chk_developer_application_owner;
ALTER TABLE developer_applications ADD CONSTRAINT chk_developer_application_owner CHECK (
    (owner_type = 'user' AND user_uuid IS NOT NULL AND company_uuid IS NULL)
    OR (owner_type = 'company' AND user_uuid IS NULL)
);

-- +goose Down
ALTER TABLE developer_applications DROP CONSTRAINT IF EXISTS chk_developer_application_owner;
ALTER TABLE developer_applications ADD CONSTRAINT chk_developer_application_owner CHECK (
    (owner_type = 'user' AND user_uuid IS NOT NULL AND company_uuid IS NULL)
    OR (owner_type = 'company' AND company_uuid IS NOT NULL AND user_uuid IS NULL)
);

ALTER TABLE billing_accounts DROP CONSTRAINT IF EXISTS chk_billing_account_owner;
ALTER TABLE billing_accounts ADD CONSTRAINT chk_billing_account_owner CHECK (
    (owner_type = 'user' AND user_uuid IS NOT NULL AND company_uuid IS NULL)
    OR (owner_type = 'company' AND company_uuid IS NOT NULL AND user_uuid IS NULL)
);

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid','action_status_reverted',
    'support_access_requested','support_access_decided',
    'action_external_sync_requested','action_external_sync_decided',
    'invitation_approval_requested','invitation_approval_decided',
    'department_transfer_requested','department_transfer_decided','department_member_moved',
    'company_owner_transfer_requested','company_owner_transfer_decided',
    'company_deputy_assigned','company_deputy_revoked','company_member_removed',
    'analysis_rerun_requested','analysis_rerun_decided'
));

-- Moving the plans back onto companies would guess which company an owner meant,
-- so the data half of this migration is deliberately not reversed.
