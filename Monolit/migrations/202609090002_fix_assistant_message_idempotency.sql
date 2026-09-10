-- +goose Up
-- NULL identifies provider answers, not a client retry key.
ALTER TABLE assistant_messages DROP CONSTRAINT assistant_messages_assistant_chat_uuid_client_message_id_key;
CREATE UNIQUE INDEX uq_assistant_client_message ON assistant_messages(assistant_chat_uuid,client_message_id) WHERE client_message_id IS NOT NULL;

-- +goose Down
-- Preserve multiple generated replies; ordinary uniqueness also permits NULL.
DROP INDEX uq_assistant_client_message;
ALTER TABLE assistant_messages ADD CONSTRAINT assistant_messages_assistant_chat_uuid_client_message_id_key UNIQUE(assistant_chat_uuid,client_message_id);
