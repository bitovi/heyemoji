package event

import (
	"bytes"
	"context"
	"strconv"
	"text/template"

	"github.com/slack-go/slack"
)

type HelpHandler struct {
	dailyCap int
	testMode bool
	emojiMap map[string]int
}

func NewHelpHandler(dailyCap int, testMode bool, emojiMap map[string]int) HelpHandler {
	return HelpHandler{dailyCap: dailyCap, testMode: testMode, emojiMap: emojiMap}
}

func (h HelpHandler) Subcommand() string { return "help" }

func (h HelpHandler) Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error {
	tmp := `>*Directions*
>Give someone recognition with */heybitovi give @username :{{index .Emoji 0}}: because they crushed it*. Everyone has {{.DailyCap}} emoji points to give out per day.
>*Recognition Emoji*
>{{ range $key, $value := .EmojiMap }}:{{$key}}: *({{$value}} pts)*  {{end}}
>*Commands*
>/heybitovi give @username :emoji: reason: give someone recognition
>/heybitovi leaderboard <day|week|month|quarter|year|all>: see the top point earners for a period
>/heybitovi points: see how many emoji points you have left to give
>/heybitovi help: show this message`

	t := template.Must(template.New("help").Parse(tmp))

	dailyCapText := strconv.Itoa(h.dailyCap)
	if h.testMode {
		dailyCapText = "unlimited (test mode)"
	}

	var helpStr bytes.Buffer
	if err := t.Execute(&helpStr, struct {
		Emoji    []string
		EmojiMap map[string]int
		DailyCap string
	}{
		Keys(h.emojiMap),
		h.emojiMap,
		dailyCapText,
	}); err != nil {
		return err
	}

	_, err := client.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID, slack.MsgOptionText(helpStr.String(), false))
	return err
}
