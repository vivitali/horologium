// Command meetup is the AWS Lambda entry point for the monthly meetup bot.
// EventBridge invokes it daily; the code decides whether today is a poll
// day, a summary day, or nothing.
//
// Payload: {} normally, or {"date":"YYYY-MM-DD"} to simulate a day.
package main

import (
	"context"
	"fmt"
	"log"
	"time"
	_ "time/tzdata" // provided.al2023 has no zoneinfo

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"event_calendar/internal/config"
	"event_calendar/internal/meetup"
	"event_calendar/internal/ssmparam"
	"event_calendar/pkg/telegram"
)

// Event is the invocation payload.
type Event struct {
	Date string `json:"date,omitempty"`
}

const zone = "America/Winnipeg"

func main() {
	lambda.Start(handle)
}

func handle(ctx context.Context, evt Event) (res meetup.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v", r)
			res = meetup.Result{Error: fmt.Sprintf("panic: %v", r)}
			err = nil
		}
	}()

	cfg := config.Load()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return meetup.Result{Error: fmt.Sprintf("load tz %s: %v", zone, err)}, nil
	}
	now, err := resolveNow(evt.Date, loc)
	if err != nil {
		return meetup.Result{Error: err.Error()}, nil
	}
	log.Printf("meetup lambda invoked: now=%s test_mode=%t", now.In(loc).Format("2006-01-02 15:04 MST"), cfg.TestMode)
	if cfg.AnthropicAPIKey == "" {
		log.Printf("warning: ANTHROPIC_API_KEY not set; the AI poll option will be skipped")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return meetup.Result{Error: fmt.Sprintf("aws config: %v", err)}, nil
	}
	store := meetup.NewSSMStore(ssmparam.NewAWSAdapter(ssm.NewFromConfig(awsCfg)), cfg.MeetupSSMParam)

	return meetup.Run(ctx, meetup.Deps{
		Bot:      telegram.NewService(cfg.MeetupBotToken),
		Store:    store,
		Ideas:    meetup.NewClaudeIdeas(cfg.AnthropicAPIKey),
		BotToken: cfg.MeetupBotToken,
		ChatID:   cfg.MeetupChatID,
		TestMode: cfg.TestMode,
		Loc:      loc,
	}, now), nil
}

// resolveNow returns time.Now, or 09:00 local on the overridden date so the
// tick lands on the same civil day it would in production (14:00 UTC).
func resolveNow(date string, loc *time.Location) (time.Time, error) {
	if date == "" {
		return time.Now(), nil
	}
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad date %q (want YYYY-MM-DD): %w", date, err)
	}
	return d.Add(9 * time.Hour), nil
}
