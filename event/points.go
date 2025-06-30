package event

import (
	"fmt"
	"strings"

	"github.com/mmcdole/heyemoji/database"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

type PointsHandler struct {
	db       database.Driver
	dailyCap int
}

func NewPointsHandler(dailyCap int, db database.Driver) PointsHandler {
	return PointsHandler{db: db, dailyCap: dailyCap}
}

func (h PointsHandler) Matches(msg *Message, client *socketmode.Client) bool {
	if msg == nil {
		return false
	}
	if !IsBotMentioned(msg, BotID) && !IsDirectMessage(msg) {
		return false
	}
	if strings.Contains(strings.ToLower(msg.Text), "points") {
		return true
	}
	return false
}

func (h PointsHandler) Execute(ev *Message, client *socketmode.Client) bool {

	given, _ := h.db.QueryKarmaGiven(ev.User, LastPointReset())
	balance := h.dailyCap - given

	timeTillReset := FmtDuration(TimeTillPointReset())
	msg := fmt.Sprintf("You have %d emoji points left to give today. Your points will reset in %s.", balance, timeTillReset)
	client.Client.PostMessage(ev.Channel, slack.MsgOptionText(msg, false))

	return true
}
