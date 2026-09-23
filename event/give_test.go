package event

import (
	"reflect"
	"testing"
)

func TestGiveHandler_EffectiveDailyCap(t *testing.T) {
	normal := NewGiveHandler(map[string]int{"star": 1}, 5, false, "", nil)
	if got := normal.effectiveDailyCap(); got != 5 {
		t.Errorf("effectiveDailyCap() (test mode off) = %d, want 5", got)
	}

	testMode := NewGiveHandler(map[string]int{"star": 1}, 5, true, "", nil)
	if got := testMode.effectiveDailyCap(); got != UnlimitedDailyCap {
		t.Errorf("effectiveDailyCap() (test mode on) = %d, want %d", got, UnlimitedDailyCap)
	}
}

func TestGiveHandler_ParseUsers(t *testing.T) {
	h := NewGiveHandler(map[string]int{"star": 1}, 5, false, "", nil)

	got := h.parseUsers("<@U123|bob> and <@U456> did great work :star:")
	want := []string{"U123", "U456"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseUsers() = %v, want %v", got, want)
	}
}

func TestGiveHandler_ParseEmojis(t *testing.T) {
	h := NewGiveHandler(map[string]int{"star": 1, "clap": 2}, 5, false, "", nil)

	got := h.parseEmojis("<@U123> :star: :clap: nice work")
	want := []string{"star", "clap"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseEmojis() = %v, want %v", got, want)
	}
}

func TestGiveHandler_ParseReason(t *testing.T) {
	h := NewGiveHandler(map[string]int{"star": 1}, 5, false, "", nil)

	cases := []struct {
		text string
		want string
	}{
		{"<@U123|bob> :star: great job on the deploy", "great job on the deploy"},
		{"<@U123> :star:", ""},
		{":star: <@U123> awesome work", "awesome work"},
	}

	for _, c := range cases {
		if got := h.parseReason(c.text, nil); got != c.want {
			t.Errorf("parseReason(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestFindPlainMentions_Basic(t *testing.T) {
	// A bare "@username" (not <@...> markup) is a candidate; a real mention's
	// <@U123|bob> internals must not also be picked up as a candidate.
	got := findPlainMentions("<@U123|bob> @luca :star: because they crushed it")
	want := []plainMentionToken{{name: "luca", token: "@luca"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findPlainMentions() = %+v, want %+v", got, want)
	}
}

func TestFindPlainMentions_TrailingPunctuation(t *testing.T) {
	got := findPlainMentions("Thanks @luca, for everything")
	want := []plainMentionToken{{name: "luca", token: "@luca"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findPlainMentions() = %+v, want %+v", got, want)
	}
}

func TestFindPlainMentions_Multiple(t *testing.T) {
	got := findPlainMentions("@luca and @kyle :star: nice work both of you")
	want := []plainMentionToken{
		{name: "luca", token: "@luca"},
		{name: "kyle", token: "@kyle"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findPlainMentions() = %+v, want %+v", got, want)
	}
}

func TestIsDirectMessage(t *testing.T) {
	if !IsDirectMessage("D12345") {
		t.Error("expected D-prefixed channel to be a direct message")
	}
	if IsDirectMessage("C12345") {
		t.Error("expected C-prefixed channel not to be a direct message")
	}
}
