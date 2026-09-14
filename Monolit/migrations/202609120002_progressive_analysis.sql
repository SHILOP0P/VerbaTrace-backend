-- +goose Up
ALTER TABLE call_analyses ADD COLUMN pipeline_run_key text;
CREATE TABLE call_analysis_tasks (
 task_uuid uuid PRIMARY KEY,
 analysis_uuid uuid NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
 result jsonb,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX call_analysis_tasks_analysis_idx ON call_analysis_tasks(analysis_uuid);

-- +goose Down
DROP TABLE call_analysis_tasks;
ALTER TABLE call_analyses DROP COLUMN pipeline_run_key;
