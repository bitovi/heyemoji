package web

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bitovi/heyemoji/event"
)

var leaderboardPeriods = []string{"day", "week", "month", "quarter", "year", "all"}

// pickablePeriods are the leaderboardPeriods that support a secondary "which one"
// dropdown (via resolvePickableWindow) rather than always meaning "the current one".
var pickablePeriods = map[string]bool{"month": true, "quarter": true, "year": true}

var currentPeriodHeaders = map[string]string{
	"month":   "This Month's Leaderboard",
	"quarter": "This Quarter's Leaderboard",
	"year":    "This Year's Leaderboard",
}

func isValidLeaderboardPeriod(period string) bool {
	for _, p := range leaderboardPeriods {
		if p == period {
			return true
		}
	}
	return false
}

type leaderboardRow struct {
	Rank   int
	Name   string
	Points int
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if !isValidLeaderboardPeriod(period) {
		period = "month"
	}
	now := time.Now()

	var header string
	var totals map[string]int
	var options []periodOption
	var selectedWhen string

	if pickablePeriods[period] {
		since, until, opts, when, label, isCurrent, err := s.resolvePickableWindow(r.Context(), period, r.URL.Query().Get("when"), now)
		if err != nil {
			log.Printf("web: resolvePickableWindow: %v", err)
			http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
			return
		}
		options, selectedWhen = opts, when
		if isCurrent {
			header = currentPeriodHeaders[period]
		} else {
			header = label + " Leaderboard"
		}

		totals, err = s.db.QueryLeaderboardRange(r.Context(), since, until)
		if err != nil {
			log.Printf("web: QueryLeaderboardRange: %v", err)
			http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
			return
		}
	} else { // "day", "week", "all" - no specific-instance picker
		since, h, _ := event.ResolvePeriod(period, now) // period is already validated above
		header = h

		var err error
		totals, err = s.db.QueryLeaderboard(r.Context(), since)
		if err != nil {
			log.Printf("web: QueryLeaderboard: %v", err)
			http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
			return
		}
	}

	type kv struct {
		user   string
		points int
	}
	sorted := make([]kv, 0, len(totals))
	for u, p := range totals {
		sorted = append(sorted, kv{u, p})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].points > sorted[j].points })

	maxEntries := s.cfg.MaxLeaderEntries
	if maxEntries <= 0 {
		maxEntries = len(sorted)
	}

	rows := make([]leaderboardRow, 0, len(sorted))
	for i, e := range sorted {
		if i >= maxEntries {
			break
		}
		rows = append(rows, leaderboardRow{
			Rank:   i + 1,
			Name:   s.resolveDisplayName(r.Context(), e.user),
			Points: e.points,
		})
	}

	s.render(w, "leaderboard.html", map[string]any{
		"Active":       "leaderboard",
		"Header":       header,
		"Period":       period,
		"Periods":      leaderboardPeriods,
		"Pickable":     pickablePeriods[period],
		"Options":      options,
		"SelectedWhen": selectedWhen,
		"Rows":         rows,
		"Email":        emailFromContext(r.Context()),
	})
}

type feedRow struct {
	FromName    string
	FromInitial string
	ToName      string
	EmojiGlyph  string
	Points      int
	Reason      string
	CreatedAt   time.Time
	ChannelName string
	ChannelURL  string
}

// initialOf returns the first rune of name, uppercased, for an avatar badge. Falls
// back to "?" for an empty name.
func initialOf(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[:1]))
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	events, err := s.db.QueryFeed(r.Context(), limit)
	if err != nil {
		log.Printf("web: QueryFeed: %v", err)
		http.Error(w, "failed to load feed", http.StatusInternalServerError)
		return
	}

	rows := make([]feedRow, 0, len(events))
	for _, ev := range events {
		fromName := s.resolveDisplayName(r.Context(), ev.From)
		channelName, channelURL := s.resolveChannel(r.Context(), ev.SourceChannelID)
		rows = append(rows, feedRow{
			FromName:    fromName,
			FromInitial: initialOf(fromName),
			ToName:      s.resolveDisplayName(r.Context(), ev.To),
			EmojiGlyph:  emojiGlyph(ev.Emoji),
			Points:      ev.Points,
			Reason:      ev.Reason,
			CreatedAt:   ev.CreatedAt.In(event.BusinessLocation()),
			ChannelName: channelName,
			ChannelURL:  channelURL,
		})
	}

	s.render(w, "feed.html", map[string]any{
		"Active": "feed",
		"Rows":   rows,
		"Email":  emailFromContext(r.Context()),
	})
}
