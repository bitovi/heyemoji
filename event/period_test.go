package event

import (
	"testing"
	"time"
)

func TestResolvePeriod_ValidTokens(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 0, 0, 0, businessLocation) // a Monday

	for _, period := range []string{"day", "week", "month", "quarter", "year", "all"} {
		if _, header, ok := ResolvePeriod(period, now); !ok || header == "" {
			t.Errorf("ResolvePeriod(%q) = ok:%v header:%q, want ok:true and a non-empty header", period, ok, header)
		}
	}
}

func TestResolvePeriod_RejectsFreeText(t *testing.T) {
	// Regression test: the legacy leaderboard handler used strings.Contains(text,
	// "day") to detect the period, which false-matched inside words like "Saturday"
	// and "yesterday". Structured period tokens must reject these outright instead
	// of silently treating them as "day".
	now := time.Date(2026, 9, 21, 15, 0, 0, 0, businessLocation)

	for _, period := range []string{"Saturday", "saturday", "Sunday", "yesterday", "someday", "", "bogus"} {
		if _, _, ok := ResolvePeriod(period, now); ok {
			t.Errorf("ResolvePeriod(%q) = ok:true, want false", period)
		}
	}
}

func TestResolvePeriod_Day(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 30, 0, 0, businessLocation)

	target, header, ok := ResolvePeriod("day", now)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := time.Date(2026, 9, 21, 0, 0, 0, 0, businessLocation); !target.Equal(want) {
		t.Errorf("target = %v, want %v", target, want)
	}
	if header != "Today's Leaderboard" {
		t.Errorf("header = %q", header)
	}
}

func TestResolvePeriod_Week(t *testing.T) {
	// Wednesday 2026-09-23 -> week should start Monday 2026-09-21.
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, businessLocation)

	target, _, ok := ResolvePeriod("week", now)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := time.Date(2026, 9, 21, 0, 0, 0, 0, businessLocation); !target.Equal(want) {
		t.Errorf("target = %v, want %v", target, want)
	}
}

func TestResolveMonth(t *testing.T) {
	start, end := ResolveMonth(2026, time.February)
	if want := time.Date(2026, 2, 1, 0, 0, 0, 0, businessLocation); !start.Equal(want) {
		t.Errorf("start = %v, want %v", start, want)
	}
	if want := time.Date(2026, 3, 1, 0, 0, 0, 0, businessLocation); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestResolveMonth_DecemberRollsIntoNextYear(t *testing.T) {
	_, end := ResolveMonth(2026, time.December)
	if want := time.Date(2027, 1, 1, 0, 0, 0, 0, businessLocation); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestResolveQuarter(t *testing.T) {
	cases := []struct {
		quarter    int
		wantStart  time.Month
		wantEndMon time.Month
		wantEndYr  int
	}{
		{1, time.January, time.April, 2026},
		{2, time.April, time.July, 2026},
		{3, time.July, time.October, 2026},
		{4, time.October, time.January, 2027},
	}
	for _, c := range cases {
		start, end := ResolveQuarter(2026, c.quarter)
		if start.Month() != c.wantStart || start.Day() != 1 {
			t.Errorf("Q%d start = %v, want month %v day 1", c.quarter, start, c.wantStart)
		}
		if end.Month() != c.wantEndMon || end.Year() != c.wantEndYr {
			t.Errorf("Q%d end = %v, want month %v year %d", c.quarter, end, c.wantEndMon, c.wantEndYr)
		}
	}
}

func TestResolveYear(t *testing.T) {
	start, end := ResolveYear(2025)
	if want := time.Date(2025, 1, 1, 0, 0, 0, 0, businessLocation); !start.Equal(want) {
		t.Errorf("start = %v, want %v", start, want)
	}
	if want := time.Date(2026, 1, 1, 0, 0, 0, 0, businessLocation); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestQuarterOf(t *testing.T) {
	cases := map[time.Month]int{
		time.January: 1, time.March: 1,
		time.April: 2, time.June: 2,
		time.July: 3, time.September: 3,
		time.October: 4, time.December: 4,
	}
	for month, want := range cases {
		if got := QuarterOf(month); got != want {
			t.Errorf("QuarterOf(%v) = %d, want %d", month, got, want)
		}
	}
}

func TestResolvePeriod_UsesEasternDayBoundaryNotUTC(t *testing.T) {
	// 2026-09-22 02:00 UTC is still 2026-09-21 22:00 EDT (Monday evening in Eastern
	// time), even though it's already Tuesday in UTC. Day/week boundaries must follow
	// Eastern local time, not whatever timezone the input happens to be expressed in
	// (which, for time.Now() on a server, is typically UTC) - this is the whole point
	// of businessLocation.
	now := time.Date(2026, 9, 22, 2, 0, 0, 0, time.UTC)

	target, _, ok := ResolvePeriod("week", now)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := time.Date(2026, 9, 21, 0, 0, 0, 0, businessLocation); !target.Equal(want) {
		t.Errorf("target = %v, want %v (Monday in Eastern time, not Tuesday in UTC)", target, want)
	}
}
