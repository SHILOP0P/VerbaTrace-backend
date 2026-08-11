-- +goose Up
CREATE TABLE call_quality_review_challenges (
    challenge_uuid UUID PRIMARY KEY,
    review_uuid UUID NOT NULL REFERENCES call_quality_reviews(review_uuid) ON DELETE CASCADE,
    analysis_uuid UUID NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    author_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (review_uuid)
);

CREATE INDEX idx_quality_review_challenges_author
    ON call_quality_review_challenges (author_user_uuid, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS call_quality_review_challenges;
