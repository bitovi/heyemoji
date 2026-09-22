package event

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bitovi/heyemoji/database"
	"github.com/slack-go/slack"
)

func NewLeaderHandler(maxLeaderEntries int, db database.Driver) LeaderHandler {
	return LeaderHandler{db: db, maxLeaderEntries: maxLeaderEntries}
}

type LeaderHandler struct {
	db               database.Driver
	maxLeaderEntries int
}

func (h LeaderHandler) Subcommand() string { return "leaderboard" }

func (h LeaderHandler) Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error {
	period := strings.ToLower(strings.TrimSpace(args))
	if period == "" {
		period = "month"
	}

	targetTime, header, ok := ResolvePeriod(period, time.Now())
	if !ok {
		_, err := client.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID, slack.MsgOptionText(
			fmt.Sprintf("Sorry, I don't recognize the period %q. Try one of: day, week, month, quarter, year, all.", period), false))
		return err
	}

	leaders, err := h.db.QueryLeaderboard(ctx, targetTime)
	if err != nil {
		return err
	}

	if len(leaders) == 0 {
		_, err := client.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID,
			slack.MsgOptionText("Nobody has given any emoji points yet!", false))
		return err
	}

	rank := rankMapStringInt(leaders)
	msg := fmt.Sprintf(">*%s*\n", header)
	for i := 0; i < len(rank) && i < h.maxLeaderEntries; i++ {
		name := rank[i]
		if uinfo, err := client.GetUserInfoContext(ctx, rank[i]); err == nil {
			name = uinfo.RealName
		}
		msg += fmt.Sprintf(">%d) %s `%d`\n", i+1, name, leaders[rank[i]])
	}
	msg += ">\n> You can view other leaderboards! :tada:\n> */heybitovi leaderboard <day|week|month|quarter|year|all>*"

	_, _, err = client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText(msg, false))
	return err
}

func rankMapStringInt(values map[string]int) []string {
	type kv struct {
		Key   string
		Value int
	}
	var ss []kv
	for k, v := range values {
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})
	ranked := make([]string, len(ss))
	for i, kv := range ss {
		ranked[i] = kv.Key
	}
	return ranked
}
