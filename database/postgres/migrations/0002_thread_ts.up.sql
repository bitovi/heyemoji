ALTER TABLE karma_events ADD COLUMN thread_ts TEXT;

CREATE INDEX idx_karma_thread_lookup ON karma_events (channel_id, to_user, created_at DESC) WHERE thread_ts IS NOT NULL;
