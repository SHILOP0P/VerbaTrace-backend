-- +goose Up
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'invitation','report_ready','subscription','processing_failed',
    'action_assigned','action_reassigned','action_due_changed','action_cancelled',
    'action_completed','action_transfer_requested','action_transfer_approved',
    'action_transfer_rejected','action_reminder','action_grace_started',
    'action_overdue','action_assignment_invalid'
));

CREATE TABLE call_action_dispositions (
    disposition_uuid UUID PRIMARY KEY,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    analysis_uuid UUID NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    kind TEXT NOT NULL CHECK (kind IN ('action_created','no_action_required')),
    reason TEXT NULL CHECK (reason IS NULL OR char_length(btrim(reason)) BETWEEN 3 AND 2000),
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    superseded_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX uq_call_action_dispositions_active_analysis
    ON call_action_dispositions(analysis_uuid) WHERE superseded_at IS NULL;

CREATE TABLE call_actions (
    action_uuid UUID PRIMARY KEY,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    source_department_uuid UUID NOT NULL REFERENCES departments(department_uuid) ON DELETE RESTRICT,
    target_department_uuid UUID NOT NULL REFERENCES departments(department_uuid) ON DELETE RESTRICT,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    analysis_uuid UUID NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    title TEXT NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','completed','cancelled','overdue')),
    assignment_state TEXT NOT NULL DEFAULT 'valid' CHECK (assignment_state IN ('valid','invalid')),
    assignee_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    due_at TIMESTAMPTZ NOT NULL,
    grace_expires_at TIMESTAMPTZ NOT NULL,
    schedule_version BIGINT NOT NULL DEFAULT 1 CHECK (schedule_version > 0),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    client_request_key TEXT NOT NULL CHECK (char_length(client_request_key) BETWEEN 8 AND 200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    completed_by_user_uuid UUID NULL REFERENCES users(user_uuid),
    cancelled_at TIMESTAMPTZ NULL,
    cancelled_by_user_uuid UUID NULL REFERENCES users(user_uuid),
    cancel_reason TEXT NULL,
    CONSTRAINT chk_call_actions_completed CHECK ((status = 'completed') = (completed_at IS NOT NULL)),
    CONSTRAINT chk_call_actions_cancelled CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
    CONSTRAINT chk_call_actions_cancel_reason CHECK (status <> 'cancelled' OR char_length(btrim(cancel_reason)) BETWEEN 10 AND 2000)
);
CREATE UNIQUE INDEX uq_call_actions_creator_request ON call_actions(created_by_user_uuid,client_request_key);
CREATE INDEX idx_call_actions_assignee_active ON call_actions(assignee_user_uuid,status,due_at,action_uuid);
CREATE INDEX idx_call_actions_company_updated ON call_actions(company_uuid,updated_at DESC,action_uuid);
CREATE INDEX idx_call_actions_source_updated ON call_actions(source_department_uuid,updated_at DESC,action_uuid);
CREATE INDEX idx_call_actions_target_updated ON call_actions(target_department_uuid,updated_at DESC,action_uuid);
CREATE INDEX idx_call_actions_due_active ON call_actions(due_at,action_uuid) WHERE status IN ('open','in_progress');
CREATE INDEX idx_call_actions_grace_active ON call_actions(grace_expires_at,action_uuid) WHERE status IN ('open','in_progress');
CREATE INDEX idx_call_actions_call ON call_actions(call_uuid,created_at DESC);

CREATE TABLE call_action_evidence (
    evidence_uuid UUID PRIMARY KEY,
    action_uuid UUID NOT NULL REFERENCES call_actions(action_uuid) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0 AND position < 20),
    kind TEXT NOT NULL CHECK (kind IN ('word_range','legacy_text')),
    word_start_index INTEGER NULL,
    word_end_index INTEGER NULL,
    quote_snapshot TEXT NOT NULL CHECK (char_length(btrim(quote_snapshot)) > 0),
    speaker_snapshot TEXT NULL,
    start_seconds DOUBLE PRECISION NULL,
    end_seconds DOUBLE PRECISION NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(action_uuid,position),
    CONSTRAINT chk_call_action_evidence_range CHECK (
      (kind='word_range' AND word_start_index IS NOT NULL AND word_end_index >= word_start_index AND start_seconds IS NOT NULL AND end_seconds >= start_seconds)
      OR (kind='legacy_text' AND word_start_index IS NULL AND word_end_index IS NULL AND start_seconds IS NULL AND end_seconds IS NULL)
    )
);
CREATE UNIQUE INDEX uq_call_action_evidence_word_range ON call_action_evidence(action_uuid,word_start_index,word_end_index) WHERE kind='word_range';

CREATE TABLE call_action_transfer_requests (
    request_uuid UUID PRIMARY KEY,
    action_uuid UUID NOT NULL REFERENCES call_actions(action_uuid) ON DELETE CASCADE,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    proposed_assignee_user_uuid UUID NULL REFERENCES users(user_uuid),
    proposed_department_uuid UUID NULL REFERENCES departments(department_uuid),
    reason TEXT NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 2000),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','withdrawn')),
    resolved_by_user_uuid UUID NULL REFERENCES users(user_uuid),
    resolution_comment TEXT NULL,
    action_lock_version_at_create BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX uq_call_action_transfer_pending ON call_action_transfer_requests(action_uuid) WHERE status='pending';

CREATE TABLE call_action_events (
    event_uuid UUID PRIMARY KEY,
    action_uuid UUID NOT NULL REFERENCES call_actions(action_uuid) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    actor_user_uuid UUID NULL REFERENCES users(user_uuid),
    reason TEXT NULL,
    old_data JSONB NULL,
    new_data JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_call_action_events_action ON call_action_events(action_uuid,created_at,event_uuid);

CREATE TABLE call_action_notification_deliveries (
    delivery_uuid UUID PRIMARY KEY,
    action_uuid UUID NOT NULL REFERENCES call_actions(action_uuid) ON DELETE CASCADE,
    recipient_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    schedule_version BIGINT NOT NULL,
    notification_uuid UUID NULL REFERENCES notifications(notification_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(action_uuid,recipient_uuid,kind,schedule_version)
);

-- +goose Down
DROP TABLE IF EXISTS call_action_notification_deliveries;
DROP TABLE IF EXISTS call_action_events;
DROP TABLE IF EXISTS call_action_transfer_requests;
DROP TABLE IF EXISTS call_action_evidence;
DROP TABLE IF EXISTS call_actions;
DROP TABLE IF EXISTS call_action_dispositions;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN ('invitation','report_ready','subscription','processing_failed'));
