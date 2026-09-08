// Package digest formats event lists into Telegram messages.
package digest

import (
	"fmt"
	"strings"
	"time"

	"event_calendar/internal/annual"
	"event_calendar/internal/models"
	"event_calendar/internal/timeutil"
	"event_calendar/pkg/telegram"
)

const telegramMaxLen = 4096

// winnipegLoc renders dates/times in local Manitoba time. Falls back to UTC
// if the tzdata isn't available (very unlikely on Lambda's Amazon Linux).
var winnipegLoc = func() *time.Location {
	if l, err := time.LoadLocation("America/Winnipeg"); err == nil {
		return l
	}
	return time.UTC
}()

var periodOrder = []string{"Today", "This Week", "Next Week", "Later"}

// FormatEventsMessage renders a MarkdownV2 Telegram digest of the events,
// grouped by time bucket relative to now. Result is guaranteed to be <= 4096 chars.
//
// Layout: header → grouped events → (optional) expandable annual-events
// blockquote → tracker credit + hashtags. When the message would exceed
// the Telegram limit we truncate the event list (never the trailing
// blockquote or hashtags) so the resulting message still parses cleanly.
func FormatEventsMessage(events []models.Event, annualEvts []annual.AnnualEvent, now time.Time) string {
	if len(events) == 0 {
		return "📅 *No upcoming events found* for Winnipeg tech community\\."
	}

	header := fmt.Sprintf("🚀 *Winnipeg Tech Events* — %s · %d upcoming\n\n",
		escMD(now.Format("Mon Jan 2")), len(events))

	var trailer strings.Builder
	if af := annualFooter(annualEvts, now); af != "" {
		trailer.WriteString(af)
		trailer.WriteString("\n")
	}
	trailer.WriteString("_Shared via Winnipeg Tech Events Tracker_\n")
	trailer.WriteString("\\#WinnipegTech \\#TechEvents")

	body := buildEventsBody(events, now)
	budget := telegramMaxLen - len(header) - len(trailer.String())
	body = truncateToLines(body, budget)

	return header + body + trailer.String()
}

