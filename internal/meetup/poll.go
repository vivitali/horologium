package meetup

import (
	"fmt"
	"strings"
	"time"
)

const (
	// OptionCannot is always the last poll option and is excluded from the
	// winner computation.
	OptionCannot = "❌ Can't make it this time"
	// MaxOptionLen is Telegram's per-option character limit.
	MaxOptionLen = 100
	ideaPrefix   = "🤖 AI suggestion: "
	// MaxIdeaLen leaves room for ideaPrefix inside MaxOptionLen (prefix is 18 runes).
	MaxIdeaLen = MaxOptionLen - 18
	// Tag makes every poll and summary findable with one tap in Telegram.
	Tag = "#meetup"
)

// DateText formats a date as "Oct 24".
func DateText(t time.Time) string {
	return t.Format("Jan 2")
}

// RangeText formats Fri–Sun as "Oct 24–26", or "Oct 30 – Nov 1" when
// Sunday is in the next month.
func RangeText(w Weekend) string {
	if w.Fri.Month() == w.Sun.Month() {
		return fmt.Sprintf("%s %d–%d", w.Fri.Format("Jan"), w.Fri.Day(), w.Sun.Day())
	}
	return DateText(w.Fri) + " – " + DateText(w.Sun)
}

// Question is the poll question naming the weekend's dates.
func Question(w Weekend) string {
	return fmt.Sprintf("%s Meetup on the last weekend of the month (%s). Pick what works for you (times are approximate), multiple choices allowed. The meetup happens with at least %d votes", Tag, RangeText(w), MinVotes)
}

// Options builds the poll options (7, or 6 without an idea; Telegram allows 10).
// idea == "" omits the AI option.
// The cannot-attend option is always last.
func Options(w Weekend, idea string) []string {
	opts := []string{
		fmt.Sprintf("🍻 Fri %s, ~6:30pm", DateText(w.Fri)),
		fmt.Sprintf("☕ Sat %s, ~12:30pm", DateText(w.Sat)),
		fmt.Sprintf("🍻 Sat %s, ~6:30pm", DateText(w.Sat)),
		fmt.Sprintf("☕ Sun %s, ~12:30pm", DateText(w.Sun)),
		fmt.Sprintf("🍻 Sun %s, ~6:30pm", DateText(w.Sun)),
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
