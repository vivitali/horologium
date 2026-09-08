// Command cli runs the same jobs the Lambda runs, from your terminal.
// Useful for local testing without invoking Lambda.
//
// Usage:
//
//	go run ./cmd/cli -action=events
//	go run ./cmd/cli -action=poll
//	go run ./cmd/cli -action=scrape_annual                    # writes to SSM via default AWS creds
//	go run ./cmd/cli -action=scrape_annual -out=/tmp/x.json   # writes JSON to file, skips SSM
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"event_calendar/internal/annual"
	"event_calendar/internal/config"
	"event_calendar/internal/jobs"
	"event_calendar/internal/ssmparam"
	"event_calendar/pkg/scraping"
	"event_calendar/pkg/telegram"
)

func main() {
	action := flag.String("action", "events", "job to run: events, poll, or scrape_annual")
	out := flag.String("out", "", "scrape_annual only: write JSON to this file instead of SSM")
	flag.Parse()

	ctx := context.Background()
	cfg := config.Load()
	var result jobs.Result
	switch *action {
	case "events":
		result = runEvents(cfg)
	case "poll":
		result = runPoll(cfg)
	case "scrape_annual":
		result = runAnnualScrape(ctx, cfg, *out)
	default:
		log.Fatalf("unknown action %q (want events, poll, or scrape_annual)", *action)
	}

	js, _ := json.MarshalIndent(result, "", "  ")
	log.Printf("result:\n%s", js)
	if !result.Success {
		os.Exit(1)
	}
}

func runEvents(cfg config.Config) jobs.Result {
	factory := scraping.NewScrapingServiceFactory()
	scraper := factory.CreateDefaultService()
	sender := telegram.NewService(cfg.BotToken)
	return jobs.RunEventsDigest(jobs.EventsDeps{Scraper: scraper, Sender: sender, Now: time.Now()}, cfg, nil)
}

func runPoll(cfg config.Config) jobs.Result {
	poller := telegram.NewService(cfg.PollBotToken)
	return jobs.RunMonthlyPoll(jobs.PollDeps{Poller: poller}, cfg)
}

func runAnnualScrape(ctx context.Context, cfg config.Config, outFile string) jobs.Result {
	scrapers := []annual.SiteScraper{
		annual.NewPrairieDevCon(),
		annual.NewWomenHack(),
		annual.NewNorthForge(),
		annual.NewMbTechWeek(),
	}

	var store annual.Store
	if outFile != "" {
		store = &fileStore{path: outFile}
	} else {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return jobs.Result{Error: "aws config: " + err.Error()}
		}
		store = annual.NewSSMStore(
			ssmparam.NewAWSAdapter(ssm.NewFromConfig(awsCfg)),
			cfg.AnnualSSMParam,
		)
	}

	return jobs.RunAnnualScrape(ctx, jobs.AnnualScrapeDeps{
		Store: store, Scrapers: scrapers, Now: time.Now(),
	})
}

// fileStore writes the JSON blob to a local file and reads it back.
// Used by the CLI's -out flag for local inspection.
type fileStore struct{ path string }

func (f *fileStore) Get(ctx context.Context) ([]annual.AnnualEvent, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []annual.AnnualEvent{}, nil
		}
		return nil, err
	}
	var out []annual.AnnualEvent
	if len(b) == 0 {
		return out, nil
	}
	return out, json.Unmarshal(b, &out)
}

func (f *fileStore) Put(ctx context.Context, events []annual.AnnualEvent) error {
	b, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, b, 0o644)
}
