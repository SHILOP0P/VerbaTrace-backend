-- +goose Up
-- A scorecard is an instruction version broken into criteria once, so every call
-- analysed against that version is scored on the same list and criteria can be
-- compared between calls. Until now the model split the instruction anew on each
-- call and the resulting requirements had no identity.
--
-- The scorecard row is also its own compile queue: the processing queue runs
-- one job at a time behind long transcriptions, and an analysis waiting for a
-- scorecard in that queue would wait for itself.
CREATE TABLE instruction_scorecards (
    scorecard_uuid UUID PRIMARY KEY,
    instruction_uuid UUID NOT NULL REFERENCES analysis_instructions(instruction_uuid) ON DELETE CASCADE,
    instruction_version_uuid UUID NOT NULL REFERENCES analysis_instruction_versions(instruction_version_uuid) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision > 0),
    status TEXT NOT NULL CHECK (status IN ('queued','compiling','ready','failed')),
    origin TEXT NOT NULL CHECK (origin IN ('compiled','copied','edited')),
    is_current BOOLEAN NOT NULL DEFAULT false,
    awaiting_confirmation BOOLEAN NOT NULL DEFAULT false,
    content_sha256 TEXT NOT NULL,
    compiler_version TEXT NOT NULL DEFAULT '',
    model TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    compile_after TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    credit_operation_uuid UUID,
    lock_version INTEGER NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    confirmed_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    confirmed_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (instruction_version_uuid, revision),
    CONSTRAINT chk_instruction_scorecards_current_ready CHECK (NOT is_current OR status = 'ready'),
    CONSTRAINT chk_instruction_scorecards_queue CHECK (status <> 'queued' OR compile_after IS NOT NULL)
);
-- One scorecard is in force per instruction. With confirmation switched on, a
-- newer version's scorecard waits beside it until somebody applies it.
CREATE UNIQUE INDEX uq_instruction_scorecards_current
    ON instruction_scorecards (instruction_uuid) WHERE is_current;
CREATE INDEX idx_instruction_scorecards_queue
    ON instruction_scorecards (compile_after) WHERE status IN ('queued','compiling');
CREATE INDEX idx_instruction_scorecards_instruction
    ON instruction_scorecards (instruction_uuid, created_at DESC);

CREATE TABLE instruction_scorecard_criteria (
    scorecard_uuid UUID NOT NULL REFERENCES instruction_scorecards(scorecard_uuid) ON DELETE CASCADE,
    criterion_key UUID NOT NULL,
    position INTEGER NOT NULL CHECK (position > 0),
    title TEXT NOT NULL,
    requirement TEXT NOT NULL,
    source_excerpt TEXT NOT NULL DEFAULT '',
    applicability TEXT NOT NULL DEFAULT '',
    depth TEXT NOT NULL DEFAULT '',
    required_question BOOLEAN NOT NULL DEFAULT false,
    cross_cutting BOOLEAN NOT NULL DEFAULT false,
    weight SMALLINT NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 3),
    is_critical BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    edited_fields TEXT[] NOT NULL DEFAULT '{}',
    change_kind TEXT NOT NULL DEFAULT 'new' CHECK (change_kind IN ('new','unchanged','reworded')),
    warnings TEXT[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (scorecard_uuid, criterion_key),
    UNIQUE (scorecard_uuid, position)
);
CREATE INDEX idx_scorecard_criteria_key ON instruction_scorecard_criteria (criterion_key);

-- The owner's correction of an automatic match between two versions: "this is
-- the same criterion as that one". Like a deprecated rule key, history under the
-- old key is read under the canonical one and no analysis result is rewritten.
CREATE TABLE criterion_key_aliases (
    alias_key UUID PRIMARY KEY,
    canonical_key UUID NOT NULL,
    instruction_uuid UUID NOT NULL REFERENCES analysis_instructions(instruction_uuid) ON DELETE CASCADE,
    created_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (alias_key <> canonical_key)
);
CREATE INDEX idx_criterion_key_aliases_canonical ON criterion_key_aliases (canonical_key);

