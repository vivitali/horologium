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
	opts := []string{"🍻 Пт 25 вересня, 18:30", "☕ Сб 26 вересня, 12:00", OptionCannot}
	got := FormatSummary(opts, []int{2, 5, 1}, 1, true)
	if !strings.HasPrefix(got, "\\#meetup\n") {
		t.Errorf("summary must start with the escaped tag:\n%s", got)
	}
	if !strings.Contains(got, "📌 *Домовились:* ☕ Сб 26 вересня, 12:00") {
		t.Errorf("missing winner line:\n%s", got)
	}
	if !strings.Contains(got, "5 голосів — до зустрічі") {
		t.Errorf("missing count:\n%s", got)
	}
	if !strings.Contains(got, "Як голосували:") || !strings.Contains(got, "18:30 — 2") {
		t.Errorf("missing tally:\n%s", got)
	}
	if !strings.Contains(got, "без мене") {
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
	got := FormatSummary(nil, []int{0, 6, 0}, 1, true)
	if !strings.Contains(got, "варіант \\#2") || strings.Contains(got, "не збираємось") {
		t.Errorf("unexpected:\n%s", got)
	}
}

func TestFormatSummary_Skip(t *testing.T) {
	opts := []string{"a", "b", OptionCannot}
	got := FormatSummary(opts, []int{2, 1, 6}, 0, false)
	if !strings.Contains(got, "Цього разу не збираємось") || !strings.Contains(got, "найбільше 2 голоси, а треба хоча б 4") {
		t.Errorf("unexpected skip text:\n%s", got)
	}
}

func TestVotesWord(t *testing.T) {
	cases := map[int]string{1: "голос", 2: "голоси", 4: "голоси", 5: "голосів", 11: "голосів", 12: "голосів", 21: "голос", 22: "голоси", 0: "голосів"}
	for n, want := range cases {
		if got := votesWord(n); got != want {
			t.Errorf("%d: got %q want %q", n, got, want)
		}
	}
}
