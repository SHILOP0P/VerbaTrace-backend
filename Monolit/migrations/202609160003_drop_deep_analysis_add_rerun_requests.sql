-- +goose Up
-- Deep analysis is withdrawn as a product: it produced generic summaries nobody
-- acted on while costing a weekly quota to keep track of.
DROP TABLE IF EXISTS aggregate_report_exports;
DROP TABLE IF EXISTS deep_analysis_usage_counters;
DROP TABLE IF EXISTS aggregate_analyses;

-- An employee cannot re-run the analysis of a company call on their own, so
-- they ask the person responsible for the call instead.
CREATE TABLE call_analysis_rerun_requests (
    request_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    company_uuid UUID NOT NULL REFERENCES companies(company_uuid) ON DELETE CASCADE,
    department_uuid UUID NULL REFERENCES departments(department_uuid) ON DELETE SET NULL,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    reason TEXT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    decided_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    decided_at TIMESTAMPTZ NULL,
    comment TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_analysis_rerun_status CHECK (status IN ('pending','approved','rejected','canceled')),
    CONSTRAINT chk_analysis_rerun_decision CHECK (
        (status = 'pending' AND decided_at IS NULL AND decided_by_user_uuid IS NULL)
        OR (status <> 'pending' AND decided_at IS NOT NULL)
    )
);

-- One open request per call: asking twice changes nothing.
CREATE UNIQUE INDEX uq_analysis_rerun_pending_call
    ON call_analysis_rerun_requests (call_uuid) WHERE status = 'pending';
CREATE INDEX idx_analysis_rerun_company_queue
    ON call_analysis_rerun_requests (company_uuid, status, created_at DESC);

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

-- +goose Down
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

DROP TABLE IF EXISTS call_analysis_rerun_requests;

CREATE TABLE aggregate_analyses (
    aggregate_analysis_uuid UUID PRIMARY KEY,
    scope TEXT NOT NULL,
    user_uuid UUID NULL REFERENCES users(user_uuid),
    company_uuid UUID NULL REFERENCES companies(company_uuid),
    department_uuid UUID NULL,
    folder_uuid UUID NULL,
    period_from TIMESTAMPTZ NOT NULL,
    period_to TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NULL,
    source_calls_count INT NOT NULL DEFAULT 0,
    result_json JSONB NULL,
    result_text TEXT NULL,
    error_message TEXT NULL,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_aggregate_analyses_scope
        CHECK (scope IN ('personal', 'company', 'department', 'folder')),
    CONSTRAINT chk_aggregate_analyses_status
        CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    CONSTRAINT chk_aggregate_analyses_scope_placement
        CHECK (
            (scope = 'personal' AND user_uuid IS NOT NULL AND company_uuid IS NULL AND department_uuid IS NULL AND folder_uuid IS NULL)
            OR (scope = 'company' AND user_uuid IS NULL AND company_uuid IS NOT NULL AND department_uuid IS NULL AND folder_uuid IS NULL)
            OR (scope = 'department' AND user_uuid IS NULL AND company_uuid IS NOT NULL AND department_uuid IS NOT NULL AND folder_uuid IS NULL)
            OR (scope = 'folder' AND folder_uuid IS NOT NULL)
        ),
    CONSTRAINT chk_aggregate_analyses_status_data
        CHECK (
            (status IN ('pending', 'processing') AND error_message IS NULL)
            OR (status = 'done' AND result_json IS NOT NULL AND error_message IS NULL)
            OR (status = 'failed' AND error_message IS NOT NULL)
        )
);

CREATE INDEX idx_aggregate_analyses_created_by
    ON aggregate_analyses (created_by_user_uuid, created_at DESC);
CREATE INDEX idx_aggregate_analyses_scope_subject_period
    ON aggregate_analyses (scope, company_uuid, department_uuid, folder_uuid, period_from, period_to);
CREATE INDEX idx_aggregate_analyses_status_updated
    ON aggregate_analyses (status, updated_at DESC);

CREATE TABLE deep_analysis_usage_counters (
    counter_uuid UUID PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_uuid UUID NOT NULL,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    used_count INT NOT NULL DEFAULT 0,
    limit_count INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subject_type, subject_uuid, period_start),
    CONSTRAINT chk_deep_analysis_usage_subject_type
        CHECK (subject_type IN ('user', 'company')),
    CONSTRAINT chk_deep_analysis_usage_count
        CHECK (used_count >= 0 AND used_count <= limit_count)
);

CREATE TABLE aggregate_report_exports (
    report_uuid UUID PRIMARY KEY,
    aggregate_analysis_uuid UUID NOT NULL REFERENCES aggregate_analyses(aggregate_analysis_uuid) ON DELETE CASCADE,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    format TEXT NOT NULL,
    status TEXT NOT NULL,
    storage_path TEXT NULL,
    file_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_aggregate_report_exports_format
        CHECK (format IN ('pdf', 'docx', 'md', 'xlsx')),
    CONSTRAINT chk_aggregate_report_exports_status
        CHECK (status IN ('pending', 'ready', 'failed')),
    CONSTRAINT chk_aggregate_report_exports_storage_for_ready
        CHECK (status <> 'ready' OR storage_path IS NOT NULL)
);

CREATE INDEX idx_aggregate_report_exports_analysis
    ON aggregate_report_exports (aggregate_analysis_uuid, created_at DESC);
CREATE INDEX idx_aggregate_report_exports_requested_by
    ON aggregate_report_exports (requested_by_user_uuid, created_at DESC);
CREATE INDEX idx_aggregate_report_exports_expires_at
    ON aggregate_report_exports (expires_at);
