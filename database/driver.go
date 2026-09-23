package database

import (
	"context"
	"errors"
	"time"
)

// ErrDailyCapExceeded is returned by GiveKarma when inserting the given batch of
// events would push the giver's total points given since 'since' past dailyCap. No
// events are inserted when this is returned.
var ErrDailyCapExceeded = errors.New("daily karma cap exceeded")

// KarmaEvent is one recognition grant: a single (from, to, emoji) triple and its
// point value and reason. Giving multiple emoji and/or to multiple recipients in one
// /heybitovi give produces multiple KarmaEvents.
type KarmaEvent struct {
	ID       string
	LegacyID string // set only for events backfilled from the legacy JSONL store; empty otherwise
	From     string
	To       string
	Emoji    string
	Points   int
	Reason   string
	// SourceChannelID is where /heybitovi give was actually typed - a public channel,
	// private channel, or DM. Purely informational (the web UI's feed shows it); it
	// has no bearing on where the announcement posts.
	SourceChannelID string
	// ThreadChannelID is where this event's announcement is (or would be) posted and
	// threaded - the configured announcement channel, the same value for every event
	// once HEY_ANNOUNCE_CHANNEL_ID is set, regardless of SourceChannelID. Empty when
	// no announcement channel is configured, meaning no announcement was attempted.
	ThreadChannelID string
	// ThreadTS is the Slack message timestamp of the announcement this event was (or
	// will be) posted under - the root of the recipient's recognition thread for the
	// grouping window in use, so later gives to the same person can thread onto it
	// instead of posting a new top-level message. Empty until known: on insert it's
	// set to an already-known root ts if one was found, and left empty otherwise
	// (backfilled afterward via SetThreadTS once a brand-new announcement's ts is
	// known).
	ThreadTS  string
	CreatedAt time.Time
}

type Driver interface {
	Open(ctx context.Context) error

	// GiveKarma inserts one row per event atomically, enforcing that the sum of
	// points already given by events[0].From since 'since' plus this batch's total
	// does not exceed dailyCap. On success it returns the inserted events (with ID
	// populated, same order as the input) and the giver's remaining balance for the
	// period. If the batch would exceed the cap, no events are inserted and
	// ErrDailyCapExceeded is returned.
	GiveKarma(ctx context.Context, events []KarmaEvent, since time.Time, dailyCap int) (inserted []KarmaEvent, remaining int, err error)

	// LatestThreadTS returns the thread_ts of the most recent event threaded under
	// (threadChannelID, toUser) created after since, if any (ok is false when there
	// isn't one). threadChannelID is the announcement channel, not where the give was
	// typed - see KarmaEvent.ThreadChannelID.
	LatestThreadTS(ctx context.Context, threadChannelID, toUser string, since time.Time) (ts string, ok bool, err error)

	// SetThreadTS backfills thread_ts on the given event IDs, once a brand-new
	// announcement message's timestamp is known (it can't be known before the
	// message is actually posted to Slack, which itself must happen after the events
	// are inserted so a cap-exceeded give never gets a public announcement).
	SetThreadTS(ctx context.Context, eventIDs []string, ts string) error

	QueryKarmaGiven(ctx context.Context, user string, since time.Time) (int, error)
	QueryKarmaReceived(ctx context.Context, user string, since time.Time) (int, error)
	QueryLeaderboard(ctx context.Context, since time.Time) (map[string]int, error)

	// QueryLeaderboardRange is QueryLeaderboard bounded on both ends - for viewing a
	// specific past month/quarter/year rather than "since X through now".
	QueryLeaderboardRange(ctx context.Context, since, until time.Time) (map[string]int, error)

	// QueryEarliestEventTime returns the created_at of the very first event ever
	// recorded, if any (ok is false when there are no events at all). Used to bound
	// how far back the web UI's month/quarter/year picker offers.
	QueryEarliestEventTime(ctx context.Context) (t time.Time, ok bool, err error)

	// QueryFeed returns the most recent recognition events (newest first), up to
	// limit, for the web UI's recognition feed.
	QueryFeed(ctx context.Context, limit int) ([]KarmaEvent, error)

	// QueryEventsSince returns every event created after since, oldest first. Used by
	// the web UI's "details" page to compute daily/weekday/emoji breakdowns in Go
	// rather than adding a separate SQL aggregation per stat - the data volume this
	// app deals with makes that simpler, not a bottleneck.
	QueryEventsSince(ctx context.Context, since time.Time) ([]KarmaEvent, error)
}
