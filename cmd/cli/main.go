// Command cli runs the same jobs the Lambda runs, from your terminal.
// Useful for local testing without invoking Lambda.
//
// Usage:
//
//	go run ./cmd/cli -action=events
//	go run ./cmd/cli -action=scrape_annual                    # writes to SSM via default AWS creds
//	go run ./cmd/cli -action=scrape_annual -out=/tmp/x.json   # writes JSON to file, skips SSM
//	go run ./cmd/cli -action=meetup                           # today's tick, SSM state
//	go run ./cmd/cli -action=meetup -date=2026-10-20 -out=/tmp/meetup.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"
	_ "time/tzdata" // keep date math identical to the Lambda build

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"event_calendar/internal/annual"
	"event_calendar/internal/config"
	"event_calendar/internal/jobs"
	"event_calendar/internal/meetup"
	"event_calendar/internal/ssmparam"
	"event_calendar/pkg/scraping"
	"event_calendar/pkg/telegram"
)

func main() {
	action := flag.String("action", "events", "job to run: events, scrape_annual, or meetup")
	out := flag.String("out", "", "scrape_annual/meetup: write state JSON to this file instead of SSM")
	date := flag.String("date", "", "meetup only: simulate this day (YYYY-MM-DD)")
	flag.Parse()

	ctx := context.Background()
	cfg := config.Load()
	var result any
	var ok bool
	switch *action {
	case "events":
		r := runEvents(cfg)
		result, ok = r, r.Success
	case "scrape_annual":
		r := runAnnualScrape(ctx, cfg, *out)
		result, ok = r, r.Success
	case "meetup":
		r := runMeetup(ctx, cfg, *date, *out)
		result, ok = r, r.Success
	default:
		log.Fatalf("unknown action %q (want events, scrape_annual, or meetup)", *action)
	}

	js, _ := json.MarshalIndent(result, "", "  ")
	log.Printf("result:\n%s", js)
	if !ok {
		os.Exit(1)
	}
}

func runEvents(cfg config.Config) jobs.Result {
	factory := scraping.NewScrapingServiceFactory()
	scraper := factory.CreateDefaultService()
	sender := telegram.NewService(cfg.BotToken)
	return jobs.RunEventsDigest(jobs.EventsDeps{Scraper: scraper, Sender: sender, Now: time.Now()}, cfg, nil)
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

func runMeetup(ctx context.Context, cfg config.Config, date, stateFile string) meetup.Result {
	loc, err := time.LoadLocation("America/Winnipeg")
	if err != nil {
		return meetup.Result{Error: "load tz: " + err.Error()}
	}
	now := time.Now()
	if date != "" {
		d, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return meetup.Result{Error: "bad -date: " + err.Error()}
		}
		now = d.Add(9 * time.Hour)
	}

	var store meetup.Store
	if stateFile != "" {
		store = meetup.NewFileStore(stateFile)
	} else {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return meetup.Result{Error: "aws config: " + err.Error()}
		}
		store = meetup.NewSSMStore(ssmparam.NewAWSAdapter(ssm.NewFromConfig(awsCfg)), cfg.MeetupSSMParam)
	}

	return meetup.Run(ctx, meetup.Deps{
		Bot:      telegram.NewService(cfg.MeetupBotToken),
		Store:    store,
		Ideas:    meetup.NewClaudeIdeas(cfg.AnthropicAPIKey),
		BotToken: cfg.MeetupBotToken,
		ChatID:   cfg.MeetupChatID,
		TestMode: cfg.TestMode,
		Loc:      loc,
	}, now)
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
