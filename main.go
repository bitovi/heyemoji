package main

import (
	"log"
	"os"

	"github.com/mmcdole/heyemoji/database"
	"github.com/mmcdole/heyemoji/event"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func main() {

	cfg := readConfig()
	db := database.NewSQLiteDriver(cfg.DatabasePath)

	if err := db.Open(); err != nil {
		log.Fatalf("Failed to open db: %v", err)
	}

	handlers := []event.EventHandler{
		event.PingHandler{},
		event.NewLeaderHandler(cfg.MaxLeaderEntries, db),
		event.NewHelpHandler(cfg.SlackDailyCap, cfg.SlackEmojiMap),
		event.NewPointsHandler(cfg.SlackDailyCap, db),
		event.NewEmojiHandler(cfg.SlackEmojiMap, cfg.SlackDailyCap, db),
	}

	appToken := cfg.SlackAppToken

	api := slack.New(
		cfg.SlackToken,
		slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)),
		slack.OptionAppLevelToken(appToken),
	)
	client := socketmode.New(
		api,
		socketmode.OptionDebug(true),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	auth, err := api.AuthTest()
	if err != nil {
		log.Fatalf("auth test failed: %v", err)
	}
	event.BotID = auth.UserID
	event.BotName = auth.User

	go client.Run()

	for evt := range client.Events {
		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				continue
			}
			client.Ack(*evt.Request)
			var msg *event.Message
			switch inner := eventsAPIEvent.InnerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				msg = &event.Message{Channel: inner.Channel, Text: inner.Text, User: inner.User}
			case *slackevents.MessageEvent:
				msg = &event.Message{Channel: inner.Channel, Text: inner.Text, User: inner.User, BotID: inner.BotID}
			default:
				continue
			}
			if event.IsBotMessage(msg) {
				continue
			}
			for _, h := range handlers {
				if !h.Matches(msg, client) {
					continue
				}
				if handled := h.Execute(msg, client); handled {
					break
				}
			}
		}
	}
}
