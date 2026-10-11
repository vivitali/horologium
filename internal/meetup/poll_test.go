package meetup

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

func TestRangeText_SameMonth(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	if got := RangeText(w); got != "Sep 25–27" {
		t.Errorf("got %q", got)
	}
}

func TestRangeText_SpansMonths(t *testing.T) {
	w := LastWeekend(2026, time.October, wpg)
	if got := RangeText(w); got != "Oct 30 – Nov 1" {
		t.Errorf("got %q", got)
	}
}

func TestQuestion(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Question(w)
	if got != "#meetup Meetup on the last weekend of the month (Sep 25–27). Pick what works for you (times are approximate), multiple choices allowed. The meetup happens with at least 4 votes" {
		t.Errorf("got %q", got)
	}
	if utf8.RuneCountInString(got) > 300 {
		t.Errorf("question too long: %d", utf8.RuneCountInString(got))
	}
}

func TestOptions_WithIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Options(w, "Sat ~2pm — pool on Osborne")
	want := []string{
		"🍻 Fri Sep 25, ~6:30pm",
		"☕ Sat Sep 26, ~12:30pm",
		"🍻 Sat Sep 26, ~6:30pm",
		"☕ Sun Sep 27, ~12:30pm",
		"🍻 Sun Sep 27, ~6:30pm",
		"🤖 AI pick: Sat ~2pm — pool on Osborne",
		OptionCannot,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d options: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("option %d: got %q want %q", i, got[i], want[i])
		}
		if utf8.RuneCountInString(got[i]) > MaxOptionLen {
			t.Errorf("option %d too long", i)
		}
	}
}

func TestOptions_WithoutIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Options(w, "")
	if len(got) != 6 || got[5] != OptionCannot {
		t.Errorf("got %q", got)
	}
}

func TestOptions_TruncatesLongIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	long := strings.Repeat("я", 150)
	got := Options(w, long)
	if utf8.RuneCountInString(got[5]) > MaxOptionLen {
		t.Errorf("idea option not truncated: %d runes", utf8.RuneCountInString(got[5]))
	}
	if !strings.HasPrefix(got[5], "🤖 ") {
		t.Errorf("missing prefix: %q", got[5])
	}
}

func TestOptions_TruncatesByUTF16Units(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	emoji := strings.Repeat("🎉", 60) // 60 runes, 120 UTF-16 units
	got := Options(w, emoji)
	if n := len(utf16.Encode([]rune(got[5]))); n > MaxOptionLen {
		t.Errorf("option is %d UTF-16 units, max %d", n, MaxOptionLen)
	}
}
