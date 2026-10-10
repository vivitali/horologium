package digest

import (
	"strings"
	"testing"
	"time"

	"event_calendar/internal/annual"
	"event_calendar/internal/models"
)

func TestFormatEventsMessage_Empty(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	got := FormatEventsMessage(nil, nil, now)
	if !strings.Contains(got, "No upcoming events") {
		t.Errorf("expected empty-state message, got: %s", got)
	}
}

func TestFormatEventsMessage_StartsWithTag(t *testing.T) {
	msg := FormatEventsMessage([]models.Event{{Name: "X", URL: "https://e", Source: "meetup", StartTime: time.Now().Add(time.Hour)}}, nil, time.Now())
	if !strings.HasPrefix(msg, "\\#events ") {
		t.Errorf("digest must start with the #events tag:\n%s", msg)
	}
}

func TestFormatEventsMessage_GroupsAndHeaders(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC) // Wed
	events := []models.Event{
		{Name: "Today Event", URL: "https://e/1", Source: "meetup", StartTime: now.Add(3 * time.Hour), Venue: "Hub"},
		{Name: "Friday Event", URL: "https://e/2", Source: "eventbrite", StartTime: time.Date(2026, 1, 9, 18, 0, 0, 0, time.UTC)},
		{Name: "Next Week Event", URL: "https://e/3", Source: "meetup", StartTime: time.Date(2026, 1, 14, 18, 0, 0, 0, time.UTC)},
	}
	msg := FormatEventsMessage(events, nil, now)
	for _, want := range []string{
		"Winnipeg Tech Events",
		"3 upcoming",
		`*🔥 Today \(Wed, Jan 7\)*`,
		`*⚡ This Week \(Jan 4 – Jan 10\)*`,
		`*📅 Next Week \(Jan 11 – Jan 17\)*`,
		"*Today Event*",
		"*Friday Event*",
		"*Next Week Event*",
		"— [Meetup](https://e/1)",
		"— [Eventbrite](https://e/2)",
		"— [Meetup](https://e/3)",
		"`Wed Jan 7 · ", // date with time prefix
		"📍 `Hub`",       // venue with pin emoji
		`\#WinnipegTech`,
		`\#TechEvents`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q.\n--- message ---\n%s", want, msg)
		}
	}
}

func TestFormatEventsMessage_DropsGenericVenue(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	events := []models.Event{
		{Name: "X", URL: "https://e/x", Source: "eventbrite", StartTime: now.Add(24 * time.Hour), Venue: "Event"},
	}
	msg := FormatEventsMessage(events, nil, now)
	if strings.Contains(msg, "Event\n") || strings.Contains(msg, "`Event`") {
		t.Errorf("generic 'Event' venue should be dropped:\n%s", msg)
	}
}

func TestFormatEventsMessage_RendersOnlineVenue(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	events := []models.Event{
		{Name: "Y", URL: "https://e/y", Source: "meetup", StartTime: now.Add(24 * time.Hour), Venue: "Online"},
	}
	msg := FormatEventsMessage(events, nil, now)
	if !strings.Contains(msg, "💻 Online") {
		t.Errorf("expected '💻 Online' marker for online venue:\n%s", msg)
	}
}

func TestFormatEventsMessage_HasExpandableAnnualFooter(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	events := []models.Event{
		{Name: "X", URL: "https://e/x", Source: "meetup", StartTime: now.Add(24 * time.Hour)},
	}
	annualEvts := []annual.AnnualEvent{
		{Name: "Prairie Dev Con", Date: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC), URL: "https://www.prairiedevcon.com/"},
	}
	msg := FormatEventsMessage(events, annualEvts, now)
	if !strings.Contains(msg, "**>📌 Save the date") {
		t.Errorf("missing expandable blockquote opener:\n%s", msg)
	}
	if !strings.HasSuffix(strings.TrimSpace(strings.Split(msg, "\n_Shared")[0]), "||") {
		t.Errorf("expandable blockquote should end with || before footer:\n%s", msg)
	}
}

func TestFormatEventsMessage_TruncatesLong(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	var events []models.Event
	for i := 0; i < 200; i++ {
		events = append(events, models.Event{
			Name:      "Event with a fairly long name that pushes content",
			URL:       "https://example.com/very/long/url/path/segment",
			Source:    "meetup",
			StartTime: now.Add(time.Duration(i) * time.Hour),
		})
	}
	msg := FormatEventsMessage(events, nil, now)
	if len(msg) > 4096 {
		t.Errorf("message exceeded Telegram limit: %d chars", len(msg))
	}
}
