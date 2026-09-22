package web

import "github.com/kyokomi/emoji/v2"

var emojiCodeMap = emoji.CodeMap()

// emojiGlyph returns the Unicode glyph for a Slack emoji short name (e.g. "star" ->
// "⭐"), falling back to ":name:" text for custom workspace emoji that have no
// standard Unicode equivalent.
func emojiGlyph(name string) string {
	if glyph, ok := emojiCodeMap[":"+name+":"]; ok {
		return glyph
	}
	return ":" + name + ":"
}