// buildEventsBody renders the grouped event list.
func buildEventsBody(events []models.Event, now time.Time) string {
	var sb strings.Builder
	groups := groupEvents(events, now)
	for _, period := range periodOrder {
		bucket := groups[period]
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "*%s*\n", escMD(bucketHeader(period, now)))
		for _, e := range bucket {
			writeEvent(&sb, e)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// truncateToLines drops trailing lines until the string fits in budget bytes,
// then re-appends a trailing newline so the next section starts cleanly.
func truncateToLines(s string, budget int) string {
	if len(s) <= budget {
		return s
	}
	if budget <= 0 {
		return ""
	}
	cut := s[:budget]
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
		return cut[:i+1]
	}
	return ""
}

func groupEvents(events []models.Event, now time.Time) map[string][]models.Event {
	groups := map[string][]models.Event{}
	for _, e := range events {
		switch {
		case timeutil.IsSameDay(e.StartTime, now):
			groups["Today"] = append(groups["Today"], e)
		case timeutil.IsThisWeek(e.StartTime, now):
			groups["This Week"] = append(groups["This Week"], e)
		case timeutil.IsNextWeek(e.StartTime, now):
			groups["Next Week"] = append(groups["Next Week"], e)
		default:
			groups["Later"] = append(groups["Later"], e)
		}
	}
	return groups
}

// bucketHeader returns a header label with a date range for the period.
// "Later" stays bare since its window is open-ended.
func bucketHeader(period string, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	startOfWeek := today.AddDate(0, 0, -int(today.Weekday()))
	endOfWeek := startOfWeek.AddDate(0, 0, 6)
	startOfNext := startOfWeek.AddDate(0, 0, 7)
	endOfNext := startOfNext.AddDate(0, 0, 6)

	switch period {
	case "Today":
		return fmt.Sprintf("🔥 Today (%s)", today.Format("Mon, Jan 2"))
	case "This Week":
		return fmt.Sprintf("⚡ This Week (%s – %s)",
			startOfWeek.Format("Jan 2"), endOfWeek.Format("Jan 2"))
	case "Next Week":
		return fmt.Sprintf("📅 Next Week (%s – %s)",
			startOfNext.Format("Jan 2"), endOfNext.Format("Jan 2"))
	default:
		return "🔮 " + period
	}
}

func writeEvent(sb *strings.Builder, e models.Event) {
	name := "*" + escMD(strings.TrimSpace(e.Name)) + "*"
	if e.URL != "" {
		fmt.Fprintf(sb, "• %s — [%s](%s)\n",
			name, escMD(sourceName(e.Source)), escMDURL(e.URL))
	} else {
		fmt.Fprintf(sb, "• %s — %s\n", name, codeMD(sourceName(e.Source)))
	}

	var meta []string
	if !e.StartTime.IsZero() {
		local := e.StartTime.In(winnipegLoc)
		layout := "Mon Jan 2"
		if local.Hour() != 0 || local.Minute() != 0 {
			layout = "Mon Jan 2 · 3:04 PM"
		}
		meta = append(meta, codeMD(local.Format(layout)))
	}
	switch v := cleanVenue(e.Venue); {
	case v == "":
		// no venue
	case isOnlineVenue(v):
		meta = append(meta, "💻 Online")
	default:
		meta = append(meta, "📍 "+codeMD(v))
	}
	if e.Price != "" && e.Price != "Free" {
		meta = append(meta, codeMD(e.Price))
	}
	if len(meta) > 0 {
		fmt.Fprintf(sb, "  %s\n", strings.Join(meta, " · "))
	}
}

// annualFooter renders future annual events as an expandable MarkdownV2 quote.
// Returns "" when there's nothing to show.
func annualFooter(events []annual.AnnualEvent, now time.Time) string {
	upcoming := futureOnly(events, now)
	if len(upcoming) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("**>📌 Save the date — annual events\n")
	for i, e := range upcoming {
		line := fmt.Sprintf(">• %s — %s [info](%s)",
			escMD(e.Name), escMD(e.Date.Format("Jan 2, 2006")), escMDURL(e.URL))
		if i == len(upcoming)-1 {
			line += "||"
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// futureOnly returns annual events whose date is strictly after now,
// preserving caller-supplied order.
func futureOnly(events []annual.AnnualEvent, now time.Time) []annual.AnnualEvent {
	out := make([]annual.AnnualEvent, 0, len(events))
	for _, e := range events {
		if e.Date.After(now) {
			out = append(out, e)
		}
	}
	return out
}

// cleanVenue drops placeholder strings that add no information. "Online"
// is kept (case-insensitively normalized) so the renderer can show 💻 instead
// of dropping the meta entirely.
func cleanVenue(v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "event", "tbd", "tba", "none":
		return ""
	case "online", "virtual", "remote", "online event":
		return "online"
	}
	return v
}

func isOnlineVenue(v string) bool {
	return v == "online"
}

func sourceName(source string) string {
	switch source {
	case "meetup":
		return "Meetup"
	case "eventbrite":
		return "Eventbrite"
	case "luma":
		return "Luma"
	default:
		return source
	}
}

// MarkdownV2 code-block escape: only `\` and `` ` `` need escaping inside `...`.
var mdV2Code = strings.NewReplacer(
	`\`, `\\`,
	"`", "\\`",
)

// MarkdownV2 URL escape: only `)` and `\` need escaping inside link target.
var mdV2URL = strings.NewReplacer(
	`\`, `\\`,
	`)`, `\)`,
)

func escMD(s string) string    { return telegram.EscapeMarkdownV2(s) }
func escMDURL(s string) string { return mdV2URL.Replace(s) }
func codeMD(s string) string   { return "`" + mdV2Code.Replace(s) + "`" }
