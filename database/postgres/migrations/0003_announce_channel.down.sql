ALTER TABLE karma_events DROP COLUMN IF EXISTS source_channel_id;
ALTER TABLE karma_events RENAME COLUMN thread_channel_id TO channel_id;
