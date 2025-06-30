package event

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

type HelpHandler struct {
	dailyCap int
	emojiMap map[string]int
}

func NewHelpHandler(dailyCap int, emojiMap map[string]int) HelpHandler {
	return HelpHandler{dailyCap: dailyCap, emojiMap: emojiMap}
}

func (h HelpHandler) Matches(msg *Message, client *socketmode.Client) bool {
	if msg == nil {
		return false
	}
	if !IsBotMentioned(msg, BotID) && !IsDirectMessage(msg) {
		return false
	}
	if strings.Contains(strings.ToLower(msg.Text), "help") {
		return true
	}
	return false
}

func (h HelpHandler) Execute(e *Message, client *socketmode.Client) bool {
	tmp := `>*Directions*
>Add a recognition emoji after someone's username like this: *@username Great job!* :{{index .Emoji 0}}:. Everyone has {{.DailyCap}} emoji points to give out per day and can only give them in the channels I've been invited to.
>*Recognition Emoji*
>{{ range $key, $value := .EmojiMap }}:{{$key}}: *({{$value}} pts)*  {{end}}
>*Channel Commands*
>/invite <@{{.Botname}}>: to invite me to channels
><@{{.Botname}}> leaderboard <day|week|month>: to see the top 10 people on your leaderboard
><@{{.Botname}}> points: see how many emoji points you have left to give 
><@{{.Botname}}> help: get help with how to send recognition emoji 
>*Direct Message Commands*
>leaderboard <day|week|month>: to see the top 10 people on your leaderboard
>points: see how many emoji points you have left to give 
>help: get help with how to send recognition emoji`

	t := template.Must(template.New("help").Parse(tmp))

	var helpStr bytes.Buffer
	t.Execute(&helpStr, struct {
		Botname  string
		Emoji    []string
		EmojiMap map[string]int
		DailyCap int
	}{
		BotName,
		Keys(h.emojiMap),
		h.emojiMap,
		h.dailyCap,
	})

	client.Client.PostMessage(e.Channel, slack.MsgOptionText(helpStr.String(), false))

	return true
}
