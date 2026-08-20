-- +goose Up
ALTER TABLE call_analysis_comments ADD COLUMN criterion_key TEXT NULL;
CREATE INDEX idx_call_analysis_comments_criterion ON call_analysis_comments(analysis_uuid, criterion_key, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_call_analysis_comments_criterion;
ALTER TABLE call_analysis_comments DROP COLUMN IF EXISTS criterion_key;
