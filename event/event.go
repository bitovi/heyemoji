package event

import (
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack/socketmode"
)

type Message struct {
	User    string
	Text    string
	Channel string
	BotID   string
}

type EventHandler interface {
	Matches(*Message, *socketmode.Client) bool
	Execute(*Message, *socketmode.Client) bool
}

const (
	directChannelMarker = "D"
	userMentionFormat   = "<@%s>"
)

// BotID holds the authenticated bot user ID used to detect mentions.
// Set during startup to help identify the bot user in messages.
var BotID string
var BotName string

func IsBotMentioned(event *Message, botID string) bool {
	return strings.Contains(event.Text, fmt.Sprintf(userMentionFormat, botID))
}

func IsDirectMessage(event *Message) bool {
	return strings.HasPrefix(event.Channel, directChannelMarker)
}

func IsBotMessage(msg *Message) bool {
	if msg == nil {
		return true
	}
	return msg.BotID != ""
}

// Get last point reset time
func LastPointReset() time.Time {
	now := time.Now().UTC()
	return now.Truncate(24 * time.Hour)
}

// Get next point reset time
func NextPointReset() time.Time {
	return LastPointReset().Add(24 * time.Hour)
}

// Get time till the next emoji reset
func TimeTillPointReset() time.Duration {
	reset := NextPointReset()
	// Duration till reset
	return reset.Sub(time.Now())
}

// Format a time.Duration till karma reset for display to user
func FmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	hr := d / time.Hour
	d -= hr * time.Hour
	m := d / time.Minute
	return fmt.Sprintf("%2d hours and %2d minutes", hr, m)
}

func Filter(arr []string, cond func(string) bool) []string {
	result := []string{}
	for i := range arr {
		if cond(arr[i]) {
			result = append(result, arr[i])
		}
	}
	return result
}

func Map(vs []string, f func(string) string) []string {
	vsm := make([]string, len(vs))
	for i, v := range vs {
		vsm[i] = f(v)
	}
	return vsm
}

func Keys(m map[string]int) []string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	return keys
}
