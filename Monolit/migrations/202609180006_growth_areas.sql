-- +goose Up
-- Growth areas are the approximate layer of the work on mistakes: recurring
-- shortcomings outside the scorecard, matched between calls by the summary step.
CREATE TABLE growth_areas (
    area_uuid UUID PRIMARY KEY,
    subject_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    -- NULL is a personal account.
    company_uuid UUID REFERENCES companies(company_uuid) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','dismissed')),
    occurrences INTEGER NOT NULL DEFAULT 1,
    clean_streak INTEGER NOT NULL DEFAULT 0,
    -- true once the area came back after it had been resolved
    returned BOOLEAN NOT NULL DEFAULT false,
    first_call_uuid UUID,
    last_seen_call_uuid UUID,
    last_seen_at TIMESTAMPTZ,
    dismissed_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    dismissed_at TIMESTAMPTZ,
    dismiss_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX idx_growth_areas_subject ON growth_areas (subject_user_uuid, company_uuid, status);

CREATE TABLE growth_area_observations (
    area_uuid UUID NOT NULL REFERENCES growth_areas(area_uuid) ON DELETE CASCADE,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    verdict TEXT NOT NULL CHECK (verdict IN ('new','repeated','improved','not_applicable')),
    item_ids TEXT[] NOT NULL DEFAULT '{}',
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (area_uuid, call_uuid)
);
CREATE INDEX idx_growth_area_observations_call ON growth_area_observations (call_uuid);

-- Hiding an area says the model was wrong or it is not a mistake; who said so
-- and why is kept.
CREATE TABLE growth_area_events (
    event_uuid UUID PRIMARY KEY,
    area_uuid UUID NOT NULL REFERENCES growth_areas(area_uuid) ON DELETE CASCADE,
    actor_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    action TEXT NOT NULL CHECK (action IN ('dismiss','reopen')),
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_growth_area_events_area ON growth_area_events (area_uuid, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS growth_area_events;
DROP TABLE IF EXISTS growth_area_observations;
DROP TABLE IF EXISTS growth_areas;
