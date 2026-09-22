package event

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"
)

// UnlimitedDailyCap is the effective daily give cap used in test mode - large enough
// to never realistically be hit, so the normal cap-check code path doesn't need a
// separate "no cap" branch. Handlers that display the cap to a user should check
// testMode instead of comparing against this value, so the display says "unlimited"
// rather than this literal number.
const UnlimitedDailyCap = 1 << 30

// EventHandler handles one /heybitovi subcommand.
type EventHandler interface {
	// Subcommand is the first word of the slash command text this handler responds
	// to, e.g. "give", "leaderboard", "points", "help", "ping".
	Subcommand() string
	// Execute handles the remaining command text (args) and sends its own response(s)
	// back to Slack via client.
	Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error
}

// LastPointReset returns the start of the current daily point-giving period, in
// businessLocation - the same day boundary the "day" leaderboard period uses.
func LastPointReset() time.Time {
	reset := time.Now().In(businessLocation)
	return time.Date(reset.Year(), reset.Month(), reset.Day(), 0, 0, 0, 0, businessLocation)
}

// NextPointReset returns the start of the next daily point-giving period, in
// businessLocation.
func NextPointReset() time.Time {
	now := time.Now().In(businessLocation)
	reset := now.AddDate(0, 0, 1)
	return time.Date(reset.Year(), reset.Month(), reset.Day(), 0, 0, 0, 0, businessLocation)
}

// TimeTillPointReset returns the duration until the next daily point reset.
func TimeTillPointReset() time.Duration {
	return NextPointReset().Sub(time.Now())
}

// FmtDuration formats a duration till karma reset for display to a user.
func FmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	hr := d / time.Hour
	d -= hr * time.Hour
	m := d / time.Minute
	return fmt.Sprintf("%2d hours and %2d minutes", hr, m)
}

// Map applies f to every element of vs, returning a new slice.
func Map(vs []string, f func(string) string) []string {
	vsm := make([]string, len(vs))
	for i, v := range vs {
		vsm[i] = f(v)
	}
	return vsm
}

// Keys returns the keys of m in unspecified order.
func Keys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
