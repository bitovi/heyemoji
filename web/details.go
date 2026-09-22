package web

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/bitovi/heyemoji/database"
	"github.com/bitovi/heyemoji/event"
)

// maxTopEmojiSlots caps how many top emoji get computed - only the single favorite
// is shown (in the KPI row), so this stays at 1.
const maxTopEmojiSlots = 1

// maxDailyChartDays caps the daily bar chart's width for the "year" period, which
// otherwise would try to render 365 bars. It shows the most recent 30 days of
// whichever year is selected (capped at today for the current year). In "month" view
// it shows every day of that month instead (at most 31, still comfortably readable).
const maxDailyChartDays = 30

type dayBar struct {
	Label     string // "Sep 15"
	Total     int
	HeightPct int
	Emphasis  bool // this is the standout day within the visible window
}

type weekdayBar struct {
	Label     string // "Mon"
	Total     int
	HeightPct int
	Emphasis  bool
}

type leaderBar struct {
	Rank       int
	Name       string
	Total      int
	WidthPct   int
	ColorClass string // cat-1, cat-2, cat-3 - see partials.html
}

type emojiStat struct {
	EmojiGlyph string
	Count      int
}

// funAward is a single "who wins this superlative" card - The Midnight Oil Burner,
// The Gift Giver, etc.
type funAward struct {
	Title  string
	Winner string
	Detail string
}

