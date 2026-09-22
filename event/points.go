package event

import (
	"context"
	"fmt"

	"github.com/bitovi/heyemoji/database"
	"github.com/slack-go/slack"
)

type PointsHandler struct {
	db       database.Driver
	dailyCap int
	testMode bool
}

func NewPointsHandler(dailyCap int, testMode bool, db database.Driver) PointsHandler {
	return PointsHandler{db: db, dailyCap: dailyCap, testMode: testMode}
}

func (h PointsHandler) Subcommand() string { return "points" }

func (h PointsHandler) Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error {
	var msg string
	if h.testMode {
		msg = "You have unlimited emoji points to give today (test mode)."
	} else {
		given, err := h.db.QueryKarmaGiven(ctx, cmd.UserID, LastPointReset())
		if err != nil {
			return err
		}
		balance := h.dailyCap - given
		msg = fmt.Sprintf("You have %d emoji points left to give today. Your points will reset in %s.",
			balance, FmtDuration(TimeTillPointReset()))
	}

	_, err := client.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID, slack.MsgOptionText(msg, false))
	return err
}
