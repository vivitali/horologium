// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
)

// Config holds all runtime settings for both Lambdas. Fields not relevant
// to a binary are simply left empty by the environment.
type Config struct {
	// Events digest bot.
	BotToken   string
	ChatID     string
	City       string
	Categories string
	PeriodDays int

	// Meetup bot (separate Telegram bot, separate Lambda).
	MeetupBotToken  string
	MeetupChatID    string
	MeetupSSMParam  string
	AnthropicAPIKey string

	// Shared.
	TestMode       bool
	AnnualSSMParam string
}

// Load reads environment variables and applies defaults.
func Load() Config {
	return Config{
		BotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		ChatID:     os.Getenv("TELEGRAM_CHAT_ID"),
		City:       envOr("CITY", "Winnipeg"),
		Categories: envOr("CATEGORIES", "tech"),
		PeriodDays: envInt("PERIOD_DAYS", 30),

		MeetupBotToken:  os.Getenv("TELEGRAM_MEETUP_BOT_TOKEN"),
		MeetupChatID:    os.Getenv("TELEGRAM_MEETUP_CHAT_ID"),
		MeetupSSMParam:  envOr("MEETUP_SSM_PARAM", "/winnipeg-tech-events/meetup-state"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),

		TestMode:       os.Getenv("TEST_MODE") == "true",
		AnnualSSMParam: envOr("ANNUAL_SSM_PARAM", "/winnipeg-tech-events/annual-events"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
