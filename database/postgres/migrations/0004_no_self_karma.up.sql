-- Application code (event/give.go) already filters the giver out of recipients
-- before inserting, but that protection lived only in app logic with nothing backing
-- it up at the data layer. This makes a self-referential row structurally impossible
-- regardless of any bug, present or future, in that application-level check.
ALTER TABLE karma_events ADD CONSTRAINT karma_events_no_self_karma CHECK (from_user <> to_user);
