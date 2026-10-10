package meetup

import (
	"fmt"
	"strings"
	"time"
)

const (
	// OptionCannot is always the last poll option and is excluded from the
	// winner computation.
	OptionCannot = "❌ Цього разу не зможу"
	// MaxOptionLen is Telegram's per-option character limit.
	MaxOptionLen = 100
	// MaxIdeaLen leaves room for the "🤖 " prefix inside MaxOptionLen.
	MaxIdeaLen = 96
	ideaPrefix = "🤖 "
	// Tag makes every poll and summary findable with one tap in Telegram.
	Tag = "#meetup"
)

// Genitive month names, as used after a day number ("24 жовтня").
var ukrMonths = [...]string{
	time.January:   "січня",
	time.February:  "лютого",
	time.March:     "березня",
	time.April:     "квітня",
	time.May:       "травня",
	time.June:      "червня",
	time.July:      "липня",
	time.August:    "серпня",
	time.September: "вересня",
	time.October:   "жовтня",
	time.November:  "листопада",
	time.December:  "грудня",
}

// UkrDate formats a date as "24 жовтня".
func UkrDate(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), ukrMonths[t.Month()])
}

// UkrRange formats Fri–Sun as "24–26 жовтня", or
// "30 жовтня – 1 листопада" when Sunday is in the next month.
func UkrRange(w Weekend) string {
	if w.Fri.Month() == w.Sun.Month() {
		return fmt.Sprintf("%d–%d %s", w.Fri.Day(), w.Sun.Day(), ukrMonths[w.Sun.Month()])
	}
	return UkrDate(w.Fri) + " – " + UkrDate(w.Sun)
}

// Question is the poll question naming the weekend's dates.
func Question(w Weekend) string {
	return fmt.Sprintf("%s Зустріч в останні вихідні місяця (%s). Оберіть зручний час (орієнтовно), можна кілька варіантів", Tag, UkrRange(w))
}

// Options builds the poll options (7, or 6 without an idea; Telegram allows 10).
// idea == "" omits the AI option.
// The cannot-attend option is always last.
func Options(w Weekend, idea string) []string {
	opts := []string{
		fmt.Sprintf("🍻 Пт %s, ~18:30", UkrDate(w.Fri)),
		fmt.Sprintf("☕ Сб %s, ~12:30", UkrDate(w.Sat)),
		fmt.Sprintf("🍻 Сб %s, ~18:30", UkrDate(w.Sat)),
		fmt.Sprintf("☕ Нд %s, ~12:30", UkrDate(w.Sun)),
		fmt.Sprintf("🍻 Нд %s, ~18:30", UkrDate(w.Sun)),
	}
	if idea = strings.TrimSpace(idea); idea != "" {
		opts = append(opts, ideaPrefix+truncateRunes(idea, MaxIdeaLen))
	}
	return append(opts, OptionCannot)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
