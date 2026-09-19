-- +goose Up
CREATE TABLE password_reset_tokens (
    -- SHA-256 of a 32-byte random token; the token itself is never stored
    token_hash TEXT PRIMARY KEY,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    -- 30 minutes after the request
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    requested_ip TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_password_reset_tokens_user ON password_reset_tokens (user_uuid) WHERE used_at IS NULL;

ALTER TABLE auth_rate_counters DROP CONSTRAINT IF EXISTS auth_rate_counters_scope_check;
ALTER TABLE auth_rate_counters ADD CONSTRAINT auth_rate_counters_scope_check
    CHECK (scope IN ('login_account','login_ip','signup_ip','reset_account','reset_ip'));

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
    'analysis_rerun_requested','analysis_rerun_decided',
    'scorecard_failed','scorecard_review_needed',
    'call_subject_marked','call_subjects_changed',
    'critical_call_alert','weekly_digest_ready',
    'password_changed'
));

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
DELETE FROM notifications WHERE type = 'password_changed';
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
    'analysis_rerun_requested','analysis_rerun_decided',
    'scorecard_failed','scorecard_review_needed',
    'call_subject_marked','call_subjects_changed',
    'critical_call_alert','weekly_digest_ready'
));
DELETE FROM auth_rate_counters WHERE scope IN ('reset_account','reset_ip');
ALTER TABLE auth_rate_counters DROP CONSTRAINT IF EXISTS auth_rate_counters_scope_check;
ALTER TABLE auth_rate_counters ADD CONSTRAINT auth_rate_counters_scope_check
    CHECK (scope IN ('login_account','login_ip','signup_ip'));
DROP TABLE IF EXISTS password_reset_tokens;
