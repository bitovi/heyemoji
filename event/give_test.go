package event

import (
	"reflect"
	"testing"
)

func TestGiveHandler_EffectiveDailyCap(t *testing.T) {
	normal := NewGiveHandler(map[string]int{"star": 1}, 5, false, nil)
	if got := normal.effectiveDailyCap(); got != 5 {
		t.Errorf("effectiveDailyCap() (test mode off) = %d, want 5", got)
	}

	testMode := NewGiveHandler(map[string]int{"star": 1}, 5, true, nil)
	if got := testMode.effectiveDailyCap(); got != UnlimitedDailyCap {
		t.Errorf("effectiveDailyCap() (test mode on) = %d, want %d", got, UnlimitedDailyCap)
	}
}

func TestGiveHandler_ParseUsers(t *testing.T) {
	h := NewGiveHandler(map[string]int{"star": 1}, 5, false, nil)

	got := h.parseUsers("<@U123|bob> and <@U456> did great work :star:")
	want := []string{"U123", "U456"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseUsers() = %v, want %v", got, want)
	}
}

func TestGiveHandler_ParseEmojis(t *testing.T) {
	h := NewGiveHandler(map[string]int{"star": 1, "clap": 2}, 5, false, nil)

	got := h.parseEmojis("<@U123> :star: :clap: nice work")
	want := []string{"star", "clap"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseEmojis() = %v, want %v", got, want)
	}
}

func TestGiveHandler_ParseReason(t *testing.T) {
	h := NewGiveHandler(map[string]int{"star": 1}, 5, false, nil)

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

func TestCandidatesForMention_SingleWord(t *testing.T) {
	// A bare "@name" (not <@...> markup) is a candidate for username lookup; a real
	// mention's <@U123|bob> internals must not also be picked up as a candidate.
	occurrences := candidatesForMention("<@U123|bob> @philh :star: because they crushed it")
	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1: %+v", len(occurrences), occurrences)
	}
	if got := occurrences[0][0]; got.name != "philh" || got.token != "@philh" {
		t.Errorf("first candidate = %+v, want name:philh token:@philh", got)
	}
}

func TestCandidatesForMention_MultiWordLongestFirst(t *testing.T) {
	// A multi-word display name like "David Nicholas" must be offered as a candidate
	// (not just the first word "David") and tried longest-first.
	occurrences := candidatesForMention("@David Nicholas :star: third thing")
	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1: %+v", len(occurrences), occurrences)
	}

	got := occurrences[0]
	want := []plainMentionCandidate{
		{name: "David Nicholas", token: "@David Nicholas"},
		{name: "David", token: "@David"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %+v, want %+v", got, want)
	}
}

func TestCandidatesForMention_TrailingPunctuation(t *testing.T) {
	// A trailing comma right after the name (mid-sentence, no emoji/mention boundary
	// following it) must still produce "David Nicholas" as a candidate, with the
	// comma stripped - even though longer, punctuation-spanning candidates are also
	// generated and tried first (resolvePlainMentions falls through to shorter ones
	// at runtime when a longer candidate doesn't match a real user).
	occurrences := candidatesForMention("Thanks @David Nicholas, for everything")
	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1: %+v", len(occurrences), occurrences)
	}

	found := false
	for _, c := range occurrences[0] {
		if c.name == "David Nicholas" && c.token == "@David Nicholas" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("candidates = %+v, want one with name:\"David Nicholas\" token:\"@David Nicholas\"", occurrences[0])
	}
}

func TestIsDirectMessage(t *testing.T) {
	if !isDirectMessage("D12345") {
		t.Error("expected D-prefixed channel to be a direct message")
	}
	if isDirectMessage("C12345") {
		t.Error("expected C-prefixed channel not to be a direct message")
	}
}
