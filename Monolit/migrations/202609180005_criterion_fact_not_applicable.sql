-- +goose Up
-- A reviewer who marks a criterion not applicable overrules the model's score:
-- the effective score of such a fact is empty, not the AI score behind it.
ALTER TABLE analytics_criterion_facts DROP COLUMN score;
ALTER TABLE analytics_criterion_facts ADD COLUMN score SMALLINT GENERATED ALWAYS AS (
    CASE WHEN human_decision = 'not_applicable' THEN NULL ELSE COALESCE(human_score, ai_score) END
) STORED;

-- +goose Down
ALTER TABLE analytics_criterion_facts DROP COLUMN score;
ALTER TABLE analytics_criterion_facts ADD COLUMN score SMALLINT GENERATED ALWAYS AS (COALESCE(human_score, ai_score)) STORED;
