package main

import (
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	BotName          string
	DatabaseURL      string
	SlackBotToken    string
	SlackAppToken    string
	SlackEmoji       string
	SlackEmojiMap    map[string]int
	SlackDailyCap    int
	MaxLeaderEntries int
	// TestMode, when true, disables the daily give cap entirely (everyone has
	// unlimited points) so testing doesn't get blocked by running out for the day.
	// Not meant for production use - every place the cap is shown to a user says
	// "unlimited (test mode)" instead of a number while this is on, so it's never
	// ambiguous which mode is running.
	TestMode bool

	// Web UI config.
	HTTPPort            string
	BaseURL             string
	SessionSecret       string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleAllowedDomain string // if set, only Google accounts on this Workspace domain (the "hd" claim) may sign in
	// WebAuthDisabled, when true, skips Google Sign-In entirely and serves the web UI
	// pages to anyone - for local testing before Google OAuth credentials exist, or
	// when you just don't want to click through a login while iterating. Not meant
	// for any deployment reachable by anyone but you.
	WebAuthDisabled bool
}

func readConfig() *Config {
	viper.SetEnvPrefix("hey")
	viper.AutomaticEnv()

	// DATABASE_URL is the one exception to the HEY_ prefix: Bitovi Platform's
	// Postgres dependency injects a connection string under this exact,
	// unprefixed name (see deploy/values.yaml's `dependencies.db` +
	// `workloads.main.uses: [db]`), and that name isn't configurable on the
	// platform side - so the app reads it directly rather than expecting
	// HEY_DATABASE_URL.
	_ = viper.BindEnv("database_url", "DATABASE_URL")

	viper.SetDefault("bot_name", "HeyBitovi")
	viper.SetDefault("database_url", "")
	viper.SetDefault("slack_bot_token", "")
	viper.SetDefault("slack_app_token", "")
	viper.SetDefault("slack_emoji", "star:1")
	viper.SetDefault("slack_daily_cap", 5)
	viper.SetDefault("max_leader_entries", 10)
	viper.SetDefault("test_mode", false)
	viper.SetDefault("http_port", "8080")
	viper.SetDefault("base_url", "http://localhost:8080")
	viper.SetDefault("session_secret", "")
	viper.SetDefault("google_client_id", "")
	viper.SetDefault("google_client_secret", "")
	viper.SetDefault("google_allowed_domain", "")
	viper.SetDefault("web_auth_disabled", false)

	c := &Config{
		BotName:             viper.GetString("bot_name"),
		DatabaseURL:         viper.GetString("database_url"),
		SlackBotToken:       viper.GetString("slack_bot_token"),
		SlackAppToken:       viper.GetString("slack_app_token"),
		SlackEmoji:          viper.GetString("slack_emoji"),
		SlackDailyCap:       viper.GetInt("slack_daily_cap"),
		MaxLeaderEntries:    viper.GetInt("max_leader_entries"),
		TestMode:            viper.GetBool("test_mode"),
		HTTPPort:            viper.GetString("http_port"),
		BaseURL:             strings.TrimSuffix(viper.GetString("base_url"), "/"),
		SessionSecret:       viper.GetString("session_secret"),
		GoogleClientID:      viper.GetString("google_client_id"),
		GoogleClientSecret:  viper.GetString("google_client_secret"),
		GoogleAllowedDomain: viper.GetString("google_allowed_domain"),
		WebAuthDisabled:     viper.GetBool("web_auth_disabled"),
	}

	c.SlackEmojiMap = createEmojiValueMap(c.SlackEmoji)

	return c
}

func createEmojiValueMap(e string) map[string]int {
	pairs := strings.Split(e, ",")

	evalues := make(map[string]int)
	for _, pair := range pairs {
		evalue := strings.Split(pair, ":")

		if len(evalue) != 2 {
			continue
		}

		emoji := evalue[0]
		if karma, err := strconv.Atoi(evalue[1]); err == nil {
			evalues[emoji] = karma
		}
	}

	return evalues
}
