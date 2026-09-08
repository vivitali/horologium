package scraping

import (
	"event_calendar/internal/models"
	"fmt"
	"log"
	"time"

	"github.com/go-resty/resty/v2"
)

// BaseScraper provides common functionality for all scrapers
type BaseScraper struct {
	name      string
	client    *resty.Client
	baseURL   string
	healthy   bool
	lastCheck time.Time
}

// NewBaseScraper creates a new base scraper
func NewBaseScraper(name, baseURL string) *BaseScraper {
	return &BaseScraper{
		name:    name,
		baseURL: baseURL,
		client: resty.New().
			SetTimeout(30*time.Second).
			SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36").
			SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8").
			SetHeader("Accept-Language", "en-US,en;q=0.5").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Connection", "keep-alive").
			SetHeader("Upgrade-Insecure-Requests", "1"),
		healthy:   true,
		lastCheck: time.Now(),
	}
}

// GetName returns the scraper name
func (b *BaseScraper) GetName() string {
	return b.name
}

// IsHealthy returns the health status of the scraper
func (b *BaseScraper) IsHealthy() bool {
	// Check if we haven't checked health in the last 5 minutes
	if time.Since(b.lastCheck) > 5*time.Minute {
		b.checkHealth()
	}
	return b.healthy
}

// checkHealth performs a health check on the scraper
func (b *BaseScraper) checkHealth() {
	b.lastCheck = time.Now()

	// Try to make a simple request to check if the service is accessible
	resp, err := b.client.R().Get(b.baseURL)
	if err != nil {
		log.Printf("Health check failed for %s: %v", b.name, err)
		b.healthy = false
		return
	}

	if resp.StatusCode() >= 200 && resp.StatusCode() < 400 {
		b.healthy = true
	} else {
		log.Printf("Health check failed for %s: status code %d", b.name, resp.StatusCode())
		b.healthy = false
	}
}

// GetClient returns the HTTP client
func (b *BaseScraper) GetClient() *resty.Client {
	return b.client
}

// GetBaseURL returns the base URL
func (b *BaseScraper) GetBaseURL() string {
	return b.baseURL
}

// SetHealthy sets the health status
func (b *BaseScraper) SetHealthy(healthy bool) {
	b.healthy = healthy
}

// ValidateEvent validates that an event has required fields
func (b *BaseScraper) ValidateEvent(event models.Event) error {
	if event.Name == "" {
		return fmt.Errorf("event name is required")
	}
	if event.URL == "" {
		return fmt.Errorf("event URL is required")
	}
	if event.Source == "" {
		return fmt.Errorf("event source is required")
	}
	return nil
}

// FilterEventsByPeriod filters events to only include those within the specified period
func (b *BaseScraper) FilterEventsByPeriod(events []models.Event, period time.Duration) []models.Event {
	now := time.Now()
	futureLimit := now.Add(period)
	var filtered []models.Event

	for _, event := range events {
		// If no start time, assume it's in the future
		if event.StartTime.IsZero() {
			filtered = append(filtered, event)
			continue
		}

		// Check if event is in the future and within the period
		if event.StartTime.After(now) && event.StartTime.Before(futureLimit) {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// RemoveDuplicateEvents removes duplicate events based on URL and name
func (b *BaseScraper) RemoveDuplicateEvents(events []models.Event) []models.Event {
	seen := make(map[string]bool)
	var unique []models.Event

	for _, event := range events {
		key := event.URL + "|" + event.Name
		if !seen[key] {
			seen[key] = true
			unique = append(unique, event)
		}
	}

	return unique
}

// LogScrapingResult logs the result of a scraping operation
func (b *BaseScraper) LogScrapingResult(events []models.Event, err error) {
	if err != nil {
		log.Printf("Scraping failed for %s: %v", b.name, err)
	} else {
		log.Printf("Scraping successful for %s: found %d events", b.name, len(events))
	}
}
