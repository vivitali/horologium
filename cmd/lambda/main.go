// Command lambda is the AWS Lambda entry point. It dispatches to one of the
// job functions based on the "action" field in the invocation event.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"event_calendar/internal/annual"
	"event_calendar/internal/config"
	"event_calendar/internal/jobs"
	"event_calendar/internal/ssmparam"
	"event_calendar/pkg/scraping"
	"event_calendar/pkg/telegram"
)

// Event is the invocation payload. EventBridge rules set "action".
type Event struct {
	Action string `json:"action"`
}

func main() {
	lambda.Start(handle)
}

func handle(ctx context.Context, evt Event) (jobs.Result, error) {
	defer recoverPanic()

	cfg := config.Load()
	log.Printf("lambda invoked: action=%q city=%s test_mode=%t", evt.Action, cfg.City, cfg.TestMode)

	switch evt.Action {
	case "events", "":
		return runEvents(ctx, cfg), nil
	case "scrape_annual":
		return runAnnualScrape(ctx, cfg), nil
	default:
		return jobs.Result{Error: fmt.Sprintf("unknown action %q", evt.Action)}, nil
	}
}

func runEvents(ctx context.Context, cfg config.Config) jobs.Result {
	factory := scraping.NewScrapingServiceFactory()
	scraper := factory.CreateDefaultService()
	sender := telegram.NewService(cfg.BotToken)

	store, err := newAnnualStore(ctx, cfg)
	var annualEvts []annual.AnnualEvent
	if err != nil {
		log.Printf("events: SSM client init failed (footer will be empty): %v", err)
	} else {
		annualEvts, err = store.Get(ctx)
		if err != nil {
			log.Printf("events: SSM read failed (footer will be empty): %v", err)
			annualEvts = nil
		}
	}

	return jobs.RunEventsDigest(jobs.EventsDeps{
		Scraper: scraper,
		Sender:  sender,
		Now:     time.Now(),
	}, cfg, annualEvts)
}

func runAnnualScrape(ctx context.Context, cfg config.Config) jobs.Result {
	store, err := newAnnualStore(ctx, cfg)
	if err != nil {
		return jobs.Result{Error: fmt.Sprintf("ssm init: %v", err)}
	}
	return jobs.RunAnnualScrape(ctx, jobs.AnnualScrapeDeps{
		Store: store,
		Scrapers: []annual.SiteScraper{
			annual.NewPrairieDevCon(),
			annual.NewWomenHack(),
			annual.NewNorthForge(),
			annual.NewMbTechWeek(),
		},
		Now: time.Now(),
	})
}

// newAnnualStore builds an SSM-backed Store using default AWS credentials
// (the Lambda execution role).
func newAnnualStore(ctx context.Context, cfg config.Config) (annual.Store, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	client := ssm.NewFromConfig(awsCfg)
	return annual.NewSSMStore(ssmparam.NewAWSAdapter(client), cfg.AnnualSSMParam), nil
}

func recoverPanic() {
	if r := recover(); r != nil {
		log.Printf("PANIC: %v", r)
	}
}
