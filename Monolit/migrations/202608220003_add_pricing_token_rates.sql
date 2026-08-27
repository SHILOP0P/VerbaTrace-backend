-- +goose Up
-- Kept separate because 202608220001 may already be deployed. Goose migrations
-- are immutable after release; this migration upgrades both existing and fresh DBs.
ALTER TABLE pricing_rates
    ADD COLUMN IF NOT EXISTS input_cost_nano_usd_per_token BIGINT NOT NULL DEFAULT 0
        CHECK (input_cost_nano_usd_per_token >= 0),
    ADD COLUMN IF NOT EXISTS output_cost_nano_usd_per_token BIGINT NOT NULL DEFAULT 0
        CHECK (output_cost_nano_usd_per_token >= 0);

UPDATE pricing_rates
   SET input_cost_nano_usd_per_token = 250,
       output_cost_nano_usd_per_token = 2000
 WHERE provider = 'openrouter'
   AND model = 'openai/gpt-5-mini'
   AND operation_type IN ('analysis', 'deep_analysis');

-- +goose Down
ALTER TABLE pricing_rates
    DROP COLUMN IF EXISTS output_cost_nano_usd_per_token,
    DROP COLUMN IF EXISTS input_cost_nano_usd_per_token;
