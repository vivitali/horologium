package meetup

import (
	"strings"
	"testing"
)

func TestPickWinner_Basic(t *testing.T) {
	idx, ok := PickWinner([]int{2, 5, 1, 0, 3})
	if !ok || idx != 1 {
		t.Errorf("got idx=%d ok=%v", idx, ok)
	}
}

func TestPickWinner_IgnoresCannotOption(t *testing.T) {
	// Last option has the most votes but is "cannot attend".
	idx, ok := PickWinner([]int{4, 1, 0, 9})
	if !ok || idx != 0 {
		t.Errorf("got idx=%d ok=%v", idx, ok)
	}
}

func TestPickWinner_TieGoesToEarliest(t *testing.T) {
	idx, ok := PickWinner([]int{4, 4, 4, 0})
	if !ok || idx != 0 {
		t.Errorf("got idx=%d ok=%v", idx, ok)
	}
}

func TestPickWinner_BelowThreshold(t *testing.T) {
	if idx, ok := PickWinner([]int{3, 2, 1, 0}); ok {
		t.Errorf("expected no winner, got idx=%d", idx)
	}
	if _, ok := PickWinner([]int{0}); ok {
		t.Error("single option must not produce a winner")
	}
	if _, ok := PickWinner(nil); ok {
		t.Error("nil counts must not produce a winner")
	}
}

func TestFormatSummary_Winner(t *testing.T) {
	opts := []string{"🍻 Fri Sep 25, ~6:30pm", "☕ Sat Sep 26, ~12:30pm", OptionCannot}
	got := FormatSummary(opts, []int{2, 5, 1}, 1, true, "")
	if !strings.HasPrefix(got, "\\#meetup\n") {
		t.Errorf("summary must start with the escaped tag:\n%s", got)
	}
	if !strings.Contains(got, "📌 *It's settled:* ☕ Sat Sep 26, \\~12:30pm") {
		t.Errorf("missing winner line:\n%s", got)
	}
	if !strings.Contains(got, "5 votes\\. See you there") {
		t.Errorf("missing count:\n%s", got)
	}
	if !strings.Contains(got, "How people voted:") || !strings.Contains(got, "6:30pm — 2") {
		t.Errorf("missing tally:\n%s", got)
	}
	if !strings.Contains(got, "make it this time") {
		t.Errorf("tally should include the cannot option:\n%s", got)
	}
	// Every reserved char outside our own markup must be escaped.
	for _, raw := range []string{" ~", "(", ")", "."} {
		if strings.Contains(strings.ReplaceAll(got, "\\"+strings.TrimSpace(raw), ""), strings.TrimSpace(raw)) {
			t.Errorf("unescaped %q in:\n%s", raw, got)
		}
	}
}

func TestFormatSummary_WinnerWithMissingOptions(t *testing.T) {
	got := FormatSummary(nil, []int{0, 6, 0}, 1, true, "")
	if !strings.Contains(got, "option \\#2") || strings.Contains(got, "No meetup") {
		t.Errorf("unexpected:\n%s", got)
	}
}

func TestFormatSummary_Skip(t *testing.T) {
	opts := []string{"a", "b", OptionCannot}
	got := FormatSummary(opts, []int{2, 1, 6}, 0, false, "ignored")
	if !strings.Contains(got, "No meetup this time") || !strings.Contains(got, "got 2 votes, and it takes at least 4") {
		t.Errorf("unexpected skip text:\n%s", got)
	}
}

func TestVotesWord(t *testing.T) {
	cases := map[int]string{0: "votes", 1: "vote", 2: "votes", 11: "votes"}
	for n, want := range cases {
		if got := votesWord(n); got != want {
			t.Errorf("%d: got %q want %q", n, got, want)
		}
	}
}

func TestFormatSummary_IdeaDetails(t *testing.T) {
	opts := []string{"🍻 Fri", "🤖 Sat ~2pm — pool", OptionCannot}
	got := FormatSummary(opts, []int{1, 5, 0}, 1, true, "Pool hall on Osborne. Bus 18 stops outside.")
	if !strings.Contains(got, "ℹ️ Pool hall on Osborne\\. Bus 18 stops outside\\.") {
		t.Errorf("details missing or unescaped:\n%s", got)
	}
	got = FormatSummary(opts, []int{5, 1, 0}, 0, true, "Pool hall on Osborne.")
	if strings.Contains(got, "Pool hall") {
		t.Errorf("details shown for a non-idea winner:\n%s", got)
	}
}
