package eventbrite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"event_calendar/internal/models"
)

type Scraper struct {
	client    *http.Client
	baseURL   string
	userAgent string
}

func NewScraper() *Scraper {
	return &Scraper{
		client:    &http.Client{Timeout: 30 * time.Second},
		baseURL:   "https://www.eventbrite.ca/d/canada--winnipeg/tech-event/",
		userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
	}
}

func (s *Scraper) GetEvents(city, category string, period time.Duration) ([]models.Event, error) {
	html, err := s.fetchHTML(s.baseURL)
	if err != nil {
		log.Printf("[Eventbrite] fetch failed: %v", err)
		return nil, err
	}
	events, err := parseServerData(html)
	if err != nil {
		log.Printf("[Eventbrite] parse failed: %v", err)
		return nil, err
	}
	cutoff := time.Now().Add(period)
	out := events[:0]
	for _, e := range events {
		if e.StartTime.IsZero() || e.StartTime.After(cutoff) {
			continue
		}
		if city != "" {
			e.City = city
		}
		if category != "" {
			e.Category = category
		}
		out = append(out, e)
	}
	log.Printf("[Eventbrite] parsed %d events, %d within %s", len(events), len(out), period)
	return out, nil
}

func (s *Scraper) fetchHTML(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eventbrite status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// serverDataMarker is the JS assignment Eventbrite emits in its SSR HTML.
var serverDataMarker = []byte("window.__SERVER_DATA__")

type serverData struct {
	SearchData struct {
		Events struct {
			Results []ebEvent `json:"results"`
		} `json:"events"`
	} `json:"search_data"`
}

type ebEvent struct {
	ID                 string   `json:"id"`
	EventbriteEventID  string   `json:"eventbrite_event_id"`
	Name               string   `json:"name"`
	Summary            string   `json:"summary"`
	URL                string   `json:"url"`
	StartDate          string   `json:"start_date"`
	StartTime          string   `json:"start_time"`
	EndDate            string   `json:"end_date"`
	EndTime            string   `json:"end_time"`
	Timezone           string   `json:"timezone"`
	IsCancelled        bool     `json:"is_cancelled"`
	IsOnline           bool     `json:"is_online_event"`
	PrimaryVenue       *ebVenue `json:"primary_venue"`
	PrimaryOrganizerID string   `json:"primary_organizer_id"`
}

type ebVenue struct {
	Name    string `json:"name"`
	Address struct {
		City             string `json:"city"`
		LocalizedAddress string `json:"localized_address_display"`
	} `json:"address"`
}

func parseServerData(html []byte) ([]models.Event, error) {
	i := bytes.Index(html, serverDataMarker)
	if i < 0 {
		return nil, errors.New("SERVER_DATA marker not found")
	}
	start := bytes.IndexByte(html[i:], '{')
	if start < 0 {
		return nil, errors.New("SERVER_DATA opening brace not found")
	}
	start += i
	end, err := findMatchingBrace(html, start)
	if err != nil {
		return nil, fmt.Errorf("brace match: %w", err)
	}
	var data serverData
	if err := json.Unmarshal(html[start:end+1], &data); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	results := data.SearchData.Events.Results
	out := make([]models.Event, 0, len(results))
	seen := make(map[string]bool, len(results))
	for _, e := range results {
		if e.IsCancelled || e.URL == "" || e.Name == "" {
			continue
		}
		// Dedupe: same organizer posting the same date with different keyword titles
		// is a single event spammed under multiple categories.
		if e.PrimaryOrganizerID != "" && e.StartDate != "" {
			k := e.PrimaryOrganizerID + "|" + e.StartDate
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		ev := models.Event{
			ID:          eventID(e),
			Name:        e.Name,
			Description: e.Summary,
			URL:         e.URL,
			StartTime:   parseEBDateTime(e.StartDate, e.StartTime, e.Timezone),
			EndTime:     parseEBDateTime(e.EndDate, e.EndTime, e.Timezone),
			Source:      "eventbrite",
		}
		if e.PrimaryVenue != nil {
			ev.Venue = e.PrimaryVenue.Name
		}
		out = append(out, ev)
	}
	return out, nil
}

func eventID(e ebEvent) string {
	if e.EventbriteEventID != "" {
		return "eventbrite-" + e.EventbriteEventID
	}
	if e.ID != "" {
		return "eventbrite-" + e.ID
	}
	return "eventbrite-" + e.URL
}

// findMatchingBrace returns the index of the '}' that closes the '{' at start.
// Handles strings (including escapes) so braces inside strings don't fool it.
func findMatchingBrace(b []byte, start int) (int, error) {
	if start >= len(b) || b[start] != '{' {
		return 0, errors.New("start is not '{'")
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(b); i++ {
		c := b[i]
		if escape {
			escape = false
			continue
		}
		if inStr {
			switch c {
			case '\\':
				escape = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unbalanced braces")
}

// parseEBDateTime combines Eventbrite's split date/time/timezone into a time.Time.
// Input: date="2026-07-08", time="18:00", tz="America/Winnipeg" or "".
// Returns zero time if date is empty.
func parseEBDateTime(date, t, tz string) time.Time {
	date = strings.TrimSpace(date)
	t = strings.TrimSpace(t)
	if date == "" {
		return time.Time{}
	}
	if t == "" {
		t = "00:00"
	}
	if len(t) == 5 {
		t += ":00"
	}
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", date+" "+t, loc)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
