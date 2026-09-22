package event

import (
	"time"
	_ "time/tzdata" // embed the IANA tzdata database so LoadLocation works even in a
	// minimal container image (e.g. distroless) that has no /usr/share/zoneinfo.
)

// businessTimezone is the timezone Bitovi's work day is anchored to. Leaderboard
// period boundaries and the daily point-reset (see LastPointReset/NextPointReset in
// event.go) are computed against this, not the server's local time or UTC - "today"
// means today in the Eastern US, regardless of where the app happens to be deployed.
// America/New_York is used rather than a fixed UTC-5 offset so this automatically
// accounts for EST/EDT (daylight saving) transitions.
const businessTimezone = "America/New_York"

var businessLocation = mustLoadLocation(businessTimezone)

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// BusinessLocation returns the timezone all period/reset boundaries are computed
// against (see businessTimezone), for callers (e.g. the web UI) that need to display
// timestamps in the same timezone rather than UTC.
func BusinessLocation() *time.Location {
	return businessLocation
}

// ResolvePeriod maps a structured period token (day, week, month, quarter, year, all)
// to the start time a leaderboard query should use and a display header for it. ok is
// false for any other input.
//
// Unlike the legacy implementation this replaces (which used strings.Contains on a
// free-text message and could mistake "Saturday" or "yesterday" for "day"), this only
// matches exact period tokens.
func ResolvePeriod(period string, now time.Time) (target time.Time, header string, ok bool) {
	start := now.In(businessLocation)
	dayStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, businessLocation)

	switch period {
	case "day":
		return dayStart.AddDate(0, 0, -1), "Today's Leaderboard", true
	case "week":
		offset := (int(dayStart.Weekday()) + 6) % 7 // Monday = 0
		return dayStart.AddDate(0, 0, -offset), "This Week's Leaderboard", true
	case "month":
		return time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, businessLocation), "This Month's Leaderboard", true
	case "quarter":
		month := ((int(start.Month())-1)/3)*3 + 1
		return time.Date(start.Year(), time.Month(month), 1, 0, 0, 0, 0, businessLocation), "This Quarter's Leaderboard", true
	case "year":
		return time.Date(start.Year(), 1, 1, 0, 0, 0, 0, businessLocation), "This Year's Leaderboard", true
	case "all":
		return dayStart.AddDate(-100, 0, 0), "All Time Leaderboard", true
	default:
		return time.Time{}, "", false
	}
}

// ResolveMonth returns the [start, end) boundary for a specific calendar month, in
// businessLocation - for viewing a past month, not just "this month".
func ResolveMonth(year int, month time.Month) (start, end time.Time) {
	start = time.Date(year, month, 1, 0, 0, 0, 0, businessLocation)
	return start, start.AddDate(0, 1, 0)
}

// ResolveQuarter returns the [start, end) boundary for a specific quarter (1-4) of
// year, in businessLocation.
func ResolveQuarter(year, quarter int) (start, end time.Time) {
	month := time.Month((quarter-1)*3 + 1)
	start = time.Date(year, month, 1, 0, 0, 0, 0, businessLocation)
	return start, start.AddDate(0, 3, 0)
}

// ResolveYear returns the [start, end) boundary for a specific calendar year, in
// businessLocation.
func ResolveYear(year int) (start, end time.Time) {
	start = time.Date(year, 1, 1, 0, 0, 0, 0, businessLocation)
	return start, start.AddDate(1, 0, 0)
}

// QuarterOf returns which quarter (1-4) a month falls in.
func QuarterOf(month time.Month) int {
	return (int(month)-1)/3 + 1
}
