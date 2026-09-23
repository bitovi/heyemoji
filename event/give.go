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

// userRegex matches Slack's real mention markup, e.g. <@U12345> or <@U12345|username>.
// This only ever appears in regular message composition, not slash command text.
var userRegex = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|[^>]*)?>`)

// plainMentionRegex matches a bare "@username" token. This is what Slack's slash
// command text box actually inserts when someone picks a suggestion from its "@"
// autocomplete - the literal @username text, not <@U...> markup (confirmed directly
// against a production log: selecting a real suggestion produced the raw text
// "@luca ...", not a mention). So this isn't a fallback for people who skip the
// autocomplete - it's the primary, and in practice only, way a recipient ever gets
// recognized here.
var plainMentionRegex = regexp.MustCompile(`@(\S+)`)

// trailingPunctRegex strips punctuation immediately after a plain-mentioned username,
// e.g. "@luca," or "@luca:", so it isn't treated as part of the username.
var trailingPunctRegex = regexp.MustCompile(`[,.:;!?]+$`)

func NewGiveHandler(emoji map[string]int, dailyCap int, testMode bool, announceChannelID string, db database.Driver) *GiveHandler {
	return &GiveHandler{emoji: emoji, dailyCap: dailyCap, testMode: testMode, announceChannelID: announceChannelID, db: db}
}

type GiveHandler struct {
	emoji    map[string]int
	dailyCap int
	testMode bool
	// announceChannelID is where every give's announcement posts and threads,
	// regardless of where the command was run from. Empty disables public
	// announcements entirely (gives are still recorded).
	announceChannelID string
	db                database.Driver

	mu        sync.Mutex
	userCache map[string]string // lowercased Slack username -> user ID, lazily loaded
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
// h.announceChannelID - always that one configured channel, regardless of which
// channel or DM the command was actually run from (that origin is still recorded, as
// KarmaEvent.SourceChannelID, purely for the web UI's feed). If no announce channel is
// configured, the announcement is skipped entirely; points are still recorded either
// way. The giver always gets a DM confirming the points were recorded, regardless of
// whether (or why) the public announcement didn't post - DMs don't require channel
// membership the way chat.postMessage/postEphemeral into cmd.ChannelID do, so this is
// the one confirmation path that works everywhere.
//
// A recipient is recognized two ways: real <@U...> mention markup (userRegex, from
// regular message composition - effectively never seen from the slash command box in
// practice), and a bare "@username" token (plainMentionRegex) matched against Slack
// usernames only - never display name or real name. Usernames are unique per
// workspace by Slack's own rules, so this can never silently resolve to the wrong
// person on a name collision the way matching against display/real name could (both
// of those can be shared by multiple people; an earlier version matched all three
// fields and that's very likely what actually caused a giver to have their attempt
// blocked as apparent self-karma when they meant someone else entirely - not a bug in
// the self-karma check itself, which correctly blocked it, just a confusing resolve).
func (h *GiveHandler) Execute(ctx context.Context, client *slack.Client, cmd slack.SlashCommand, args string) error {
	emojis := h.parseEmojis(args)
	if len(emojis) == 0 {
		log.Printf("give: no known emoji found in args %q", args)
		return h.replyEphemeral(ctx, client, cmd, h.usageMessage())
	}

	users := h.parseUsers(args)

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
	// after we post it - see the backfill loop below). Skipped entirely when there's
	// no announce channel configured - nothing to thread under.
	existingThreadTS := make(map[string]string, len(recipients))
	if h.announceChannelID != "" {
		for _, u := range recipients {
			if ts, ok, err := h.db.LatestThreadTS(ctx, h.announceChannelID, u, since); err != nil {
				return err
			} else if ok {
				existingThreadTS[u] = ts
			}
		}
	}

	events := make([]database.KarmaEvent, 0, len(recipients)*len(emojis))
	batchTotal := 0
	for _, e := range emojis {
		points := h.emoji[e]
		batchTotal += points * len(recipients)
		for _, u := range recipients {
			events = append(events, database.KarmaEvent{
				From:            cmd.UserID,
				To:              u,
				Emoji:           e,
				Points:          points,
				Reason:          reason,
				SourceChannelID: cmd.ChannelID,
				ThreadChannelID: h.announceChannelID,
				ThreadTS:        existingThreadTS[u],
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
	if h.announceChannelID != "" {
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
	if h.announceChannelID == "" {
		confirmation += "\n\n(No public recognition message was posted - no announcement channel is configured. The points were still given.)"
	} else if len(failedAnnouncements) > 0 {
		failedMentions := Map(failedAnnouncements, func(u string) string { return fmt.Sprintf("<@%s>", u) })
		confirmation += fmt.Sprintf(
			"\n\n:warning: I couldn't post the public recognition message for %s - I may not be a member of the announcement channel anymore. The points were still given; someone should check that I'm still invited to <#%s>.",
			strings.Join(failedMentions, " "), h.announceChannelID)
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

	_, ts, err := client.PostMessageContext(ctx, h.announceChannelID, opts...)
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

// plainMentionToken is one "@username"-shaped occurrence found in text: the candidate
// username to look up, and the literal substring to strip from the reason if it turns
// out to be a real username.
type plainMentionToken struct {
	name  string
	token string
}

// findPlainMentions extracts every bare "@username"-shaped token from text, stripping
// trailing punctuation like a comma right after it (e.g. "@luca," -> "luca"). Pure and
// independent of any Slack API call, so it's unit-testable on its own;
// resolvePlainMentions does the actual username lookups.
func findPlainMentions(text string) []plainMentionToken {
	withoutMarkup := userRegex.ReplaceAllString(text, "")

	var found []plainMentionToken
	for _, m := range plainMentionRegex.FindAllStringSubmatchIndex(withoutMarkup, -1) {
		token := withoutMarkup[m[0]:m[1]]
		name := withoutMarkup[m[2]:m[3]]
		if loc := trailingPunctRegex.FindStringIndex(name); loc != nil {
			token = token[:len(token)-(len(name)-loc[0])]
			name = name[:loc[0]]
		}
		if name == "" {
			continue
		}
		found = append(found, plainMentionToken{name: name, token: token})
	}
	return found
}

// resolvePlainMentions resolves every bare "@username" occurrence in text against the
// workspace's usernames (never display name or real name - see resolveUsername).
// Returns the resolved user IDs and the literal "@..." substrings that matched, so
// callers can strip them out of the reason text.
func (h *GiveHandler) resolvePlainMentions(ctx context.Context, client *slack.Client, text string) (ids []string, tokens []string) {
	for _, m := range findPlainMentions(text) {
		if id, ok := h.resolveUsername(ctx, client, m.name); ok {
			ids = append(ids, id)
			tokens = append(tokens, m.token)
		}
	}
	return ids, tokens
}

// resolveUsername looks up name (a Slack username, case-insensitive) against the
// workspace's user list, lazily loaded and cached for the life of the process. Matches
// only the username field, deliberately never display name or real name - Slack
// guarantees a username is unique per workspace, so this can never resolve to the
// wrong person the way matching a possibly-shared display/real name could. Returns the
// user's ID and true on a match.
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
