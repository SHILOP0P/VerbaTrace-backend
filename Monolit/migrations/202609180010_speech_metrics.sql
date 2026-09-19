-- +goose Up
-- Speech numbers measured from word timings, without a model or credits.
-- Whole-conversation numbers.
CREATE TABLE call_speech_metrics (
    call_uuid UUID PRIMARY KEY REFERENCES calls(call_uuid) ON DELETE CASCADE,
    transcription_revision INTEGER NOT NULL,
    speaker_switches_per_5min NUMERIC(5,1),
    pauses_over_threshold INTEGER,
    longest_pause_seconds INTEGER,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One row per speaker: a call may have several subjects, and the other side's
-- numbers are needed for the talk ratio anyway.
CREATE TABLE call_speaker_speech_metrics (
    call_uuid UUID NOT NULL REFERENCES call_speech_metrics(call_uuid) ON DELETE CASCADE,
    speaker_key TEXT NOT NULL,
    talk_seconds INTEGER NOT NULL,
    -- percent, 0–100
    talk_share NUMERIC(5,2) NOT NULL,
    words INTEGER NOT NULL,
    words_per_minute INTEGER,
    longest_monologue_seconds INTEGER,
    questions INTEGER,
    questions_per_hour NUMERIC(6,1),
    -- "patience": the other speaker ended -> this one started
    response_pause_median_ms INTEGER,
    PRIMARY KEY (call_uuid, speaker_key)
);

-- +goose Down
DROP TABLE IF EXISTS call_speaker_speech_metrics;
DROP TABLE IF EXISTS call_speech_metrics;
