package event

import (
	"context"

	"github.com/slack-go/slack"
)

type PingHandler struct{}

func (PingHandler) Subcommand() string { return "ping" }

func (PingHandler) Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error {
	_, err := client.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID, slack.MsgOptionText("pong", false))
	return err
}
