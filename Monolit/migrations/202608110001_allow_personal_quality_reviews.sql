-- +goose Up
ALTER TABLE call_quality_reviews
    ALTER COLUMN company_uuid DROP NOT NULL;

-- +goose Down
DELETE FROM call_quality_reviews WHERE company_uuid IS NULL;
ALTER TABLE call_quality_reviews
    ALTER COLUMN company_uuid SET NOT NULL;
