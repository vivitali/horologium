package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "main-token")
	t.Setenv("TELEGRAM_CHAT_ID", "main-chat")
	for _, k := range []string{"CITY", "CATEGORIES", "PERIOD_DAYS", "TEST_MODE",
		"TELEGRAM_MEETUP_BOT_TOKEN", "TELEGRAM_MEETUP_CHAT_ID", "ANTHROPIC_API_KEY", "MEETUP_SSM_PARAM", "ANNUAL_SSM_PARAM"} {
		t.Setenv(k, "")
	}
	cfg := Load()
	if cfg.BotToken != "main-token" || cfg.ChatID != "main-chat" {
		t.Errorf("main creds: %+v", cfg)
	}
	if cfg.City != "Winnipeg" || cfg.Categories != "tech" || cfg.PeriodDays != 30 || cfg.TestMode {
		t.Errorf("defaults: %+v", cfg)
	}
	if cfg.AnnualSSMParam != "/winnipeg-tech-events/annual-events" {
		t.Errorf("annual param: %q", cfg.AnnualSSMParam)
	}
	if cfg.MeetupSSMParam != "/winnipeg-tech-events/meetup-state" {
		t.Errorf("meetup param: %q", cfg.MeetupSSMParam)
	}
	if cfg.MeetupBotToken != "" || cfg.MeetupChatID != "" || cfg.AnthropicAPIKey != "" {
		t.Errorf("meetup creds should be empty by default: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "main-token")
	t.Setenv("TELEGRAM_CHAT_ID", "main-chat")
	t.Setenv("TELEGRAM_MEETUP_BOT_TOKEN", "meet-token")
	t.Setenv("TELEGRAM_MEETUP_CHAT_ID", "meet-chat")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	t.Setenv("MEETUP_SSM_PARAM", "/custom/meetup")
	t.Setenv("CITY", "Brandon")
	t.Setenv("CATEGORIES", "business")
	t.Setenv("PERIOD_DAYS", "14")
	t.Setenv("TEST_MODE", "true")
	cfg := Load()
	if cfg.MeetupBotToken != "meet-token" || cfg.MeetupChatID != "meet-chat" || cfg.AnthropicAPIKey != "sk-ant" {
		t.Errorf("meetup creds: %+v", cfg)
	}
	if cfg.MeetupSSMParam != "/custom/meetup" {
		t.Errorf("meetup param: %q", cfg.MeetupSSMParam)
	}
	if cfg.City != "Brandon" || cfg.Categories != "business" || cfg.PeriodDays != 14 || !cfg.TestMode {
		t.Errorf("overrides: %+v", cfg)
	}
}

func TestLoad_InvalidPeriodDaysFallsBack(t *testing.T) {
	t.Setenv("PERIOD_DAYS", "abc")
	if got := Load().PeriodDays; got != 30 {
		t.Errorf("PeriodDays = %d, want 30", got)
	}
	t.Setenv("PERIOD_DAYS", "-5")
	if got := Load().PeriodDays; got != 30 {
		t.Errorf("PeriodDays = %d, want 30", got)
	}
}
