-- +goose Up
DELETE FROM processing_jobs j
USING ingest_items i, developer_applications app
WHERE j.entity_uuid=i.call_uuid AND j.job_type='analyze_call'
  AND app.application_uuid=i.application_uuid AND app.environment='sandbox';

DELETE FROM call_analyses a
USING ingest_items i, developer_applications app
WHERE a.call_uuid=i.call_uuid
  AND app.application_uuid=i.application_uuid AND app.environment='sandbox';

UPDATE calls c SET status='transcribed'
FROM ingest_items i, developer_applications app
WHERE c.call_uuid=i.call_uuid
  AND app.application_uuid=i.application_uuid AND app.environment='sandbox'
  AND EXISTS (SELECT 1 FROM call_transcriptions t WHERE t.call_uuid=c.call_uuid AND t.status='transcribed');

-- +goose Down
-- Removed AI results and jobs cannot be reconstructed safely.
SELECT 1;
