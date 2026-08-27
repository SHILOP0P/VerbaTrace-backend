-- +goose Up
ALTER TABLE call_folders DROP CONSTRAINT chk_call_folders_system_type;
ALTER TABLE call_folders ADD CONSTRAINT chk_call_folders_system_type
    CHECK (system_type IS NULL OR system_type IN ('external_ingest','sandbox_test'));

ALTER TABLE ingest_items DROP CONSTRAINT chk_ingest_placement_source;
ALTER TABLE ingest_items ADD CONSTRAINT chk_ingest_placement_source
    CHECK (placement_source IN ('connection_default','request_override','system_external','system_sandbox'));

CREATE UNIQUE INDEX uq_call_folders_sandbox_personal ON call_folders(user_uuid)
    WHERE deleted_at IS NULL AND scope='personal' AND system_type='sandbox_test';
CREATE UNIQUE INDEX uq_call_folders_sandbox_company ON call_folders(company_uuid)
    WHERE deleted_at IS NULL AND scope='company' AND system_type='sandbox_test';
CREATE UNIQUE INDEX uq_call_folders_sandbox_department ON call_folders(company_uuid,department_uuid)
    WHERE deleted_at IS NULL AND scope='department' AND system_type='sandbox_test';

INSERT INTO call_folders(folder_uuid,scope,user_uuid,company_uuid,department_uuid,name,description,created_by_user_uuid,created_by_actor_type,system_type)
SELECT gen_random_uuid(),i.destination_scope,i.destination_user_uuid,i.destination_company_uuid,i.destination_department_uuid,
       'Тестовые звонки','Системная папка для звонков из тестовой среды',c.created_by_user_uuid,'system','sandbox_test'
FROM ingest_items i
JOIN integration_connections c ON c.connection_uuid=i.connection_uuid
JOIN developer_applications app ON app.application_uuid=i.application_uuid
WHERE app.environment='sandbox' AND i.call_uuid IS NOT NULL
GROUP BY i.destination_scope,i.destination_user_uuid,i.destination_company_uuid,i.destination_department_uuid,c.created_by_user_uuid
ON CONFLICT DO NOTHING;

DELETE FROM call_folder_assignments a
USING ingest_items i, developer_applications app
WHERE i.call_uuid=a.call_uuid AND app.application_uuid=i.application_uuid AND app.environment='sandbox';

INSERT INTO call_folder_assignments(folder_uuid,call_uuid,assigned_by_user_uuid)
SELECT f.folder_uuid,i.call_uuid,c.created_by_user_uuid
FROM ingest_items i
JOIN integration_connections c ON c.connection_uuid=i.connection_uuid
JOIN developer_applications app ON app.application_uuid=i.application_uuid
JOIN call_folders f ON f.deleted_at IS NULL AND f.system_type='sandbox_test'
 AND f.scope=i.destination_scope
 AND f.user_uuid IS NOT DISTINCT FROM i.destination_user_uuid
 AND f.company_uuid IS NOT DISTINCT FROM i.destination_company_uuid
 AND f.department_uuid IS NOT DISTINCT FROM i.destination_department_uuid
WHERE app.environment='sandbox' AND i.call_uuid IS NOT NULL
ON CONFLICT DO NOTHING;

UPDATE ingest_items i SET placement_source='system_sandbox'
FROM developer_applications app
WHERE app.application_uuid=i.application_uuid AND app.environment='sandbox';

UPDATE processing_jobs j SET status='failed',locked_at=NULL,locked_by=NULL,
    last_error='test calls are read only',updated_at=now()
FROM ingest_items i JOIN developer_applications app ON app.application_uuid=i.application_uuid
WHERE j.entity_uuid=i.call_uuid AND j.job_type='analyze_call'
  AND j.status IN ('pending','running') AND app.environment='sandbox';

UPDATE call_analyses a SET status='failed',error_message='Тестовые звонки не анализируются',updated_at=now()
FROM ingest_items i JOIN developer_applications app ON app.application_uuid=i.application_uuid
WHERE a.call_uuid=i.call_uuid AND a.status IN ('pending','processing')
  AND app.environment='sandbox';

-- +goose Down
UPDATE ingest_items SET placement_source='system_external'
WHERE placement_source='system_sandbox';
DELETE FROM call_folders WHERE system_type='sandbox_test';
DROP INDEX IF EXISTS uq_call_folders_sandbox_department;
DROP INDEX IF EXISTS uq_call_folders_sandbox_company;
DROP INDEX IF EXISTS uq_call_folders_sandbox_personal;
ALTER TABLE ingest_items DROP CONSTRAINT chk_ingest_placement_source;
ALTER TABLE ingest_items ADD CONSTRAINT chk_ingest_placement_source
    CHECK (placement_source IN ('connection_default','request_override','system_external'));
ALTER TABLE call_folders DROP CONSTRAINT chk_call_folders_system_type;
ALTER TABLE call_folders ADD CONSTRAINT chk_call_folders_system_type
    CHECK (system_type IS NULL OR system_type IN ('external_ingest'));
