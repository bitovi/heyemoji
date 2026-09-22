CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE karma_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user   TEXT NOT NULL,
    to_user     TEXT NOT NULL,
    emoji       TEXT NOT NULL,
    points      INT NOT NULL CHECK (points > 0),
    reason      TEXT,
    channel_id  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    legacy_id   TEXT UNIQUE
);

CREATE INDEX idx_karma_to_user   ON karma_events (to_user, created_at DESC);
CREATE INDEX idx_karma_from_user ON karma_events (from_user, created_at DESC);
