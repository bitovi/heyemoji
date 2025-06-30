package event

import (
	"fmt"
	"github.com/slack-go/slack"
	"strings"

	"github.com/slack-go/slack/socketmode"
)

type PingHandler struct{}

func (p PingHandler) Matches(msg *Message, client *socketmode.Client) bool {
	if msg == nil {
		return false
	}
	if !IsBotMentioned(msg, BotID) && !IsDirectMessage(msg) {
		return false
	}
	if strings.Contains(strings.ToLower(msg.Text), "ping") {
		return true
	}
	return false
}

func (p PingHandler) Execute(msg *Message, client *socketmode.Client) bool {
	if msg == nil {
		return false
	}

	fmt.Println("EXECUTE PING START")
	fmt.Printf("Channel: %s\n", msg.Channel)
	client.Client.PostMessage(msg.Channel, slack.MsgOptionText("pong", false))

	return true
}
