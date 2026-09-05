-- Department privacy policies override the company policy for department-scoped calls.
-- +goose Up
ALTER TABLE transcription_privacy_policies
    DROP CONSTRAINT transcription_privacy_policies_scope_type_check,
    DROP CONSTRAINT transcription_privacy_policies_check,
    ADD COLUMN department_uuid UUID,
    ADD CONSTRAINT fk_privacy_policy_department_company
        FOREIGN KEY (department_uuid, company_uuid)
        REFERENCES departments(department_uuid, company_uuid)
        ON DELETE CASCADE,
    ADD CONSTRAINT chk_privacy_policy_scope_type
        CHECK (scope_type IN ('personal','company','department')),
    ADD CONSTRAINT chk_privacy_policy_scope_owner
        CHECK (
            (scope_type='personal' AND owner_user_uuid IS NOT NULL AND company_uuid IS NULL AND department_uuid IS NULL)
            OR (scope_type='company' AND owner_user_uuid IS NULL AND company_uuid IS NOT NULL AND department_uuid IS NULL)
            OR (scope_type='department' AND owner_user_uuid IS NULL AND company_uuid IS NOT NULL AND department_uuid IS NOT NULL)
        );

CREATE UNIQUE INDEX uq_privacy_policy_department
    ON transcription_privacy_policies(department_uuid)
    WHERE scope_type='department';

ALTER TABLE call_privacy_states
    DROP CONSTRAINT call_privacy_states_policy_source_check,
    ADD CONSTRAINT chk_call_privacy_policy_source
        CHECK (policy_source IN ('none','personal','company','department','simulated'));

-- +goose Down
ALTER TABLE call_privacy_states
    DROP CONSTRAINT chk_call_privacy_policy_source,
    ADD CONSTRAINT call_privacy_states_policy_source_check
        CHECK (policy_source IN ('none','personal','company','simulated'));

DROP INDEX IF EXISTS uq_privacy_policy_department;

ALTER TABLE transcription_privacy_policies
    DROP CONSTRAINT chk_privacy_policy_scope_owner,
    DROP CONSTRAINT chk_privacy_policy_scope_type,
    DROP CONSTRAINT fk_privacy_policy_department_company,
    DROP COLUMN department_uuid,
    ADD CONSTRAINT transcription_privacy_policies_scope_type_check
        CHECK (scope_type IN ('personal','company')),
    ADD CONSTRAINT transcription_privacy_policies_check
        CHECK (
            (scope_type='personal' AND owner_user_uuid IS NOT NULL AND company_uuid IS NULL)
            OR (scope_type='company' AND company_uuid IS NOT NULL AND owner_user_uuid IS NULL)
        );
