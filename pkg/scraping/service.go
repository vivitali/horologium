package scraping

import (
	"event_calendar/internal/models"
	"fmt"
	"log"
	"sync"
	"time"
)

// EventScraper defines the interface for event scrapers
type EventScraper interface {
	GetEvents(city, category string, period time.Duration) ([]models.Event, error)
	GetName() string
	IsHealthy() bool
}

// ScrapingService manages multiple event scrapers
type ScrapingService struct {
	scrapers map[string]EventScraper
	mu       sync.RWMutex
}

// NewScrapingService creates a new scraping service
func NewScrapingService() *ScrapingService {
	return &ScrapingService{
		scrapers: make(map[string]EventScraper),
	}
}

// RegisterScraper adds a new scraper to the service
func (s *ScrapingService) RegisterScraper(name string, scraper EventScraper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scrapers[name] = scraper
	log.Printf("Registered scraper: %s", name)
}

// GetScraper retrieves a scraper by name
func (s *ScrapingService) GetScraper(name string) (EventScraper, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	scraper, exists := s.scrapers[name]
	return scraper, exists
}

// GetAllScrapers returns all registered scrapers
func (s *ScrapingService) GetAllScrapers() map[string]EventScraper {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	scrapers := make(map[string]EventScraper)
	for name, scraper := range s.scrapers {
		scrapers[name] = scraper
	}
	return scrapers
}

// ScrapeEvents scrapes events from all registered scrapers
func (s *ScrapingService) ScrapeEvents(city, category string, period time.Duration) ([]models.Event, error) {
	s.mu.RLock()
	scrapers := make(map[string]EventScraper)
	for name, scraper := range s.scrapers {
		scrapers[name] = scraper
	}
	s.mu.RUnlock()

	if len(scrapers) == 0 {
		log.Printf("❌ No scrapers registered in the service")
		return nil, fmt.Errorf("no scrapers registered")
	}

	log.Printf("🚀 Starting scraping process for city: %s, category: %s, period: %v", city, category, period)
	log.Printf("📋 Registered scrapers: %v", s.GetRegisteredScrapers())

	var allEvents []models.Event
	var errors []error
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Scrape from all sources concurrently
	for name, scraper := range scrapers {
		wg.Add(1)
		go func(scraperName string, scraper EventScraper) {
			defer wg.Done()

			log.Printf("🔄 Starting scraping from %s", scraperName)
			startTime := time.Now()

			events, err := scraper.GetEvents(city, category, period)
			duration := time.Since(startTime)

			mu.Lock()
			if err != nil {
				log.Printf("❌ Error scraping from %s after %v: %v", scraperName, duration, err)
				errors = append(errors, fmt.Errorf("%s: %w", scraperName, err))
			} else {
				log.Printf("✅ Successfully scraped %d events from %s in %v", len(events), scraperName, duration)
				if len(events) > 0 {
					log.Printf("📊 Sample event from %s: %s", scraperName, events[0].Name)
				}
				allEvents = append(allEvents, events...)
			}
			mu.Unlock()
		}(name, scraper)
	}

	wg.Wait()

	// Log detailed summary
	log.Printf("📈 Scraping Summary:")
	log.Printf("   Total events found: %d", len(allEvents))
	log.Printf("   Successful scrapers: %d", len(scrapers)-len(errors))
	log.Printf("   Failed scrapers: %d", len(errors))

	if len(errors) > 0 {
		log.Printf("⚠️  Scraper errors:")
		for _, err := range errors {
			log.Printf("   - %v", err)
		}
	}

	// Log event sources breakdown
	sourceCount := make(map[string]int)
	for _, event := range allEvents {
		sourceCount[event.Source]++
	}
	log.Printf("📊 Events by source: %v", sourceCount)

	// Return events even if some scrapers failed
	return allEvents, nil
}

// ScrapeEventsFromSource scrapes events from a specific source
func (s *ScrapingService) ScrapeEventsFromSource(source, city, category string, period time.Duration) ([]models.Event, error) {
	scraper, exists := s.GetScraper(source)
	if !exists {
		return nil, fmt.Errorf("scraper '%s' not found", source)
	}

	log.Printf("Scraping events from %s for city: %s, category: %s", source, city, category)
	events, err := scraper.GetEvents(city, category, period)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape from %s: %w", source, err)
	}

	log.Printf("Successfully scraped %d events from %s", len(events), source)
	return events, nil
}

// GetHealthStatus returns the health status of all scrapers
func (s *ScrapingService) GetHealthStatus() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := make(map[string]bool)
	for name, scraper := range s.scrapers {
		status[name] = scraper.IsHealthy()
	}
	return status
}

// GetRegisteredScrapers returns a list of registered scraper names
func (s *ScrapingService) GetRegisteredScrapers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var names []string
	for name := range s.scrapers {
		names = append(names, name)
	}
	return names
}

// RemoveScraper removes a scraper from the service
func (s *ScrapingService) RemoveScraper(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scrapers[name]; exists {
		delete(s.scrapers, name)
		log.Printf("Removed scraper: %s", name)
	}
}

// ClearAllScrapers removes all scrapers from the service
func (s *ScrapingService) ClearAllScrapers() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scrapers = make(map[string]EventScraper)
	log.Printf("Cleared all scrapers")
}
