// Package luma scrapes events from a Luma calendar via its public iCal feed.
package luma

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"event_calendar/internal/models"
)

// DefaultCalendarID is the Winnipeg Tech Thursday calendar.
const DefaultCalendarID = "cal-7F8pZGxrsjSQVRL"

type Scraper struct {
	client     *http.Client
	calendarID string
	userAgent  string
}

func NewScraper() *Scraper {
	return &Scraper{
		client:     &http.Client{Timeout: 30 * time.Second},
		calendarID: DefaultCalendarID,
		userAgent:  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
	}
}

func (s *Scraper) GetEvents(city, category string, period time.Duration) ([]models.Event, error) {
	ics, err := s.fetchICS()
	if err != nil {
		log.Printf("[Luma] fetch failed: %v", err)
		return nil, err
	}
	events, err := parseICS(ics)
	if err != nil {
		log.Printf("[Luma] parse failed: %v", err)
		return nil, err
	}
	cutoff := time.Now().Add(period)
	out := events[:0]
	for _, e := range events {
		if !e.StartTime.IsZero() && e.StartTime.After(cutoff) {
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
	log.Printf("[Luma] parsed %d events, %d within %s", len(events), len(out), period)
	return out, nil
}

func (s *Scraper) fetchICS() ([]byte, error) {
	url := fmt.Sprintf("https://api.luma.com/ics/get?entity=calendar&id=%s", s.calendarID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("luma status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// unfoldICS joins continuation lines per RFC 5545: lines starting with a space
// or tab continue the previous line. The leading whitespace is stripped.
func unfoldICS(b []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(b))
	lines := bytes.Split(b, []byte("\n"))
	for i, line := range lines {
		line = bytes.TrimRight(line, "\r")
		if i > 0 && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			out.Write(line[1:])
		} else {
			if i > 0 {
				out.WriteByte('\n')
			}
			out.Write(line)
		}
	}
	if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteByte('\n')
	}
	return out.Bytes()
}

var (
	lumaURLRE    = regexp.MustCompile(`https://luma\.com/[A-Za-z0-9_-]+`)
	icsDate      = "20060102T150405Z"
	icsDateLocal = "20060102T150405"
)

// parseICS extracts VEVENT blocks from an unfolded iCal stream.
func parseICS(b []byte) ([]models.Event, error) {
	b = unfoldICS(b)
	var out []models.Event
	var inEvent bool
	var fields map[string]string
	for _, raw := range bytes.Split(b, []byte("\n")) {
		line := string(raw)
		switch {
		case line == "BEGIN:VEVENT":
			inEvent = true
			fields = map[string]string{}
		case line == "END:VEVENT":
			if ev, ok := mapICSEvent(fields); ok {
				out = append(out, ev)
			}
			inEvent = false
			fields = nil
		default:
			if !inEvent {
				continue
			}
			key, val := splitICSLine(line)
			if key == "" {
				continue
			}
			fields[key] = val
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no VEVENT blocks parsed")
	}
	return out, nil
}

// splitICSLine returns the property name (everything before the first ; or :)
// and the value (everything after the first : that follows the name).
func splitICSLine(line string) (key, val string) {
	colon := strings.Index(line, ":")
	if colon < 0 {
		return "", ""
	}
	name := line[:colon]
	if semi := strings.Index(name, ";"); semi >= 0 {
		name = name[:semi]
	}
	return name, line[colon+1:]
}

func mapICSEvent(f map[string]string) (models.Event, bool) {
	name := unescapeICS(f["SUMMARY"])
	if name == "" {
		return models.Event{}, false
	}
	start := parseICSTime(f["DTSTART"])
	end := parseICSTime(f["DTEND"])

	loc := unescapeICS(f["LOCATION"])
	desc := unescapeICS(f["DESCRIPTION"])

	url := ""
	venue := loc
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		// Luma uses LOCATION to carry the external ticket URL (e.g. Eventbrite).
		url = strings.Fields(loc)[0]
		venue = ""
	}
	if url == "" {
		if m := lumaURLRE.FindString(desc); m != "" {
			url = m
		}
	}
	if url == "" {
		// Synthesize from UID as last resort.
		if uid := f["UID"]; uid != "" {
			url = "https://luma.com/" + uid
		}
	}
	if url == "" {
		return models.Event{}, false
	}

	return models.Event{
		ID:        "luma-" + f["UID"],
		Name:      name,
		URL:       url,
		StartTime: start,
		EndTime:   end,
		Venue:     venue,
		Source:    "luma",
	}, true
}

// unescapeICS reverses the ICS escape sequences (RFC 5545 §3.3.11).
func unescapeICS(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer(
		`\n`, "\n",
		`\N`, "\n",
		`\,`, ",",
		`\;`, ";",
		`\\`, `\`,
	)
	return strings.TrimSpace(r.Replace(s))
}

// parseICSTime parses both UTC (with trailing Z) and floating-local timestamps.
func parseICSTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(icsDate, s); err == nil {
		return t
	}
	if t, err := time.Parse(icsDateLocal, s); err == nil {
		return t
	}
	return time.Time{}
}
