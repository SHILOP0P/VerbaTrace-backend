-- +goose Up
-- Delivery of digests and alerts. The bell is a real channel today; mail and
-- Telegram have their tables and queue but no sender beyond the mock one.
CREATE TABLE notification_channels (
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('email','telegram')),
    -- email or telegram chat id
    address TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','verified','disabled')),
    verify_token_hash TEXT,
    verify_expires_at TIMESTAMPTZ,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uuid, channel)
);

-- No row means the event is on in the app and off elsewhere.
CREATE TABLE notification_subscriptions (
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('weekly_digest','critical_call_alert')),
    channel TEXT NOT NULL CHECK (channel IN ('in_app','email','telegram')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (user_uuid, kind, channel)
);

CREATE TABLE outbound_messages (
    message_uuid UUID PRIMARY KEY,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    kind TEXT NOT NULL,
    dedupe_key TEXT NOT NULL UNIQUE,
    payload_json JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','sending','sent','failed','dead')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ
);
CREATE INDEX idx_outbound_messages_due ON outbound_messages (available_at) WHERE status IN ('pending','failed');

-- One row per event that was delivered, whatever the channels: a digest of a
-- week, an alert about a call. It keeps a second run from repeating them.
CREATE TABLE delivery_marks (
    dedupe_key TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
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

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
DELETE FROM notifications WHERE type IN ('critical_call_alert','weekly_digest_ready');
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
    'call_subject_marked','call_subjects_changed'
));
DROP TABLE IF EXISTS delivery_marks;
DROP TABLE IF EXISTS outbound_messages;
DROP TABLE IF EXISTS notification_subscriptions;
DROP TABLE IF EXISTS notification_channels;
