-- +goose Up
-- Who a call is about. Until now that was only the person who uploaded it; the
-- employees who actually spoke are found on the server from speaker roles,
-- names and hints, and a call with two employees counts for both.
CREATE TABLE call_subjects (
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('uploader','speaker_match','manual')),
    is_primary BOOLEAN NOT NULL DEFAULT false,
    speaker_key TEXT,
    talk_share NUMERIC(5,2),
    match_signals TEXT[] NOT NULL DEFAULT '{}',
    -- Set when a person or a system of record marked the employee; only then
    -- may the call be shown to them. A name matched in the text never grants it.
    grants_access BOOLEAN NOT NULL DEFAULT false,
    set_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (call_uuid, user_uuid)
);
CREATE INDEX idx_call_subjects_user ON call_subjects (user_uuid, call_uuid);
CREATE UNIQUE INDEX uq_call_subjects_primary ON call_subjects (call_uuid) WHERE is_primary;

-- What resolving the subjects found about the conversation as a whole. It is
-- kept apart from the facts because the call page needs it before any analysis.
CREATE TABLE call_subject_states (
    call_uuid UUID PRIMARY KEY REFERENCES calls(call_uuid) ON DELETE CASCADE,
    -- more than one employee and an outside party
    is_shared BOOLEAN NOT NULL DEFAULT false,
    -- every speaker is an employee of the company: a meeting, not a client call
    is_internal BOOLEAN NOT NULL DEFAULT false,
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every change of who a call counts for, so moving a bad call onto a colleague
-- or out of one's own numbers leaves a trace.
CREATE TABLE call_subject_events (
    event_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    actor_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    before JSONB NOT NULL,
    after JSONB NOT NULL,
    cause TEXT NOT NULL CHECK (cause IN ('transcription','transcription_edit','speaker_assignments','analysis','manual','backfill','company_transfer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_call_subject_events_call ON call_subject_events (call_uuid, created_at DESC);

-- Facts are a projection of the effective analysis of a call, so analytics
-- never reads result_json. Company and department are left out on purpose: who
-- may see a fact is decided by joining calls, like every other call read.
CREATE TABLE analytics_call_facts (
    call_uuid UUID PRIMARY KEY REFERENCES calls(call_uuid) ON DELETE CASCADE,
    analysis_uuid UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    is_shared BOOLEAN NOT NULL DEFAULT false,
    is_internal BOOLEAN NOT NULL DEFAULT false,
    schema_version SMALLINT NOT NULL,
    scorecard_mode TEXT NOT NULL CHECK (scorecard_mode IN ('fixed','partial','adhoc','none','legacy')),
    pipeline_version TEXT NOT NULL DEFAULT '',
    judge_model TEXT NOT NULL DEFAULT '',
    ai_overall_score SMALLINT,
    human_overall_score SMALLINT,
    overall_score SMALLINT GENERATED ALWAYS AS (COALESCE(human_overall_score, ai_overall_score)) STORED,
    criteria_score SMALLINT,
    unassessed_weight_share NUMERIC(5,2) NOT NULL DEFAULT 0,
    coverage_status TEXT NOT NULL,
    criteria_total SMALLINT NOT NULL DEFAULT 0,
    criteria_scored SMALLINT NOT NULL DEFAULT 0,
    critical_missed SMALLINT NOT NULL DEFAULT 0,
    questions_total SMALLINT NOT NULL DEFAULT 0,
    questions_avg_score SMALLINT,
    projected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_analytics_call_facts_occurred ON analytics_call_facts (occurred_at DESC);

CREATE TABLE analytics_criterion_facts (
    call_uuid UUID NOT NULL REFERENCES analytics_call_facts(call_uuid) ON DELETE CASCADE,
    criterion_key UUID NOT NULL,
    instruction_uuid UUID NOT NULL,
    scorecard_uuid UUID NOT NULL,
    item_id TEXT NOT NULL,
    ai_status TEXT NOT NULL,
    ai_score SMALLINT,
    human_score SMALLINT,
    human_decision TEXT CHECK (human_decision IN ('confirmed','overridden','not_applicable')),
    score SMALLINT GENERATED ALWAYS AS (COALESCE(human_score, ai_score)) STORED,
    weight SMALLINT NOT NULL,
    is_critical BOOLEAN NOT NULL,
    -- false for the key of an instruction whose identical requirement was
    -- scored once under a narrower instruction's criterion
    counted_in_score BOOLEAN NOT NULL DEFAULT true,
    evidence_start_seconds NUMERIC,
    PRIMARY KEY (call_uuid, criterion_key)
);
CREATE INDEX idx_analytics_criterion_facts_key ON analytics_criterion_facts (criterion_key, call_uuid);

CREATE TABLE company_analytics_settings (
    company_uuid UUID PRIMARY KEY REFERENCES companies(company_uuid) ON DELETE CASCADE,
    critical_alert_threshold SMALLINT NOT NULL DEFAULT 50 CHECK (critical_alert_threshold BETWEEN 0 AND 100),
    growth_areas_enabled BOOLEAN NOT NULL DEFAULT true,
    lock_version INTEGER NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    updated_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The same two settings for a personal account.
ALTER TABLE user_preferences
    ADD COLUMN critical_alert_threshold SMALLINT NOT NULL DEFAULT 50 CHECK (critical_alert_threshold BETWEEN 0 AND 100),
    ADD COLUMN growth_areas_enabled BOOLEAN NOT NULL DEFAULT true;

-- Personal progress is paid: Plus and Pro. The free and zero-priced Start plans
-- see the overview only. The flag, not the plan code, is what the code reads.
ALTER TABLE plans ADD COLUMN personal_progress_enabled BOOLEAN NOT NULL DEFAULT false;
UPDATE plans SET personal_progress_enabled = true WHERE code IN ('personal_plus','personal_pro');

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
    'call_subject_marked','call_subjects_changed'
));

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
DELETE FROM notifications WHERE type IN ('call_subject_marked','call_subjects_changed');
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
    'scorecard_failed','scorecard_review_needed'
));
ALTER TABLE plans DROP COLUMN IF EXISTS personal_progress_enabled;
ALTER TABLE user_preferences DROP COLUMN IF EXISTS growth_areas_enabled, DROP COLUMN IF EXISTS critical_alert_threshold;
DROP TABLE IF EXISTS company_analytics_settings;
DROP TABLE IF EXISTS analytics_criterion_facts;
DROP TABLE IF EXISTS analytics_call_facts;
DROP TABLE IF EXISTS call_subject_events;
DROP TABLE IF EXISTS call_subject_states;
DROP TABLE IF EXISTS call_subjects;
