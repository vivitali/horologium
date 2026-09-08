package meetup

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestUkrRange_SameMonth(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg) // 25–27 вересня
	if got := UkrRange(w); got != "25–27 вересня" {
		t.Errorf("got %q", got)
	}
}

func TestUkrRange_SpansMonths(t *testing.T) {
	w := LastWeekend(2026, time.October, wpg) // 30 жовтня – 1 листопада
	if got := UkrRange(w); got != "30 жовтня – 1 листопада" {
		t.Errorf("got %q", got)
	}
}

func TestQuestion(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Question(w)
	if got != "Зустріч в останні вихідні місяця (25–27 вересня)?" {
		t.Errorf("got %q", got)
	}
	if utf8.RuneCountInString(got) > 300 {
		t.Errorf("question too long: %d", utf8.RuneCountInString(got))
	}
}

func TestOptions_WithIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Options(w, "Субота після обіду — більярд на Осборн")
	want := []string{
		"🍻 Пʼятниця 25 вересня, ~18:30–19:00 — кава/пиво",
		"☕ Субота 26 вересня, ~12:00–13:00 — кава",
		"☕ Неділя 27 вересня, ~12:00–13:00 — кава",
		"🤖 Субота після обіду — більярд на Осборн",
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
	if len(got) != 4 || got[3] != OptionCannot {
		t.Errorf("got %q", got)
	}
}

func TestOptions_TruncatesLongIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	long := strings.Repeat("я", 150)
	got := Options(w, long)
	if utf8.RuneCountInString(got[3]) > MaxOptionLen {
		t.Errorf("idea option not truncated: %d runes", utf8.RuneCountInString(got[3]))
	}
	if !strings.HasPrefix(got[3], "🤖 ") {
		t.Errorf("missing prefix: %q", got[3])
	}
}
