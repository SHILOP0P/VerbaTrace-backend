-- +goose Up
-- Deleting a call in the middle of processing left the queue working on
-- something nobody wanted and, if the provider was stuck, nothing could stop it
-- until every retry was spent. Cancelling is now its own operation: the call
-- stops, the credit reservation goes back, and the call waits for whatever its
-- owner decides next with the ordinary retention period.
ALTER TABLE calls DROP CONSTRAINT IF EXISTS chk_calls_status;
ALTER TABLE calls ADD CONSTRAINT chk_calls_status
    CHECK (status IN ('new', 'processing', 'transcribed', 'analyzed', 'failed', 'awaiting_credits', 'cancelled'));

-- +goose Down
UPDATE calls SET status = 'failed' WHERE status = 'cancelled';
ALTER TABLE calls DROP CONSTRAINT IF EXISTS chk_calls_status;
ALTER TABLE calls ADD CONSTRAINT chk_calls_status
    CHECK (status IN ('new', 'processing', 'transcribed', 'analyzed', 'failed', 'awaiting_credits'));