func pluralize(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func (s *Server) handleDetails(w http.ResponseWriter, r *http.Request) {
	loc := event.BusinessLocation()
	now := time.Now().In(loc)
	dayKey := func(t time.Time) time.Time {
		t = t.In(loc)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	today := dayKey(now)

	period := r.URL.Query().Get("period")
	if period != "month" {
		period = "year" // default
	}

	since, until, options, selectedWhen, label, isCurrent, err := s.resolvePickableWindow(r.Context(), period, r.URL.Query().Get("when"), now)
	if err != nil {
		log.Printf("web: resolvePickableWindow: %v", err)
		http.Error(w, "failed to load details", http.StatusInternalServerError)
		return
	}

	periodLabel := "this " + period
	if !isCurrent {
		periodLabel = "in " + label
	}

	var chartStart time.Time
	var chartDays int
	var dailyChartTitle string
	if period == "month" {
		chartStart = since
		chartDays = int(until.Sub(since).Hours() / 24) // exact number of days in that month
		dailyChartTitle = "Points given per day — " + periodLabel
	} else {
		chartEnd := until.AddDate(0, 0, -1)
		if chartEnd.After(today) {
			chartEnd = today
		}
		chartDays = maxDailyChartDays
		chartStart = chartEnd.AddDate(0, 0, -(maxDailyChartDays - 1))
		dailyChartTitle = fmt.Sprintf("Points given per day — last %d days", maxDailyChartDays)
	}

	// QueryEventsSince only bounds the lower end, so filter the upper end (until) in
	// Go - needed so viewing a specific past month/year doesn't pull in everything
	// from then through today.
	allEvents, err := s.db.QueryEventsSince(r.Context(), since)
	if err != nil {
		log.Printf("web: QueryEventsSince: %v", err)
		http.Error(w, "failed to load details", http.StatusInternalServerError)
		return
	}
	events := make([]database.KarmaEvent, 0, len(allEvents))
	for _, ev := range allEvents {
		if ev.CreatedAt.Before(until) {
			events = append(events, ev)
		}
	}

	// --- Per-day totals across the whole selected period (used for both the daily
	// chart and "best single day"; for "month" these cover the same range, for "year"
	// the chart is only the most recent 30 days but "best day" looks at the whole
	// year). ---
	periodDailyTotals := map[time.Time]int{}
	chartDailyTotals := map[time.Time]int{}
	weekdayTotals := [7]int{} // index 0 = Monday, per the same Mon-first convention event.ResolvePeriod uses
	emojiCounts := map[string]int{}
	leaderTotals := map[string]int{}
	giverCounts := map[string]int{}
	totalPointsInPeriod := 0

	var latestTimeOfDayEvent database.KarmaEvent
	latestTimeOfDaySeconds := -1

	// "The Points Shark" award: who named the most distinct people in one
	// /heybitovi give. All rows written by a single give share the exact same
	// created_at (GiveKarma inserts them in one DB transaction, and Postgres's now()
	// is the transaction start time, constant across every statement in it), so
	// grouping by (from_user, created_at) recovers "which events came from the same
	// give command" without needing a column dedicated to it.
	type giveGroupKey struct {
		from string
		ts   int64
	}
	groupRecipients := map[giveGroupKey]map[string]bool{}

	// "The Beloved" award: who received recognition from the most distinct givers
	// (breadth of admirers, not total points).
	receivedFrom := map[string]map[string]bool{}

	for _, ev := range events {
		d := dayKey(ev.CreatedAt)
		periodDailyTotals[d] += ev.Points
		if !d.Before(chartStart) {
			chartDailyTotals[d] += ev.Points
		}
		local := ev.CreatedAt.In(loc)
		weekdayTotals[(int(local.Weekday())+6)%7] += ev.Points
		emojiCounts[ev.Emoji]++
		leaderTotals[ev.To] += ev.Points
		giverCounts[ev.From]++
		totalPointsInPeriod += ev.Points

		// "The Midnight Oil Burner" - whoever gave recognition at the latest clock
		// time in the period (time-of-day, not most-recent date).
		secondsOfDay := local.Hour()*3600 + local.Minute()*60 + local.Second()
		if secondsOfDay > latestTimeOfDaySeconds {
			latestTimeOfDaySeconds = secondsOfDay
			latestTimeOfDayEvent = ev
		}

		gk := giveGroupKey{from: ev.From, ts: ev.CreatedAt.UnixNano()}
		if groupRecipients[gk] == nil {
			groupRecipients[gk] = map[string]bool{}
		}
		groupRecipients[gk][ev.To] = true

		if receivedFrom[ev.To] == nil {
			receivedFrom[ev.To] = map[string]bool{}
		}
		receivedFrom[ev.To][ev.From] = true
	}

	// --- Daily bar chart (bar chart, emphasis on the local peak) ---
	maxDaily := 0
	dayTotalsOrdered := make([]int, chartDays)
	dayLabels := make([]string, chartDays)
	for i := 0; i < chartDays; i++ {
		d := chartStart.AddDate(0, 0, i)
		total := chartDailyTotals[d]
		dayTotalsOrdered[i] = total
		dayLabels[i] = d.Format("Jan 2")
		if total > maxDaily {
			maxDaily = total
		}
	}
	dayBars := make([]dayBar, chartDays)
	for i, total := range dayTotalsOrdered {
		heightPct := 0
		if maxDaily > 0 {
			heightPct = total * 100 / maxDaily
		}
		dayBars[i] = dayBar{
			Label:     dayLabels[i],
			Total:     total,
			HeightPct: heightPct,
			Emphasis:  total == maxDaily && maxDaily > 0,
		}
	}

	// --- Best single day in the period ---
	var bestDay time.Time
	bestDayTotal := 0
	for d, total := range periodDailyTotals {
		if total > bestDayTotal {
			bestDayTotal = total
			bestDay = d
		}
	}
	bestDayLabel := ""
	if !bestDay.IsZero() {
		bestDayLabel = bestDay.Format("Monday, Jan 2")
	}

	// --- Busiest day of week (bar chart, emphasis on the peak weekday) ---
	weekdayNames := [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	maxWeekday := 0
	for _, total := range weekdayTotals {
		if total > maxWeekday {
			maxWeekday = total
		}
	}
	weekdayBars := make([]weekdayBar, 7)
	for i, total := range weekdayTotals {
		heightPct := 0
		if maxWeekday > 0 {
			heightPct = total * 100 / maxWeekday
		}
		weekdayBars[i] = weekdayBar{
			Label:     weekdayNames[i],
			Total:     total,
			HeightPct: heightPct,
			Emphasis:  total == maxWeekday && maxWeekday > 0,
		}
	}

	// --- Top 3 leaders in the period (categorical bars - see partials.html for the
	// validated 3-color theme) ---
	type kv struct {
		user   string
		points int
	}
	sortedLeaders := make([]kv, 0, len(leaderTotals))
	for u, p := range leaderTotals {
		sortedLeaders = append(sortedLeaders, kv{u, p})
	}
	sort.Slice(sortedLeaders, func(i, j int) bool { return sortedLeaders[i].points > sortedLeaders[j].points })

	colorClasses := []string{"cat-1", "cat-2", "cat-3"}
	maxLeaderTotal := 0
	if len(sortedLeaders) > 0 {
		maxLeaderTotal = sortedLeaders[0].points
	}
	leaderBars := make([]leaderBar, 0, 3)
	for i, kv := range sortedLeaders {
		if i >= 3 {
			break
		}
		widthPct := 0
		if maxLeaderTotal > 0 {
			widthPct = kv.points * 100 / maxLeaderTotal
		}
		leaderBars = append(leaderBars, leaderBar{
			Rank:       i + 1,
			Name:       s.resolveDisplayName(r.Context(), kv.user),
			Total:      kv.points,
			WidthPct:   widthPct,
			ColorClass: colorClasses[i],
		})
	}

	// --- Top emoji in the period ---
	type ekv struct {
		emoji string
		count int
	}
	sortedEmoji := make([]ekv, 0, len(emojiCounts))
	for e, c := range emojiCounts {
		sortedEmoji = append(sortedEmoji, ekv{e, c})
	}
	sort.Slice(sortedEmoji, func(i, j int) bool { return sortedEmoji[i].count > sortedEmoji[j].count })

	topEmoji := make([]emojiStat, 0, maxTopEmojiSlots)
	for i, e := range sortedEmoji {
		if i >= maxTopEmojiSlots {
			break
		}
		topEmoji = append(topEmoji, emojiStat{EmojiGlyph: emojiGlyph(e.emoji), Count: e.count})
	}

	// --- Fun awards: The Midnight Oil Burner, The Gift Giver, The Points Shark ---
	var awards []funAward
	if latestTimeOfDaySeconds >= 0 {
		local := latestTimeOfDayEvent.CreatedAt.In(loc)
		awards = append(awards, funAward{
			Title:  "The Midnight Oil Burner",
			Winner: s.resolveDisplayName(r.Context(), latestTimeOfDayEvent.From),
			Detail: fmt.Sprintf("Gave recognition at %s on %s", local.Format("3:04 PM"), local.Format("Jan 2")),
		})
	}
	bestGiver, bestGiverCount := "", 0
	for user, count := range giverCounts {
		if count > bestGiverCount {
			bestGiver, bestGiverCount = user, count
		}
	}
	if bestGiver != "" {
		awards = append(awards, funAward{
			Title:  "The Gift Giver",
			Winner: s.resolveDisplayName(r.Context(), bestGiver),
			Detail: fmt.Sprintf("Submitted %d %s %s", bestGiverCount, pluralize(bestGiverCount, "recognition"), periodLabel),
		})
	}

	var mostDistributedGiver string
	mostDistributedCount := 0
	for gk, recipients := range groupRecipients {
		if len(recipients) > mostDistributedCount {
			mostDistributedCount = len(recipients)
			mostDistributedGiver = gk.from
		}
	}
	if mostDistributedCount > 1 { // only interesting if someone actually named more than one person at once
		awards = append(awards, funAward{
			Title:  "The Points Shark",
			Winner: s.resolveDisplayName(r.Context(), mostDistributedGiver),
			Detail: fmt.Sprintf("Recognized %d people in a single give", mostDistributedCount),
		})
	}

	var mostBeloved string
	mostBelovedCount := 0
	for user, givers := range receivedFrom {
		if len(givers) > mostBelovedCount {
			mostBelovedCount = len(givers)
			mostBeloved = user
		}
	}
	if mostBeloved != "" {
		peopleWord := "person"
		if mostBelovedCount != 1 {
			peopleWord = "people" // pluralize() assumes a regular "+s" plural, which doesn't work for "person"
		}
		awards = append(awards, funAward{
			Title:  "The Beloved",
			Winner: s.resolveDisplayName(r.Context(), mostBeloved),
			Detail: fmt.Sprintf("Recognized by %d different %s %s", mostBelovedCount, peopleWord, periodLabel),
		})
	}

	s.render(w, "details.html", map[string]any{
		"Active":              "details",
		"Email":               emailFromContext(r.Context()),
		"Period":              period,
		"PeriodLabel":         periodLabel,
		"Options":             options,
		"SelectedWhen":        selectedWhen,
		"DailyChartTitle":     dailyChartTitle,
		"TotalRecognitions":   len(events),
		"TotalPointsInPeriod": totalPointsInPeriod,
		"BestDayLabel":        bestDayLabel,
		"BestDayTotal":        bestDayTotal,
		"Awards":              awards,
		"DayBars":             dayBars,
		"WeekdayBars":         weekdayBars,
		"LeaderBars":          leaderBars,
		"TopEmoji":            topEmoji,
	})
}
