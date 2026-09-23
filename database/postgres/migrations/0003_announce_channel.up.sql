-- The announcement now always posts to one configured channel (HEY_ANNOUNCE_CHANNEL_ID)
-- instead of wherever /heybitovi give was typed, so "which channel is this event's
-- thread grouped under" and "which channel was it actually given from" are no longer
-- the same value - split them into two columns.
ALTER TABLE karma_events RENAME COLUMN channel_id TO thread_channel_id;
ALTER TABLE karma_events ADD COLUMN source_channel_id TEXT;

-- Existing rows' announcement WAS posted into their origin channel, so that's the
-- correct source_channel_id backfill value.
UPDATE karma_events SET source_channel_id = thread_channel_id;
