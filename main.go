package main

import (
	"context"
	"log"
	"strings"

	"github.com/bitovi/heyemoji/database/postgres"
	"github.com/bitovi/heyemoji/event"
	"github.com/bitovi/heyemoji/web"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	cfg := readConfig()
	ctx := context.Background()

	db := postgres.New(cfg.DatabaseURL)
	if err := db.Open(ctx); err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	api := slack.New(
		cfg.SlackBotToken,
		slack.OptionAppLevelToken(cfg.SlackAppToken),
	)
	client := socketmode.New(api)

	if cfg.TestMode {
		log.Println("TEST MODE ENABLED (HEY_TEST_MODE=true): daily give cap is disabled")
	}

	handlers := map[string]event.EventHandler{}
	register := func(h event.EventHandler) { handlers[h.Subcommand()] = h }
	register(event.PingHandler{})
	register(event.NewHelpHandler(cfg.SlackDailyCap, cfg.TestMode, cfg.SlackEmojiMap))
	register(event.NewPointsHandler(cfg.SlackDailyCap, cfg.TestMode, db))
	register(event.NewLeaderHandler(cfg.MaxLeaderEntries, db))
	register(event.NewGiveHandler(cfg.SlackEmojiMap, cfg.SlackDailyCap, cfg.TestMode, db))

	googleConfigured := cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" && cfg.SessionSecret != ""
	switch {
	case cfg.WebAuthDisabled:
		log.Println("WEB UI AUTH DISABLED (HEY_WEB_AUTH_DISABLED=true): the web UI is reachable by anyone, with no sign-in. For local testing only.")
		fallthrough
	case googleConfigured:
		webServer := web.New(web.Config{
			BaseURL:             cfg.BaseURL,
			SessionSecret:       cfg.SessionSecret,
			GoogleClientID:      cfg.GoogleClientID,
			GoogleClientSecret:  cfg.GoogleClientSecret,
			GoogleAllowedDomain: cfg.GoogleAllowedDomain,
			MaxLeaderEntries:    cfg.MaxLeaderEntries,
			AuthDisabled:        cfg.WebAuthDisabled,
		}, db, api)
		go func() {
			if err := webServer.Run(ctx, cfg.HTTPPort); err != nil {
				log.Printf("web server error: %v", err)
			}
		}()
	default:
		log.Println("web UI disabled: set HEY_GOOGLE_CLIENT_ID, HEY_GOOGLE_CLIENT_SECRET, and HEY_SESSION_SECRET to enable it (or HEY_WEB_AUTH_DISABLED=true for local testing without Google OAuth)")
	}

	go handleEvents(ctx, client, handlers)

	if err := client.Run(); err != nil {
		log.Fatalf("socket mode client error: %v", err)
	}
}

func handleEvents(ctx context.Context, client *socketmode.Client, handlers map[string]event.EventHandler) {
	for evt := range client.Events {
		switch evt.Type {
		case socketmode.EventTypeConnecting:
			log.Println("connecting to Slack...")
		case socketmode.EventTypeConnectionError:
			log.Println("connection error, retrying...")
		case socketmode.EventTypeConnected:
			log.Println("connected to Slack")
		case socketmode.EventTypeSlashCommand:
			cmd, ok := evt.Data.(slack.SlashCommand)
			if !ok {
				continue
			}
			if evt.Request != nil {
				client.Ack(*evt.Request)
			}
			go respond(ctx, client, handlers, cmd)
		}
	}
}

func respond(ctx context.Context, client *socketmode.Client, handlers map[string]event.EventHandler, cmd slack.SlashCommand) {
	subcommand, args := splitCommand(cmd.Text)

	h, ok := handlers[subcommand]
	if !ok {
		// The first word isn't a recognized subcommand - most likely someone typed
		// "/heybitovi @user :emoji: reason" and just skipped "give" (it's the default
		// action, so this saves a word). Assume give and hand it the whole original
		// text, not the post-split args, since what splitCommand treated as
		// "subcommand" (e.g. "@user") is actually part of give's own input.
		// (An empty command never reaches here - splitCommand already returns "help"
		// for that, which is a registered handler.)
		subcommand = "give"
		args = strings.TrimSpace(cmd.Text)
		h = handlers["give"]
	}

	if err := h.Execute(ctx, &client.Client, cmd, args); err != nil {
		log.Printf("error executing /heybitovi %s: %v", subcommand, err)
	}
}

// splitCommand splits "subcommand rest of args..." into its subcommand and the
// remaining argument text. Defaults to "help" when no subcommand is given.
func splitCommand(text string) (subcommand string, args string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "help", ""
	}
	parts := strings.SplitN(text, " ", 2)
	subcommand = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return subcommand, args
}
