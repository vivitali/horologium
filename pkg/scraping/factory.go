package scraping

import (
	"log"
)

// ScrapingServiceFactory creates and configures scraping services
type ScrapingServiceFactory struct{}

// NewScrapingServiceFactory creates a new factory
func NewScrapingServiceFactory() *ScrapingServiceFactory {
	return &ScrapingServiceFactory{}
}

// CreateDefaultService creates a scraping service with all default scrapers
func (f *ScrapingServiceFactory) CreateDefaultService() *ScrapingService {
	service := NewScrapingService()

	// Register default scrapers
	service.RegisterScraper("meetup", NewMeetupScraper())
	service.RegisterScraper("eventbrite", NewEventbriteScraper())
	service.RegisterScraper("luma", NewLumaScraper())

	log.Printf("Created scraping service with %d scrapers", len(service.GetRegisteredScrapers()))
	return service
}

// CreateServiceWithScrapers creates a scraping service with specific scrapers
func (f *ScrapingServiceFactory) CreateServiceWithScrapers(scraperNames []string) *ScrapingService {
	service := NewScrapingService()

	for _, name := range scraperNames {
		switch name {
		case "meetup":
			service.RegisterScraper("meetup", NewMeetupScraper())
		case "eventbrite":
			service.RegisterScraper("eventbrite", NewEventbriteScraper())
		case "luma":
			service.RegisterScraper("luma", NewLumaScraper())
		default:
			log.Printf("Warning: Unknown scraper name '%s', skipping", name)
		}
	}

	log.Printf("Created scraping service with %d scrapers: %v", len(service.GetRegisteredScrapers()), service.GetRegisteredScrapers())
	return service
}

// CreateMeetupOnlyService creates a service with only the Meetup scraper
func (f *ScrapingServiceFactory) CreateMeetupOnlyService() *ScrapingService {
	return f.CreateServiceWithScrapers([]string{"meetup"})
}

// CreateEventbriteOnlyService creates a service with only the Eventbrite scraper
func (f *ScrapingServiceFactory) CreateEventbriteOnlyService() *ScrapingService {
	return f.CreateServiceWithScrapers([]string{"eventbrite"})
}