ALTER TABLE call_analysis_instruction_snapshots
    ADD COLUMN scorecard_uuid UUID REFERENCES instruction_scorecards(scorecard_uuid) ON DELETE RESTRICT;
CREATE INDEX idx_analysis_instruction_snapshots_scorecard
    ON call_analysis_instruction_snapshots (scorecard_uuid) WHERE scorecard_uuid IS NOT NULL;

ALTER TABLE analysis_instructions
    ADD COLUMN scorecard_confirm_required BOOLEAN NOT NULL DEFAULT false;

-- Only versions used by some call, plus the current one, are kept. A version
-- replaced before any call read it is swept after a grace period; superseded_at
-- is when that clock starts.
ALTER TABLE analysis_instruction_versions ADD COLUMN superseded_at TIMESTAMPTZ;
UPDATE analysis_instruction_versions v
   SET superseded_at = next.created_at
  FROM (
      SELECT instruction_version_uuid,
             lead(created_at) OVER (PARTITION BY instruction_uuid ORDER BY version) AS created_at
        FROM analysis_instruction_versions
  ) next
 WHERE next.instruction_version_uuid = v.instruction_version_uuid
   AND next.created_at IS NOT NULL;

-- +goose StatementBegin
-- A new version supersedes the previous ones and queues its own scorecard. The
-- compile waits two minutes: while somebody is still editing, every save makes
-- a version, and only the one they settle on should be compiled and paid for.
-- A queued scorecard of a replaced version was never compiled and simply goes.
CREATE OR REPLACE FUNCTION queue_instruction_scorecard() RETURNS trigger AS $$
BEGIN
    UPDATE analysis_instruction_versions
       SET superseded_at = now()
     WHERE instruction_uuid = NEW.instruction_uuid
       AND instruction_version_uuid <> NEW.instruction_version_uuid
       AND superseded_at IS NULL;
    DELETE FROM instruction_scorecards
     WHERE instruction_uuid = NEW.instruction_uuid
       AND instruction_version_uuid <> NEW.instruction_version_uuid
       AND status = 'queued';
    INSERT INTO instruction_scorecards (
        scorecard_uuid, instruction_uuid, instruction_version_uuid, revision, status, origin,
        content_sha256, compile_after, created_by_user_uuid, created_at, updated_at
    ) VALUES (
        gen_random_uuid(), NEW.instruction_uuid, NEW.instruction_version_uuid, 1, 'queued', 'compiled',
        NEW.content_sha256, now() + interval '2 minutes', NEW.created_by_user_uuid, now(), now()
    );
    RETURN NEW;
END $$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER analysis_instruction_versions_queue_scorecard
AFTER INSERT ON analysis_instruction_versions
FOR EACH ROW EXECUTE FUNCTION queue_instruction_scorecard();

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
    'scorecard_failed','scorecard_review_needed'
));

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
DELETE FROM notifications WHERE type IN ('scorecard_failed','scorecard_review_needed');
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
DROP TRIGGER IF EXISTS analysis_instruction_versions_queue_scorecard ON analysis_instruction_versions;
DROP FUNCTION IF EXISTS queue_instruction_scorecard();
ALTER TABLE analysis_instruction_versions DROP COLUMN IF EXISTS superseded_at;
ALTER TABLE analysis_instructions DROP COLUMN IF EXISTS scorecard_confirm_required;
DROP INDEX IF EXISTS idx_analysis_instruction_snapshots_scorecard;
ALTER TABLE call_analysis_instruction_snapshots DROP COLUMN IF EXISTS scorecard_uuid;
DROP TABLE IF EXISTS criterion_key_aliases;
DROP TABLE IF EXISTS instruction_scorecard_criteria;
DROP TABLE IF EXISTS instruction_scorecards;
