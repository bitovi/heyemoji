package web

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bitovi/heyemoji/event"
)

// periodOption is one entry in a month/quarter/year picker dropdown, newest first.
type periodOption struct {
	Value string // e.g. "2026-09", "2026-Q3", "2026" - goes in the URL's "when" param
	Label string // e.g. "September 2026", "Q3 2026", "2026"
}

// resolvePickableWindow handles the "month"/"quarter"/"year" periods, which (unlike
// day/week/all) support picking a SPECIFIC past instance via a "when" query param and
// a secondary dropdown, not just "the current one". It bounds the dropdown to
// whatever actually has data (from the first-ever event through now), validates the
// requested "when" against that list (falling back to the current instance for an
// empty or invalid value), and resolves the concrete [since, until) window for it.
func (s *Server) resolvePickableWindow(ctx context.Context, period, when string, now time.Time) (since, until time.Time, options []periodOption, selectedWhen, label string, isCurrent bool, err error) {
	loc := event.BusinessLocation()
	nowLocal := now.In(loc)

	earliest, hasData, qerr := s.db.QueryEarliestEventTime(ctx)
	if qerr != nil {
		return time.Time{}, time.Time{}, nil, "", "", false, qerr
	}
	if !hasData {
		earliest = nowLocal
	}
	earliestLocal := earliest.In(loc)

	switch period {
	case "month":
		options = monthOptions(earliestLocal, nowLocal)
	case "quarter":
		options = quarterOptions(earliestLocal, nowLocal)
	case "year":
		options = yearOptions(earliestLocal, nowLocal)
	}

	selectedWhen = when
	valid := false
	for _, o := range options {
		if o.Value == selectedWhen {
			valid = true
			break
		}
	}
	if !valid && len(options) > 0 {
		selectedWhen = options[0].Value // built newest-first, so index 0 is the current one
	}

	switch period {
	case "month":
		t, perr := time.ParseInLocation("2006-01", selectedWhen, loc)
		if perr != nil {
			t = time.Date(nowLocal.Year(), nowLocal.Month(), 1, 0, 0, 0, 0, loc)
		}
		since, until = event.ResolveMonth(t.Year(), t.Month())
		isCurrent = t.Year() == nowLocal.Year() && t.Month() == nowLocal.Month()
		label = t.Format("January 2006")

	case "quarter":
		var year, quarter int
		if _, serr := fmt.Sscanf(selectedWhen, "%d-Q%d", &year, &quarter); serr != nil || quarter < 1 || quarter > 4 {
			year, quarter = nowLocal.Year(), event.QuarterOf(nowLocal.Month())
		}
		since, until = event.ResolveQuarter(year, quarter)
		isCurrent = year == nowLocal.Year() && quarter == event.QuarterOf(nowLocal.Month())
		label = fmt.Sprintf("Q%d %d", quarter, year)

	case "year":
		year, aerr := strconv.Atoi(selectedWhen)
		if aerr != nil {
			year = nowLocal.Year()
		}
		since, until = event.ResolveYear(year)
		isCurrent = year == nowLocal.Year()
		label = strconv.Itoa(year)
	}

	return since, until, options, selectedWhen, label, isCurrent, nil
}

// monthOptionCap/quarterOptionCap/yearOptionCap are safety limits on how far back a
// picker will offer, regardless of how old the earliest event is.
const (
	monthOptionCap   = 240 // 20 years
	quarterOptionCap = 80  // 20 years
	yearOptionCap    = 50
)

func monthOptions(earliest, now time.Time) []periodOption {
	loc := event.BusinessLocation()
	cursor := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	earliestMonth := time.Date(earliest.Year(), earliest.Month(), 1, 0, 0, 0, 0, loc)

	var opts []periodOption
	for len(opts) < monthOptionCap && !cursor.Before(earliestMonth) {
		opts = append(opts, periodOption{Value: cursor.Format("2006-01"), Label: cursor.Format("January 2006")})
		cursor = cursor.AddDate(0, -1, 0)
	}
	return opts
}

func quarterOptions(earliest, now time.Time) []periodOption {
	year, quarter := now.Year(), event.QuarterOf(now.Month())
	earliestYear, earliestQuarter := earliest.Year(), event.QuarterOf(earliest.Month())

	var opts []periodOption
	for len(opts) < quarterOptionCap {
		if year < earliestYear || (year == earliestYear && quarter < earliestQuarter) {
			break
		}
		opts = append(opts, periodOption{
			Value: fmt.Sprintf("%d-Q%d", year, quarter),
			Label: fmt.Sprintf("Q%d %d", quarter, year),
		})
		quarter--
		if quarter < 1 {
			quarter = 4
			year--
		}
	}
	return opts
}

func yearOptions(earliest, now time.Time) []periodOption {
	var opts []periodOption
	for y := now.Year(); y >= earliest.Year() && len(opts) < yearOptionCap; y-- {
		opts = append(opts, periodOption{Value: strconv.Itoa(y), Label: strconv.Itoa(y)})
	}
	return opts
}
