DROP INDEX IF EXISTS idx_karma_thread_lookup;
ALTER TABLE karma_events DROP COLUMN IF EXISTS thread_ts;
