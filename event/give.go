package event

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/bitovi/heyemoji/database"
	"github.com/slack-go/slack"
)

// userRegex matches Slack's mention markup, e.g. <@U12345> or <@U12345|username>. This
// is what Slack sends when someone picks a user from the @mention autocomplete
// suggestion in the slash command box.
var userRegex = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|[^>]*)?>`)

// wordTokenRegex splits text on whitespace for plain-@mention scanning.
var wordTokenRegex = regexp.MustCompile(`\S+`)

// trailingPunctRegex strips punctuation someone might type right after a name, e.g.
// "@David Nicholas," or "@David Nicholas:", so it isn't treated as part of the name.
var trailingPunctRegex = regexp.MustCompile(`[,.:;!?]+$`)

// emojiTokenRegex matches a colon-wrapped emoji token like ":star:", used as a
// boundary that stops a name candidate from growing past it.
var emojiTokenRegex = regexp.MustCompile(`^:[^:\s]+:$`)

// maxPlainMentionWords bounds how many words after a bare "@" we'll try joining into
// one name candidate (e.g. "@David Nicholas" -> "David Nicholas"), so a first-plus-last
// name resolves without scanning the entire rest of the message as one candidate.
const maxPlainMentionWords = 4

func NewGiveHandler(emoji map[string]int, dailyCap int, testMode bool, db database.Driver) *GiveHandler {
	return &GiveHandler{emoji: emoji, dailyCap: dailyCap, testMode: testMode, db: db}
}

type GiveHandler struct {
	emoji    map[string]int
	dailyCap int
	testMode bool
	db       database.Driver

	mu        sync.Mutex
	userCache map[string]string // lowercased username/display name -> user ID, lazily loaded
}

func (h *GiveHandler) Subcommand() string { return "give" }

// effectiveDailyCap is h.dailyCap normally, or UnlimitedDailyCap when test mode is on.
func (h *GiveHandler) effectiveDailyCap() int {
	if h.testMode {
		return UnlimitedDailyCap
	}
	return h.dailyCap
}

// Execute parses "@user :emoji: optional reason text" out of args, gives the
// recognized users the corresponding emoji points (deducted once from the giver's
// daily balance per emoji per recipient), and announces the recognition publicly in
// the channel the command was run from - unless that "channel" is a DM with the bot,
// which has nowhere public to announce into, so the announcement is skipped entirely
// there. The giver always gets a DM confirming the points were recorded, regardless of
// whether (or why) the public announcement didn't post - DMs don't require channel
// membership the way chat.postMessage/postEphemeral into cmd.ChannelID do, so this is
// the one confirmation path that works everywhere.
func (h *GiveHandler) Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error {
	isDM := IsDirectMessage(cmd.ChannelID)

	emojis := h.parseEmojis(args)
	if len(emojis) == 0 {
		log.Printf("give: no known emoji found in args %q", args)
		return h.replyEphemeral(ctx, client, cmd, h.usageMessage())
	}

	users := h.parseUsers(args)

	// Fall back to resolving plain "@Some Name" text (not real mention markup)
	// against the workspace's user list, and remember which literal tokens resolved
	// so they can be stripped out of the reason text below.
	plainIDs, resolvedTokens := h.resolvePlainMentions(ctx, client, args)
	users = append(users, plainIDs...)

	gaveSelfKarma := false
	recipients := make([]string, 0, len(users))
	for _, u := range users {
		if u == cmd.UserID {
			gaveSelfKarma = true
			continue
		}
		recipients = append(recipients, u)
	}

	if len(recipients) == 0 {
		if gaveSelfKarma {
			return h.replyEphemeral(ctx, client, cmd, "Sorry, you can only give emoji points to other people on your team.")
		}
		log.Printf("give: no @mentions found in args %q (parsed users: %v)", args, users)
		return h.replyEphemeral(ctx, client, cmd, h.usageMessage())
	}

	reason := h.parseReason(args, resolvedTokens)
	since := LastPointReset() // also the start of "today" - the recognition-thread grouping window

	// Look up each recipient's already-open recognition thread for today, if any,
	// before inserting so we know up front which rows can be threaded immediately
	// versus which belong to a brand-new announcement (whose ts we won't know until
	// after we post it - see the backfill loop below).
	existingThreadTS := make(map[string]string, len(recipients))
	for _, u := range recipients {
		if ts, ok, err := h.db.LatestThreadTS(ctx, cmd.ChannelID, u, since); err != nil {
			return err
		} else if ok {
			existingThreadTS[u] = ts
		}
	}

	events := make([]database.KarmaEvent, 0, len(recipients)*len(emojis))
	batchTotal := 0
	for _, e := range emojis {
		points := h.emoji[e]
		batchTotal += points * len(recipients)
		for _, u := range recipients {
			events = append(events, database.KarmaEvent{
				From:      cmd.UserID,
				To:        u,
				Emoji:     e,
				Points:    points,
				Reason:    reason,
				ChannelID: cmd.ChannelID,
				ThreadTS:  existingThreadTS[u],
			})
		}
	}

	inserted, remaining, err := h.db.GiveKarma(ctx, events, since, h.effectiveDailyCap())
	if err == database.ErrDailyCapExceeded {
		given, qerr := h.db.QueryKarmaGiven(ctx, cmd.UserID, since)
		if qerr != nil {
			return qerr
		}
		balance := h.dailyCap - given
		return h.replyEphemeral(ctx, client, cmd, fmt.Sprintf(
			"Whoops! You tried to give *%d* emoji point(s). You have *%d* point(s) left to give today. "+
				"Your point balance will reset in *%s*.",
			batchTotal, balance, FmtDuration(TimeTillPointReset())))
	}
	if err != nil {
		return err
	}

	// One announcement per recipient (not one combined message) so each person's
	// recognition thread stays theirs: a give naming multiple people posts multiple
	// messages, each threaded onto (or starting) that specific person's thread for
	// today.
	byRecipient := make(map[string][]database.KarmaEvent, len(recipients))
	for _, ev := range inserted {
		byRecipient[ev.To] = append(byRecipient[ev.To], ev)
	}

	var failedAnnouncements []string // recipient user IDs whose public announcement didn't post
	if !isDM {
		for _, u := range recipients {
			if err := h.announce(ctx, client, cmd, u, byRecipient[u], existingThreadTS[u]); err != nil {
				log.Printf("give: failed to post announcement for recipient %s: %v", u, err)
				failedAnnouncements = append(failedAnnouncements, u)
			}
		}
	}

	mentions := Map(recipients, func(u string) string { return fmt.Sprintf("<@%s>", u) })
	var confirmation string
	if h.testMode {
		confirmation = fmt.Sprintf("%s received recognition from you. (test mode: unlimited points)", strings.Join(mentions, " "))
	} else {
		confirmation = fmt.Sprintf("%s received recognition from you. You have *%d point(s)* left to give out today.",
			strings.Join(mentions, " "), remaining)
	}
	if gaveSelfKarma {
		confirmation += " (Note: you can't give points to yourself, so that part was skipped.)"
	}
	if isDM {
		confirmation += "\n\n(No public recognition message was posted, since this was a DM - only you can see this. Run `/heybitovi give` from a channel instead if you want the team to see it.)"
	} else if len(failedAnnouncements) > 0 {
		failedMentions := Map(failedAnnouncements, func(u string) string { return fmt.Sprintf("<@%s>", u) })
		confirmation += fmt.Sprintf(
			"\n\n:warning: I couldn't post the public recognition message in <#%s> for %s - I'm probably not a member of that channel (common for private channels, which I can never join automatically). The points were still given; `/invite @HeyBitovi` to that channel to get public announcements there too.",
			cmd.ChannelID, strings.Join(failedMentions, " "))
	}
	return h.dmUser(ctx, client, cmd.UserID, confirmation)
}

// announce posts the public recognition message for one recipient's slice of newly
// inserted events, threading it onto existingTS if the recipient already had an open
// thread today, or starting a fresh top-level message and backfilling its ts onto
// those rows otherwise.
func (h *GiveHandler) announce(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, recipient string, events []database.KarmaEvent, existingTS string) error {
	if len(events) == 0 {
		return nil
	}

	emojiSeen := map[string]bool{}
	var emojiText []string
	var reason string
	ids := make([]string, 0, len(events))
	for _, ev := range events {
		if !emojiSeen[ev.Emoji] {
			emojiSeen[ev.Emoji] = true
			emojiText = append(emojiText, fmt.Sprintf(":%s:", ev.Emoji))
		}
		reason = ev.Reason
		ids = append(ids, ev.ID)
	}

	text := fmt.Sprintf("%s <@%s> gave recognition to <@%s>", strings.Join(emojiText, " "), cmd.UserID, recipient)
	if reason != "" {
		text += fmt.Sprintf(": %s", reason)
	}

	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if existingTS != "" {
		opts = append(opts, slack.MsgOptionTS(existingTS))
	}

	_, ts, err := client.PostMessageContext(ctx, cmd.ChannelID, opts...)
	if err != nil {
		return err
	}

	if existingTS == "" {
		// This was a brand-new top-level message - it becomes today's thread root for
		// this recipient, so backfill it onto the rows we just inserted.
		if err := h.db.SetThreadTS(ctx, ids, ts); err != nil {
			return fmt.Errorf("posted but failed to record thread root: %w", err)
		}
	}
	return nil
}

func (h *GiveHandler) replyEphemeral(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, msg string) error {
	_, err := client.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID, slack.MsgOptionText(msg, false))
	return err
}

// dmUser sends userID a direct message. chat.postMessage auto-opens the DM channel on
// first use - no separate conversations.open call or extra OAuth scope needed - and
// unlike posting into cmd.ChannelID, it works regardless of whether the bot has ever
// been added to any channel the user is in.
func (h *GiveHandler) dmUser(ctx context.Context, client *slack.Client, userID, msg string) error {
	_, _, err := client.PostMessageContext(ctx, userID, slack.MsgOptionText(msg, false))
	return err
}

func (h *GiveHandler) usageMessage() string {
	example := "star"
	for e := range h.emoji {
		example = e
		break
	}
	return fmt.Sprintf("Give someone recognition like this: `/heybitovi give @username :%s: because they crushed it`", example)
}

func (h *GiveHandler) emojiRegex() *regexp.Regexp {
	tokens := make([]string, 0, len(h.emoji))
	for k := range h.emoji {
		tokens = append(tokens, regexp.QuoteMeta(fmt.Sprintf(":%s:", k)))
	}
	return regexp.MustCompile(strings.Join(tokens, "|"))
}

func (h *GiveHandler) parseEmojis(text string) []string {
	matches := h.emojiRegex().FindAllString(text, -1)
	for i, m := range matches {
		matches[i] = strings.Trim(m, ":")
	}
	return matches
}

func (h *GiveHandler) parseUsers(text string) []string {
	matches := userRegex.FindAllStringSubmatch(text, -1)
	users := make([]string, 0, len(matches))
	for _, m := range matches {
		users = append(users, m[1])
	}
	return users
}

// plainMentionCandidate is one "try resolving this phrase" option for a single "@..."
// occurrence in a message - candidatesForMention returns these longest-first so the
// caller can take the longest one that actually resolves to a real user (so "@David
// Nicholas" is tried as "David Nicholas" before falling back to just "David").
type plainMentionCandidate struct {
	name  string // the phrase to look up, e.g. "David Nicholas"
	token string // the exact literal substring it came from, e.g. "@David Nicholas" - what to strip from the reason if this candidate is the one that resolves
}

// candidatesForMention finds every "@..." occurrence in text that isn't part of real
// <@...> mention markup, and for each one returns its resolution candidates
// (1..maxPlainMentionWords words after the @, longest first, trailing punctuation like
// a comma stripped). Pure and independent of any Slack API call, so it's unit-testable
// on its own; resolvePlainMentions does the actual lookups.
func candidatesForMention(text string) [][]plainMentionCandidate {
	withoutMarkup := userRegex.ReplaceAllString(text, "")
	tokenIdx := wordTokenRegex.FindAllStringIndex(withoutMarkup, -1)

	var occurrences [][]plainMentionCandidate
	for i := 0; i < len(tokenIdx); i++ {
		start := tokenIdx[i][0]
		if withoutMarkup[start] != '@' || tokenIdx[i][1]-start < 2 {
			continue
		}

		var candidates []plainMentionCandidate
		for j := i; j < len(tokenIdx) && j < i+maxPlainMentionWords; j++ {
			if j > i {
				word := withoutMarkup[tokenIdx[j][0]:tokenIdx[j][1]]
				if strings.HasPrefix(word, "@") || emojiTokenRegex.MatchString(word) {
					break // an emoji token or another mention can't be part of this name
				}
			}
			end := tokenIdx[j][1]
			raw := withoutMarkup[start+1 : end]
			if loc := trailingPunctRegex.FindStringIndex(raw); loc != nil {
				end -= len(raw) - loc[0]
				raw = raw[:loc[0]]
			}
			if raw == "" {
				continue
			}
			candidates = append(candidates, plainMentionCandidate{name: raw, token: withoutMarkup[start:end]})
		}

		// Longest phrase first, so the caller stops at the first (longest) match.
		for l, r := 0, len(candidates)-1; l < r; l, r = l+1, r-1 {
			candidates[l], candidates[r] = candidates[r], candidates[l]
		}
		occurrences = append(occurrences, candidates)
	}
	return occurrences
}

// resolvePlainMentions resolves every plain "@Some Name" occurrence in text (not real
// mention markup) against the workspace's user list, taking the longest word sequence
// that matches a real user for each occurrence. Returns the resolved user IDs and the
// literal "@..." substrings that matched, so callers can strip them out of the reason
// text.
func (h *GiveHandler) resolvePlainMentions(ctx context.Context, client *slack.Client, text string) (ids []string, tokens []string) {
	for _, candidates := range candidatesForMention(text) {
		for _, c := range candidates {
			if id, ok := h.resolveUsername(ctx, client, c.name); ok {
				ids = append(ids, id)
				tokens = append(tokens, c.token)
				break // longest-first, so the first hit is the best one
			}
		}
	}
	return ids, tokens
}

// resolveUsername looks up name (a username, display name, or real name,
// case-insensitive) against the workspace's user list, lazily loaded and cached for
// the life of the process. Returns the user's ID and true on a match.
func (h *GiveHandler) resolveUsername(ctx context.Context, client *slack.Client, name string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.userCache == nil {
		h.userCache = map[string]string{}
		users, err := client.GetUsersContext(ctx)
		if err != nil {
			log.Printf("give: failed to list workspace users for @mention lookup: %v", err)
			return "", false
		}
		for _, u := range users {
			if u.Deleted || u.IsBot {
				continue
			}
			h.userCache[strings.ToLower(u.Name)] = u.ID
			if u.Profile.DisplayName != "" {
				h.userCache[strings.ToLower(u.Profile.DisplayName)] = u.ID
			}
			if u.RealName != "" {
				h.userCache[strings.ToLower(u.RealName)] = u.ID
			}
		}
	}

	id, ok := h.userCache[strings.ToLower(name)]
	return id, ok
}

// parseReason returns whatever text remains in args after stripping user mentions,
// resolved plain-text @mentions (extraTokens), and emoji tokens - the recognition
// reason. May be empty.
func (h *GiveHandler) parseReason(text string, extraTokens []string) string {
	reason := userRegex.ReplaceAllString(text, "")
	reason = h.emojiRegex().ReplaceAllString(reason, "")
	for _, tok := range extraTokens {
		reason = strings.ReplaceAll(reason, tok, "")
	}
	return strings.TrimSpace(reason)
}

// IsDirectMessage reports whether channelID is a DM (a Slack channel ID conversation
// with a single other user), not a public or private channel. Exported so other
// packages (the web UI's feed rendering) can recognize the same convention rather than
// re-deriving it.
func IsDirectMessage(channelID string) bool {
	return strings.HasPrefix(channelID, "D")
}
