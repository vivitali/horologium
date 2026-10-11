// Package jobs contains the reusable scheduled-job logic for the events
// digest and the annual-events scrape. Each job takes its dependencies as interfaces so callers
// (Lambda, local CLI) can wire real or fake implementations.
package jobs

import (
	"fmt"
	"log"
	"time"

	"event_calendar/internal/annual"
	"event_calendar/internal/config"
	"event_calendar/internal/digest"
	"event_calendar/internal/models"
)

// Scraper produces events from one or more sources.
type Scraper interface {
	ScrapeEvents(city, category string, period time.Duration) ([]models.Event, error)
}

// Sender posts a Markdown-formatted message to a Telegram chat.
type Sender interface {
	SendMessage(chatID, message string) error
}

// EventsDeps holds the collaborators needed by RunEventsDigest.
type EventsDeps struct {
	Scraper Scraper
	Sender  Sender
	Now     time.Time
}

// Result reports the outcome of a job execution.
type Result struct {
	Success     bool   `json:"success"`
	EventsCount int    `json:"events_count"`
	MessageSent bool   `json:"message_sent"`
	Error       string `json:"error,omitempty"`
}

// RunEventsDigest scrapes upcoming events, formats a digest, and sends it to
// the configured chat. In TestMode the digest is built but not sent.
func RunEventsDigest(deps EventsDeps, cfg config.Config, annualEvts []annual.AnnualEvent) Result {
	period := time.Duration(cfg.PeriodDays) * 24 * time.Hour
	events, err := deps.Scraper.ScrapeEvents(cfg.City, cfg.Categories, period)
	if err != nil {
		return Result{Error: fmt.Sprintf("scrape failed: %v", err)}
	}

	future := filterFuture(events, deps.Now)
	log.Printf("events: scraped=%d future=%d", len(events), len(future))

	if len(future) == 0 {
		return Result{Success: true, EventsCount: 0}
	}

	message := digest.FormatEventsMessage(future, annualEvts, deps.Now)

	if cfg.TestMode {
		log.Printf("test mode: would have sent %d chars", len(message))
		return Result{Success: true, EventsCount: len(future), MessageSent: false}
	}

	if cfg.BotToken == "" || cfg.ChatID == "" {
		return Result{Error: "TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID not set"}
	}

	if err := deps.Sender.SendMessage(cfg.ChatID, message); err != nil {
		return Result{EventsCount: len(future), Error: fmt.Sprintf("send failed: %v", err)}
	}
	return Result{Success: true, EventsCount: len(future), MessageSent: true}
}

func filterFuture(events []models.Event, now time.Time) []models.Event {
	var out []models.Event
	for _, e := range events {
		if e.StartTime.After(now) {
			out = append(out, e)
		}
	}
	return out
}
