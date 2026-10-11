# Meetup Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A second Lambda (`cmd/meetup`) run by a dedicated Telegram bot that, once a month, polls the group about the last weekend of the month (with a Haiku-generated idea option), reads counts back via `stopPoll`, and posts + pins the decision.

**Architecture:** Daily EventBridge tick → `meetup.Run` computes the last weekend in `America/Winnipeg`, reads a JSON state blob from SSM, and either sends the poll (S-11), stops it and summarizes (S-3), or no-ops. No inbound HTTP. Old `poll` action/rule/config removed.

**Tech Stack:** Go 1.24 module `event_calendar`, `aws-lambda-go`, `aws-sdk-go-v2/service/ssm`, `github.com/anthropics/anthropic-sdk-go` v1.71.0, Telegram Bot API via `net/http`, bash deploy scripts, GitHub Actions OIDC.

**Spec:** `docs/superpowers/specs/2026-09-08-meetup-bot-design.md`

## Global Constraints

- Module path is `event_calendar`; all imports use it.
- Ukrainian copy exactly as in the spec: question `Зустріч в останні вихідні місяця (%s)?`; last option `❌ Цього місяця не можу`.
- Time zone `America/Winnipeg`; every binary that computes dates imports `_ "time/tzdata"`.
- Winner threshold is **4** votes (`MinVotes = 4`).
- Model id is exactly `claude-haiku-4-5`.
- Telegram limits: question ≤ 300 chars, option ≤ 100 chars, 2–10 options.
- SSM parameter names: `/winnipeg-tech-events/meetup-state` (new), `/winnipeg-tech-events/annual-events` (existing).
- Lambda names: `winnipeg-tech-events` (existing), `winnipeg-meetup-bot` (new). Old rule `winnipeg-poll-monthly` must be deleted by deploy.
- Tests: `go test ./...` and `go vet ./...` must pass after every task. Commit after every task.
- Branch: `claude/meetup-bot` off `main`. Never commit to `main`.

---

### Task 1: Extract `internal/ssmparam` from `internal/annual`

**Files:**
- Create: `internal/ssmparam/ssmparam.go`
- Create: `internal/ssmparam/aws.go`
- Modify: `internal/annual/store.go` (drop local `ErrParameterNotFound`/`ssmAPI` definitions, alias to ssmparam)
- Delete: `internal/annual/ssm_aws.go`
- Modify: `cmd/lambda/main.go:110-117`, `cmd/cli/main.go:83-86`
- Test: existing `internal/annual/store_test.go` keeps passing

**Interfaces:**
- Produces: `ssmparam.API` interface `{ GetParameter(ctx, name) (string, error); PutParameter(ctx, name, value) error }`, `ssmparam.ErrNotFound`, `ssmparam.NewAWSAdapter(c AWSClient) API`, `ssmparam.AWSClient` interface.

- [ ] **Step 1: Create the package**

`internal/ssmparam/ssmparam.go`:

```go
// Package ssmparam is a thin, test-friendly abstraction over one AWS SSM
// Parameter Store parameter. Stores that persist JSON blobs between Lambda
// invocations build on it.
package ssmparam

import (
	"context"
	"errors"
)

// ErrNotFound is returned by API.GetParameter when the parameter does not
// exist. Callers typically treat it as "empty".
var ErrNotFound = errors.New("parameter not found")

// API is the slice of SSM we need. Tests substitute a fake.
type API interface {
	GetParameter(ctx context.Context, name string) (string, error)
	PutParameter(ctx context.Context, name, value string) error
}
```

`internal/ssmparam/aws.go` (moved from `internal/annual/ssm_aws.go`, renamed):

```go
package ssmparam

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// AWSClient is the subset of the SDK ssm.Client we use.
type AWSClient interface {
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
}

// NewAWSAdapter wraps an SDK client so it satisfies API.
func NewAWSAdapter(c AWSClient) API {
	return &awsAdapter{c: c}
}

type awsAdapter struct {
	c AWSClient
}

func (a *awsAdapter) GetParameter(ctx context.Context, name string) (string, error) {
	out, err := a.c.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(name)})
	if err != nil {
		var nf *ssmtypes.ParameterNotFound
		if errors.As(err, &nf) {
			return "", ErrNotFound
		}
		return "", err
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", nil
	}
	return *out.Parameter.Value, nil
}

func (a *awsAdapter) PutParameter(ctx context.Context, name, value string) error {
	_, err := a.c.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(value),
		Type:      ssmtypes.ParameterTypeString,
		Tier:      ssmtypes.ParameterTierStandard,
		Overwrite: aws.Bool(true),
	})
	return err
}
```

- [ ] **Step 2: Point `annual` at it**

In `internal/annual/store.go` replace the `ErrParameterNotFound` var and the `ssmAPI` interface with:

```go
// ErrParameterNotFound is kept as an alias so existing tests and callers
// keep compiling; new code should use ssmparam.ErrNotFound.
var ErrParameterNotFound = ssmparam.ErrNotFound

// ssmAPI is the parameter-store abstraction shared with other stores.
type ssmAPI = ssmparam.API
```

Add `"event_calendar/internal/ssmparam"` to the import block and remove the now-unused `"errors"` import only if nothing else in the file uses it (`errors.Is` is still used in `Get`, so keep it).

Delete `internal/annual/ssm_aws.go` (`git rm`).

Replace `NewAWSSSMAdapter` with a compatibility shim at the bottom of `store.go`:

```go
// NewAWSSSMAdapter is retained for callers; it delegates to ssmparam.
func NewAWSSSMAdapter(c ssmparam.AWSClient) ssmAPI {
	return ssmparam.NewAWSAdapter(c)
}
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS; `cmd/lambda` and `cmd/cli` compile unchanged because the shim keeps the name.

- [ ] **Step 4: Commit**

```bash
git add internal/ssmparam internal/annual
git commit -m "refactor(ssm): extract internal/ssmparam from annual store"
```

---

### Task 2: Telegram client — result-returning calls

**Files:**
- Modify: `pkg/telegram/service.go`
- Modify: `internal/digest/format.go:264` (`escMD` delegates to `telegram.EscapeMarkdownV2`)
- Create: `pkg/telegram/service_test.go`

**Interfaces:**
- Produces on `*telegram.Service`:
  - `SendPoll(chatID, question string, options []string, allowMultiple bool) (int64, error)` — returns `message_id`
  - `StopPoll(chatID string, messageID int64) ([]int, error)` — `voter_count` per option, in order
  - `SendMessageID(chatID, text string) (int64, error)` — MarkdownV2, returns `message_id`
  - `SendMessage(chatID, text string) error` — unchanged signature, now wraps `SendMessageID`
  - `PinChatMessage(chatID string, messageID int64) error`
  - package func `EscapeMarkdownV2(s string) string`
- Removes: `SendMonthlyMeetupPoll` (its only caller goes away in Task 3; delete it here and Task 3 fixes the build — so run Task 3 before `go build` if you execute out of order; otherwise keep the method until Task 3 and delete it there).

- [ ] **Step 1: Write failing tests**

`pkg/telegram/service_test.go`:

```go
package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestService returns a Service pointed at a fake Telegram API that
// records the last request body and replies with the given JSON.
func newTestService(t *testing.T, reply string) (*Service, *string, *string) {
	t.Helper()
	var lastPath, lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastPath, lastBody = r.URL.Path, string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	s := NewService("tok")
	s.baseURL = srv.URL + "/bottok"
	return s, &lastPath, &lastBody
}

func TestSendPoll_ReturnsMessageID(t *testing.T) {
	s, path, body := newTestService(t, `{"ok":true,"result":{"message_id":42}}`)
	id, err := s.SendPoll("-1", "Q?", []string{"a", "b"}, true)
	if err != nil || id != 42 {
		t.Fatalf("got id=%d err=%v", id, err)
	}
	if *path != "/bottok/sendPoll" {
		t.Errorf("path = %s", *path)
	}
	var req map[string]any
	_ = json.Unmarshal([]byte(*body), &req)
	if req["is_anonymous"] != false || req["allows_multiple_answers"] != true {
		t.Errorf("unexpected body: %s", *body)
	}
}

func TestSendPoll_RejectsBadOptionCount(t *testing.T) {
	s, _, _ := newTestService(t, `{"ok":true}`)
	if _, err := s.SendPoll("-1", "Q?", []string{"only"}, false); err == nil {
		t.Error("expected error for 1 option")
	}
}

func TestStopPoll_ReturnsCountsInOrder(t *testing.T) {
	s, path, _ := newTestService(t, `{"ok":true,"result":{"id":"p","question":"Q","options":[{"text":"a","voter_count":3},{"text":"b","voter_count":0},{"text":"c","voter_count":5}]}}`)
	counts, err := s.StopPoll("-1", 42)
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/bottok/stopPoll" {
		t.Errorf("path = %s", *path)
	}
	want := []int{3, 0, 5}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("counts = %v, want %v", counts, want)
		}
	}
}

func TestSendMessageID_ReturnsID(t *testing.T) {
	s, _, _ := newTestService(t, `{"ok":true,"result":{"message_id":7}}`)
	id, err := s.SendMessageID("-1", "hi")
	if err != nil || id != 7 {
		t.Fatalf("got id=%d err=%v", id, err)
	}
}

func TestPinChatMessage_SendsIDs(t *testing.T) {
	s, path, body := newTestService(t, `{"ok":true,"result":true}`)
	if err := s.PinChatMessage("-1", 7); err != nil {
		t.Fatal(err)
	}
	if *path != "/bottok/pinChatMessage" {
		t.Errorf("path = %s", *path)
	}
	var req map[string]any
	_ = json.Unmarshal([]byte(*body), &req)
	if req["message_id"] != float64(7) || req["disable_notification"] != true {
		t.Errorf("unexpected body: %s", *body)
	}
}

func TestAPIError_IsReturned(t *testing.T) {
	s, _, _ := newTestService(t, `{"ok":false,"description":"Bad Request: nope"}`)
	if _, err := s.SendMessageID("-1", "hi"); err == nil {
		t.Error("expected API error")
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	got := EscapeMarkdownV2("a_b*c.d-e(f)")
	want := `a\_b\*c\.d\-e\(f\)`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/telegram/`
Expected: compile errors (`StopPoll`, `SendMessageID`, `PinChatMessage`, `EscapeMarkdownV2` undefined; `SendPoll` returns one value).

- [ ] **Step 3: Implement**

In `pkg/telegram/service.go`:

Add `"strings"` to imports. Replace `apiResponse`, `post`, `SendMessage`, `SendPoll`, and `SendMonthlyMeetupPoll` with:

```go
type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

type messageResult struct {
	MessageID int64 `json:"message_id"`
}

type pollResult struct {
	Options []struct {
		Text       string `json:"text"`
		VoterCount int    `json:"voter_count"`
	} `json:"options"`
}

// SendMessage sends a MarkdownV2-formatted message to chatID.
func (s *Service) SendMessage(chatID, message string) error {
	_, err := s.SendMessageID(chatID, message)
	return err
}

// SendMessageID sends a MarkdownV2-formatted message and returns its message_id.
func (s *Service) SendMessageID(chatID, message string) (int64, error) {
	if s.botToken == "" {
		return 0, fmt.Errorf("bot token not configured")
	}
	if chatID == "" {
		return 0, fmt.Errorf("chat ID not provided")
	}
	if message == "" {
		return 0, fmt.Errorf("message is empty")
	}
	if len(message) > 4096 {
		return 0, fmt.Errorf("message too long (%d characters, max 4096)", len(message))
	}
	var res messageResult
	if err := s.post("/sendMessage", sendMessageRequest{
		ChatID:                chatID,
		Text:                  message,
		ParseMode:             "MarkdownV2",
		DisableWebPagePreview: true,
	}, &res); err != nil {
		return 0, err
	}
	return res.MessageID, nil
}

// SendPoll sends a non-anonymous poll and returns its message_id.
// Telegram requires 2-10 options.
func (s *Service) SendPoll(chatID, question string, options []string, allowMultiple bool) (int64, error) {
	if s.botToken == "" {
		return 0, fmt.Errorf("bot token not configured")
	}
	if chatID == "" {
		return 0, fmt.Errorf("chat ID not provided")
	}
	if question == "" {
		return 0, fmt.Errorf("question is empty")
	}
	if len(options) < 2 || len(options) > 10 {
		return 0, fmt.Errorf("poll requires 2-10 options, got %d", len(options))
	}
	var res messageResult
	if err := s.post("/sendPoll", sendPollRequest{
		ChatID:                chatID,
		Question:              question,
		Options:               options,
		IsAnonymous:           false,
		Type:                  "regular",
		AllowsMultipleAnswers: allowMultiple,
	}, &res); err != nil {
		return 0, err
	}
	return res.MessageID, nil
}

type stopPollRequest struct {
	ChatID    string `json:"chat_id"`
	MessageID int64  `json:"message_id"`
}

// StopPoll closes the poll and returns voter counts per option, in the
// order the options were sent. Only works on polls this bot sent.
func (s *Service) StopPoll(chatID string, messageID int64) ([]int, error) {
	var res pollResult
	if err := s.post("/stopPoll", stopPollRequest{ChatID: chatID, MessageID: messageID}, &res); err != nil {
		return nil, err
	}
	counts := make([]int, len(res.Options))
	for i, o := range res.Options {
		counts[i] = o.VoterCount
	}
	return counts, nil
}

type pinRequest struct {
	ChatID              string `json:"chat_id"`
	MessageID           int64  `json:"message_id"`
	DisableNotification bool   `json:"disable_notification"`
}

// PinChatMessage pins a message. The bot must be an admin with pin rights.
func (s *Service) PinChatMessage(chatID string, messageID int64) error {
	return s.post("/pinChatMessage", pinRequest{ChatID: chatID, MessageID: messageID, DisableNotification: true}, nil)
}

// mdV2Escape escapes every character Telegram MarkdownV2 treats as special.
var mdV2Escape = strings.NewReplacer(
	`\`, `\\`, "_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
	"~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=",
	"|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
)

// EscapeMarkdownV2 makes arbitrary text safe inside a MarkdownV2 message.
func EscapeMarkdownV2(s string) string { return mdV2Escape.Replace(s) }

// post sends body as JSON and, when out is non-nil, decodes the "result"
// field into it.
func (s *Service) post(path string, body any, out any) error {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	resp, err := s.client.Post(s.baseURL+path, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var r apiResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if !r.OK {
		return fmt.Errorf("telegram API error: %s", r.Description)
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("parse result: %w", err)
		}
	}
	return nil
}
```

Delete `SendMonthlyMeetupPoll`. Update the package doc comment to "used to send event digests, polls, pins, and alerts".

In `internal/digest/format.go`, replace the `mdV2Escape` replacer and `escMD` with:

```go
func escMD(s string) string { return telegram.EscapeMarkdownV2(s) }
```

and add `"event_calendar/pkg/telegram"` to its imports. First compare the removed replacer against `telegram.mdV2Escape` character-for-character; they must escape the same set (`_ * [ ] ( ) ~ \` > # + - = | { } . ! \`). Keep `mdV2URL` and `mdV2Code` as they are.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/telegram/ ./internal/digest/`
Expected: PASS. (`go build ./...` fails until Task 3 removes the `SendMonthlyMeetupPoll` caller — that is expected; do Task 3 immediately.)

- [ ] **Step 5: Commit**

```bash
git add pkg/telegram internal/digest/format.go
git commit -m "feat(telegram): result-returning sendPoll, stopPoll, pin, MarkdownV2 escape"
```

---

### Task 3: Config for the meetup bot; retire the old poll job

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`
- Delete: `internal/jobs/poll.go`, `internal/jobs/poll_test.go`
- Modify: `cmd/lambda/main.go` (remove `poll` case + `runPoll`)
- Modify: `cmd/cli/main.go` (remove `poll` case + `runPoll`; `meetup` is added in Task 10)

**Interfaces:**
- Produces `config.Config` fields: `MeetupBotToken string`, `MeetupChatID string`, `MeetupSSMParam string`, `AnthropicAPIKey string`. Removes `PollBotToken`, `PollChatID`.

- [ ] **Step 1: Update config tests**

Replace `internal/config/config_test.go` entirely:

```go
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
```

Before overwriting, read the existing file and keep any extra test cases it has that are not about poll creds (e.g. there is an invalid-`PERIOD_DAYS` test; the one above covers it).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/`
Expected: compile error — `MeetupSSMParam` etc. undefined.

- [ ] **Step 3: Implement config**

Replace the `Config` struct and `Load` in `internal/config/config.go`:

```go
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
```

Update the doc comment above the struct (remove the "Poll fields default to…" sentence).

- [ ] **Step 4: Remove the old poll job and its callers**

```bash
git rm internal/jobs/poll.go internal/jobs/poll_test.go
```

`cmd/lambda/main.go`: delete the `case "poll":` line and the `runPoll` function. Update the file's doc comment if it lists actions.

`cmd/cli/main.go`: delete the `case "poll":` line, the `runPoll` function, the `go run ./cmd/cli -action=poll` usage line, and change the flag help to `"job to run: events or scrape_annual"` and the `log.Fatalf` text accordingly (Task 10 adds `meetup`).

`internal/jobs/events.go`: change the package doc comment to "Package jobs contains the reusable scheduled-job logic for the events digest and annual-events scrape."

- [ ] **Step 5: Build, vet, test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add -A internal/config internal/jobs cmd/lambda cmd/cli
git commit -m "feat(config): meetup bot settings; retire day-of-week poll job"
```

---

### Task 4: `internal/meetup/schedule.go` — last weekend + step decision

**Files:**
- Create: `internal/meetup/schedule.go`
- Test: `internal/meetup/schedule_test.go`

**Interfaces:**
- Produces:
  - `type Weekend struct { Fri, Sat, Sun time.Time }` (midnight civil dates in the given location)
  - `func LastWeekend(year int, month time.Month, loc *time.Location) Weekend`
  - `func (w Weekend) Key() string` → `"2006-01"` of `Sat`
  - `func (w Weekend) PollDay() time.Time` = Sat-11, `func (w Weekend) SummaryDay() time.Time` = Sat-3
  - `type Step int` with `StepNone`, `StepSendPoll`, `StepSummarize`
  - `type Phase string` with `PhaseNew Phase = ""`, `PhasePolled = "polled"`, `PhaseDone = "done"`
  - `func NextStep(today time.Time, w Weekend, phase Phase) Step`
  - `func DateOnly(t time.Time, loc *time.Location) time.Time`

- [ ] **Step 1: Write failing tests**

`internal/meetup/schedule_test.go`:

```go
package meetup

import (
	"testing"
	"time"
)

var wpg = mustLoc("America/Winnipeg")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, wpg)
}

func TestLastWeekend(t *testing.T) {
	cases := []struct {
		y             int
		m             time.Month
		fri, sat, sun time.Time
	}{
		{2026, time.October, d(2026, 10, 30), d(2026, 10, 31), d(2026, 11, 1)}, // Sunday spills into November
		{2026, time.September, d(2026, 9, 25), d(2026, 9, 26), d(2026, 9, 27)},
		{2026, time.February, d(2026, 2, 27), d(2026, 2, 28), d(2026, 3, 1)},
		{2027, time.January, d(2027, 1, 29), d(2027, 1, 30), d(2027, 1, 31)},
	}
	for _, c := range cases {
		w := LastWeekend(c.y, c.m, wpg)
		if !w.Fri.Equal(c.fri) || !w.Sat.Equal(c.sat) || !w.Sun.Equal(c.sun) {
			t.Errorf("%d-%02d: got %v/%v/%v want %v/%v/%v", c.y, c.m, w.Fri, w.Sat, w.Sun, c.fri, c.sat, c.sun)
		}
		if w.Sat.Weekday() != time.Saturday {
			t.Errorf("%d-%02d: Sat is %v", c.y, c.m, w.Sat.Weekday())
		}
	}
}

func TestWeekend_KeyAndDays(t *testing.T) {
	w := LastWeekend(2026, time.October, wpg)
	if w.Key() != "2026-10" {
		t.Errorf("Key = %q", w.Key())
	}
	if !w.PollDay().Equal(d(2026, 10, 20)) || w.PollDay().Weekday() != time.Tuesday {
		// 31 Oct 2026 is a Saturday; Sat-11 = Tue 20 Oct.
		t.Errorf("PollDay = %v", w.PollDay())
	}
	if !w.SummaryDay().Equal(d(2026, 10, 28)) {
		t.Errorf("SummaryDay = %v", w.SummaryDay())
	}
}

func TestNextStep(t *testing.T) {
	w := LastWeekend(2026, time.October, wpg) // poll 20th, summary 28th, Sat 31st
	cases := []struct {
		name  string
		today time.Time
		phase Phase
		want  Step
	}{
		{"before window", d(2026, 10, 19), PhaseNew, StepNone},
		{"poll day", d(2026, 10, 20), PhaseNew, StepSendPoll},
		{"missed poll day, still before summary", d(2026, 10, 25), PhaseNew, StepSendPoll},
		{"already polled mid-window", d(2026, 10, 25), PhasePolled, StepNone},
		{"summary day", d(2026, 10, 28), PhasePolled, StepSummarize},
		{"missed summary day", d(2026, 10, 30), PhasePolled, StepSummarize},
		{"summary day but never polled", d(2026, 10, 28), PhaseNew, StepNone},
		{"after Saturday", d(2026, 11, 1), PhasePolled, StepNone},
		{"done", d(2026, 10, 29), PhaseDone, StepNone},
	}
	for _, c := range cases {
		if got := NextStep(c.today, w, c.phase); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestDateOnly_UsesLocation(t *testing.T) {
	// 14:00 UTC on 20 Oct 2026 is 09:00 CDT the same day in Winnipeg.
	utc := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	if got := DateOnly(utc, wpg); !got.Equal(d(2026, 10, 20)) {
		t.Errorf("got %v", got)
	}
	// 03:00 UTC on 21 Oct is still 22:00 on the 20th in Winnipeg.
	utc = time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	if got := DateOnly(utc, wpg); !got.Equal(d(2026, 10, 20)) {
		t.Errorf("got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/meetup/`
Expected: compile errors, package does not exist.

- [ ] **Step 3: Implement**

`internal/meetup/schedule.go`:

```go
// Package meetup implements the monthly last-weekend meetup poll: schedule
// decisions, poll text, AI idea generation, result summary, and the SSM
// state that ties the daily ticks together.
package meetup

import "time"

// Weekend is the last Fri/Sat/Sun of a month as midnight civil dates.
type Weekend struct {
	Fri, Sat, Sun time.Time
}

// LastWeekend returns the weekend anchored on the last Saturday of the
// month. Sun may fall in the following month.
func LastWeekend(year int, month time.Month, loc *time.Location) Weekend {
	// Day 0 of next month == last day of this month.
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, loc)
	back := (int(last.Weekday()) - int(time.Saturday) + 7) % 7
	sat := last.AddDate(0, 0, -back)
	return Weekend{Fri: sat.AddDate(0, 0, -1), Sat: sat, Sun: sat.AddDate(0, 0, 1)}
}

// Key identifies the month this weekend belongs to ("2006-01" of Saturday).
func (w Weekend) Key() string { return w.Sat.Format("2006-01") }

// PollDay is when the poll opens: 11 days before Saturday.
func (w Weekend) PollDay() time.Time { return w.Sat.AddDate(0, 0, -11) }

// SummaryDay is when the poll closes and the summary posts: 3 days before Saturday.
func (w Weekend) SummaryDay() time.Time { return w.Sat.AddDate(0, 0, -3) }

// Phase is where the month's flow currently stands.
type Phase string

const (
	PhaseNew    Phase = ""
	PhasePolled Phase = "polled"
	PhaseDone   Phase = "done"
)

// Step is the action a tick should take.
type Step int

const (
	StepNone Step = iota
	StepSendPoll
	StepSummarize
)

func (s Step) String() string {
	switch s {
	case StepSendPoll:
		return "send_poll"
	case StepSummarize:
		return "summarize"
	default:
		return "none"
	}
}

// NextStep decides what to do today. Window comparisons are inclusive on
// the start so a missed tick self-heals the next day.
func NextStep(today time.Time, w Weekend, phase Phase) Step {
	switch phase {
	case PhaseNew:
		if !today.Before(w.PollDay()) && today.Before(w.SummaryDay()) {
			return StepSendPoll
		}
	case PhasePolled:
		if !today.Before(w.SummaryDay()) && !today.After(w.Sat) {
			return StepSummarize
		}
	}
	return StepNone
}

// DateOnly converts an instant to midnight of its civil date in loc.
func DateOnly(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/meetup/ -run 'TestLastWeekend|TestWeekend|TestNextStep|TestDateOnly' -v`
Expected: PASS. If `TestLastWeekend` fails on the 2026 October case, verify with `cal 10 2026` that 31 Oct 2026 is a Saturday (it is).

- [ ] **Step 5: Commit**

```bash
git add internal/meetup/schedule.go internal/meetup/schedule_test.go
git commit -m "feat(meetup): last-weekend schedule and daily step decision"
```

---

### Task 5: `internal/meetup/poll.go` — question and options

**Files:**
- Create: `internal/meetup/poll.go`
- Test: `internal/meetup/poll_test.go`

**Interfaces:**
- Produces:
  - `const OptionCannot = "❌ Цього місяця не можу"`
  - `func Question(w Weekend) string`
  - `func Options(w Weekend, idea string) []string` — 5 options, or 4 when `idea == ""`; last is always `OptionCannot`
  - `func UkrDate(t time.Time) string` → `"24 жовтня"`
  - `func UkrRange(w Weekend) string` → `"24–26 жовтня"` or `"30 жовтня – 1 листопада"`
  - `const MaxOptionLen = 100`, `const MaxIdeaLen = 96`

- [ ] **Step 1: Write failing tests**

`internal/meetup/poll_test.go`:

```go
package meetup

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestUkrRange_SameMonth(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg) // 25–27 вересня
	if got := UkrRange(w); got != "25–27 вересня" {
		t.Errorf("got %q", got)
	}
}

func TestUkrRange_SpansMonths(t *testing.T) {
	w := LastWeekend(2026, time.October, wpg) // 30 жовтня – 1 листопада
	if got := UkrRange(w); got != "30 жовтня – 1 листопада" {
		t.Errorf("got %q", got)
	}
}

func TestQuestion(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Question(w)
	if got != "Зустріч в останні вихідні місяця (25–27 вересня)?" {
		t.Errorf("got %q", got)
	}
	if utf8.RuneCountInString(got) > 300 {
		t.Errorf("question too long: %d", utf8.RuneCountInString(got))
	}
}

func TestOptions_WithIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Options(w, "Субота після обіду — більярд на Осборн")
	want := []string{
		"🍻 Пʼятниця 25 вересня, ~18:30–19:00 — кава/пиво",
		"☕ Субота 26 вересня, ~12:30–13:00 — кава",
		"☕ Неділя 27 вересня, ~12:30–13:00 — кава",
		"🤖 Субота після обіду — більярд на Осборн",
		OptionCannot,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d options: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("option %d: got %q want %q", i, got[i], want[i])
		}
		if utf8.RuneCountInString(got[i]) > MaxOptionLen {
			t.Errorf("option %d too long", i)
		}
	}
}

func TestOptions_WithoutIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	got := Options(w, "")
	if len(got) != 4 || got[3] != OptionCannot {
		t.Errorf("got %q", got)
	}
}

func TestOptions_TruncatesLongIdea(t *testing.T) {
	w := LastWeekend(2026, time.September, wpg)
	long := strings.Repeat("я", 150)
	got := Options(w, long)
	if utf8.RuneCountInString(got[3]) > MaxOptionLen {
		t.Errorf("idea option not truncated: %d runes", utf8.RuneCountInString(got[3]))
	}
	if !strings.HasPrefix(got[3], "🤖 ") {
		t.Errorf("missing prefix: %q", got[3])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/meetup/ -run 'TestUkr|TestQuestion|TestOptions'`
Expected: compile errors.

- [ ] **Step 3: Implement**

`internal/meetup/poll.go`:

```go
package meetup

import (
	"fmt"
	"strings"
	"time"
)

const (
	// OptionCannot is always the last poll option and is excluded from the
	// winner computation.
	OptionCannot = "❌ Цього місяця не можу"
	// MaxOptionLen is Telegram's per-option character limit.
	MaxOptionLen = 100
	// MaxIdeaLen leaves room for the "🤖 " prefix inside MaxOptionLen.
	MaxIdeaLen = 96
	ideaPrefix = "🤖 "
)

// Genitive month names, as used after a day number ("24 жовтня").
var ukrMonths = [...]string{
	time.January:   "січня",
	time.February:  "лютого",
	time.March:     "березня",
	time.April:     "квітня",
	time.May:       "травня",
	time.June:      "червня",
	time.July:      "липня",
	time.August:    "серпня",
	time.September: "вересня",
	time.October:   "жовтня",
	time.November:  "листопада",
	time.December:  "грудня",
}

// UkrDate formats a date as "24 жовтня".
func UkrDate(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), ukrMonths[t.Month()])
}

// UkrRange formats Fri–Sun as "24–26 жовтня", or
// "30 жовтня – 1 листопада" when Sunday is in the next month.
func UkrRange(w Weekend) string {
	if w.Fri.Month() == w.Sun.Month() {
		return fmt.Sprintf("%d–%d %s", w.Fri.Day(), w.Sun.Day(), ukrMonths[w.Sun.Month()])
	}
	return UkrDate(w.Fri) + " – " + UkrDate(w.Sun)
}

// Question is the poll question naming the weekend's dates.
func Question(w Weekend) string {
	return fmt.Sprintf("Зустріч в останні вихідні місяця (%s)?", UkrRange(w))
}

// Options builds the poll options. idea == "" omits the AI option.
// The cannot-attend option is always last.
func Options(w Weekend, idea string) []string {
	opts := []string{
		fmt.Sprintf("🍻 Пʼятниця %s, ~18:30–19:00 — кава/пиво", UkrDate(w.Fri)),
		fmt.Sprintf("☕ Субота %s, ~12:30–13:00 — кава", UkrDate(w.Sat)),
		fmt.Sprintf("☕ Неділя %s, ~12:30–13:00 — кава", UkrDate(w.Sun)),
	}
	if idea = strings.TrimSpace(idea); idea != "" {
		opts = append(opts, ideaPrefix+truncateRunes(idea, MaxIdeaLen))
	}
	return append(opts, OptionCannot)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/meetup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/meetup/poll.go internal/meetup/poll_test.go
git commit -m "feat(meetup): poll question and options with Ukrainian dates"
```

---

### Task 6: `internal/meetup/summary.go` — winner and summary text

**Files:**
- Create: `internal/meetup/summary.go`
- Test: `internal/meetup/summary_test.go`

**Interfaces:**
- Produces:
  - `const MinVotes = 4`
  - `func PickWinner(counts []int) (idx int, ok bool)` — ignores the last index; `ok=false` when best count < MinVotes or fewer than 2 counts
  - `func FormatSummary(options []string, counts []int, winner int, ok bool) string` — MarkdownV2-escaped text

- [ ] **Step 1: Write failing tests**

`internal/meetup/summary_test.go`:

```go
package meetup

import (
	"strings"
	"testing"
)

func TestPickWinner_Basic(t *testing.T) {
	idx, ok := PickWinner([]int{2, 5, 1, 0, 3})
	if !ok || idx != 1 {
		t.Errorf("got idx=%d ok=%v", idx, ok)
	}
}

func TestPickWinner_IgnoresCannotOption(t *testing.T) {
	// Last option has the most votes but is "cannot attend".
	idx, ok := PickWinner([]int{4, 1, 0, 9})
	if !ok || idx != 0 {
		t.Errorf("got idx=%d ok=%v", idx, ok)
	}
}

func TestPickWinner_TieGoesToEarliest(t *testing.T) {
	idx, ok := PickWinner([]int{4, 4, 4, 0})
	if !ok || idx != 0 {
		t.Errorf("got idx=%d ok=%v", idx, ok)
	}
}

func TestPickWinner_BelowThreshold(t *testing.T) {
	if idx, ok := PickWinner([]int{3, 2, 1, 0}); ok {
		t.Errorf("expected no winner, got idx=%d", idx)
	}
	if _, ok := PickWinner([]int{0}); ok {
		t.Error("single option must not produce a winner")
	}
	if _, ok := PickWinner(nil); ok {
		t.Error("nil counts must not produce a winner")
	}
}

func TestFormatSummary_Winner(t *testing.T) {
	opts := []string{"🍻 Пʼятниця 25 вересня, ~18:30–19:00 — кава/пиво", "☕ Субота 26 вересня, ~12:30–13:00 — кава", OptionCannot}
	got := FormatSummary(opts, []int{2, 5, 1}, 1, true)
	if !strings.Contains(got, "📌 Зустріч цього місяця: ☕ Субота 26 вересня, \\~12:30–13:00 — кава") {
		t.Errorf("missing winner line:\n%s", got)
	}
	if !strings.Contains(got, "Голосів: 5") {
		t.Errorf("missing count:\n%s", got)
	}
	if !strings.Contains(got, "Усі варіанти:") || !strings.Contains(got, "кава/пиво — 2") {
		t.Errorf("missing tally:\n%s", got)
	}
	if strings.Contains(got, "не можу") == false {
		t.Errorf("tally should include the cannot option:\n%s", got)
	}
	if strings.Contains(got, "18:30-19:00") { // hyphen must be escaped as \- if ever present
		t.Errorf("unescaped hyphen:\n%s", got)
	}
}

func TestFormatSummary_Skip(t *testing.T) {
	opts := []string{"a", "b", OptionCannot}
	got := FormatSummary(opts, []int{2, 1, 6}, 0, false)
	if !strings.Contains(got, "😴 Цього місяця пропускаємо") || !strings.Contains(got, "максимум 2, потрібно 4") {
		t.Errorf("unexpected skip text:\n%s", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/meetup/ -run 'TestPickWinner|TestFormatSummary'`
Expected: compile errors.

- [ ] **Step 3: Implement**

`internal/meetup/summary.go`:

```go
package meetup

import (
	"fmt"
	"strings"

	"event_calendar/pkg/telegram"
)

// MinVotes is the smallest winning count that confirms a meetup.
const MinVotes = 4

// PickWinner returns the index of the option with the most votes, ignoring
// the last option (cannot attend). Ties go to the earliest option. ok is
// false when there is no eligible option or the best count is below MinVotes.
func PickWinner(counts []int) (int, bool) {
	if len(counts) < 2 {
		return 0, false
	}
	best := 0
	for i := 1; i < len(counts)-1; i++ {
		if counts[i] > counts[best] {
			best = i
		}
	}
	return best, counts[best] >= MinVotes
}

// FormatSummary renders the MarkdownV2 message announcing the decision.
func FormatSummary(options []string, counts []int, winner int, ok bool) string {
	esc := telegram.EscapeMarkdownV2
	var sb strings.Builder
	if ok && winner < len(options) {
		sb.WriteString("📌 *Зустріч цього місяця:* ")
		sb.WriteString(esc(options[winner]))
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("Голосів: %d", countAt(counts, winner))))
	} else {
		maxVotes := 0
		for i := 0; i < len(counts)-1; i++ {
			if counts[i] > maxVotes {
				maxVotes = counts[i]
			}
		}
		sb.WriteString(esc(fmt.Sprintf("😴 Цього місяця пропускаємо — замало голосів (максимум %d, потрібно %d).", maxVotes, MinVotes)))
	}
	sb.WriteString("\n\n")
	sb.WriteString(esc("Усі варіанти:"))
	for i, o := range options {
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%s — %d", o, countAt(counts, i))))
	}
	return sb.String()
}

func countAt(counts []int, i int) int {
	if i < len(counts) {
		return counts[i]
	}
	return 0
}
```

Note the winner line is bold: `*Зустріч цього місяця:*` is literal MarkdownV2 markup and must not be escaped; the test checks the escaped option text after it. Adjust the test's expected substring to `"📌 *Зустріч цього місяця:* ☕ Субота 26 вересня, \\~12:30–13:00 — кава"`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/meetup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/meetup/summary.go internal/meetup/summary_test.go
git commit -m "feat(meetup): winner selection with 4-vote threshold and summary text"
```

---

### Task 7: `internal/meetup/state.go` — state blob, SSM and file stores

**Files:**
- Create: `internal/meetup/state.go`
- Test: `internal/meetup/state_test.go`

**Interfaces:**
- Produces:
  - `type State struct { Month string; Phase Phase; PollMessageID int64; Options []string; PastIdeas []string }` with JSON tags `month`, `phase`, `poll_message_id,omitempty`, `options,omitempty`, `past_ideas,omitempty`
  - `func (s State) ForMonth(key string) State` — resets everything but `PastIdeas` when `s.Month != key`
  - `func (s *State) RememberIdea(idea string)` — appends, keeps last `MaxPastIdeas = 12`
  - `type Store interface { Get(ctx) (State, error); Put(ctx, State) error }`
  - `func NewSSMStore(api ssmparam.API, name string) *SSMStore`
  - `func NewFileStore(path string) *FileStore`

- [ ] **Step 1: Write failing tests**

`internal/meetup/state_test.go`:

```go
package meetup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"event_calendar/internal/ssmparam"
)

type fakeParam struct {
	value  string
	getErr error
	putErr error
}

func (f *fakeParam) GetParameter(ctx context.Context, name string) (string, error) {
	return f.value, f.getErr
}
func (f *fakeParam) PutParameter(ctx context.Context, name, value string) error {
	f.value = value
	return f.putErr
}

func TestSSMStore_RoundTrip(t *testing.T) {
	api := &fakeParam{}
	st := NewSSMStore(api, "/p")
	in := State{Month: "2026-10", Phase: PhasePolled, PollMessageID: 9, Options: []string{"a", "b"}, PastIdeas: []string{"x"}}
	if err := st.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	out, err := st.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Month != "2026-10" || out.Phase != PhasePolled || out.PollMessageID != 9 || len(out.Options) != 2 || out.PastIdeas[0] != "x" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestSSMStore_Get_NotFoundIsEmpty(t *testing.T) {
	st := NewSSMStore(&fakeParam{getErr: ssmparam.ErrNotFound}, "/p")
	out, err := st.Get(context.Background())
	if err != nil || out.Month != "" || out.Phase != PhaseNew {
		t.Errorf("got %+v err=%v", out, err)
	}
}

func TestSSMStore_Get_EmptyValueIsEmpty(t *testing.T) {
	st := NewSSMStore(&fakeParam{value: ""}, "/p")
	if out, err := st.Get(context.Background()); err != nil || out.Month != "" {
		t.Errorf("got %+v err=%v", out, err)
	}
}

func TestSSMStore_Get_PropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	st := NewSSMStore(&fakeParam{getErr: boom}, "/p")
	if _, err := st.Get(context.Background()); !errors.Is(err, boom) {
		t.Errorf("got %v", err)
	}
}

func TestFileStore_RoundTripAndMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := NewFileStore(path)
	if out, err := st.Get(context.Background()); err != nil || out.Month != "" {
		t.Fatalf("missing file should be empty: %+v %v", out, err)
	}
	if err := st.Put(context.Background(), State{Month: "2026-10"}); err != nil {
		t.Fatal(err)
	}
	if out, err := st.Get(context.Background()); err != nil || out.Month != "2026-10" {
		t.Errorf("got %+v err=%v", out, err)
	}
}

func TestState_ForMonth(t *testing.T) {
	s := State{Month: "2026-09", Phase: PhaseDone, PollMessageID: 1, Options: []string{"a"}, PastIdeas: []string{"i1"}}
	same := s.ForMonth("2026-09")
	if same.Phase != PhaseDone || same.PollMessageID != 1 {
		t.Errorf("same month must be untouched: %+v", same)
	}
	next := s.ForMonth("2026-10")
	if next.Month != "2026-10" || next.Phase != PhaseNew || next.PollMessageID != 0 || next.Options != nil {
		t.Errorf("new month must reset: %+v", next)
	}
	if len(next.PastIdeas) != 1 || next.PastIdeas[0] != "i1" {
		t.Errorf("past ideas must carry over: %+v", next.PastIdeas)
	}
}

func TestState_RememberIdea_Caps(t *testing.T) {
	var s State
	for i := 0; i < 15; i++ {
		s.RememberIdea(string(rune('a' + i)))
	}
	if len(s.PastIdeas) != MaxPastIdeas {
		t.Fatalf("len = %d", len(s.PastIdeas))
	}
	if s.PastIdeas[0] != "d" || s.PastIdeas[MaxPastIdeas-1] != "o" {
		t.Errorf("should keep the newest: %v", s.PastIdeas)
	}
	s.RememberIdea("   ")
	if len(s.PastIdeas) != MaxPastIdeas {
		t.Error("blank idea must be ignored")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/meetup/ -run 'TestSSMStore|TestFileStore|TestState'`
Expected: compile errors.

- [ ] **Step 3: Implement**

`internal/meetup/state.go`:

```go
package meetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"event_calendar/internal/ssmparam"
)

// MaxPastIdeas bounds the list handed to the idea generator to avoid repeats.
const MaxPastIdeas = 12

// State is the per-month flow state persisted between daily ticks.
type State struct {
	Month         string   `json:"month"`
	Phase         Phase    `json:"phase"`
	PollMessageID int64    `json:"poll_message_id,omitempty"`
	Options       []string `json:"options,omitempty"`
	PastIdeas     []string `json:"past_ideas,omitempty"`
}

// ForMonth returns the state to use for month key. A different month
// resets the flow but keeps PastIdeas.
func (s State) ForMonth(key string) State {
	if s.Month == key {
		return s
	}
	return State{Month: key, PastIdeas: s.PastIdeas}
}

// RememberIdea appends idea, keeping only the newest MaxPastIdeas.
func (s *State) RememberIdea(idea string) {
	idea = strings.TrimSpace(idea)
	if idea == "" {
		return
	}
	s.PastIdeas = append(s.PastIdeas, idea)
	if n := len(s.PastIdeas); n > MaxPastIdeas {
		s.PastIdeas = s.PastIdeas[n-MaxPastIdeas:]
	}
}

// Store persists State between Lambda invocations.
type Store interface {
	Get(ctx context.Context) (State, error)
	Put(ctx context.Context, s State) error
}

// SSMStore keeps the state as JSON in one SSM parameter.
type SSMStore struct {
	api  ssmparam.API
	name string
}

// NewSSMStore returns a store over the named parameter.
func NewSSMStore(api ssmparam.API, name string) *SSMStore {
	return &SSMStore{api: api, name: name}
}

// Get returns the stored state; a missing or empty parameter is a zero State.
func (s *SSMStore) Get(ctx context.Context) (State, error) {
	raw, err := s.api.GetParameter(ctx, s.name)
	if err != nil {
		if errors.Is(err, ssmparam.ErrNotFound) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("ssm get %s: %w", s.name, err)
	}
	return decodeState([]byte(raw), s.name)
}

// Put overwrites the parameter with the JSON-encoded state.
func (s *SSMStore) Put(ctx context.Context, st State) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := s.api.PutParameter(ctx, s.name, string(raw)); err != nil {
		return fmt.Errorf("ssm put %s: %w", s.name, err)
	}
	return nil
}

// FileStore keeps the state in a local JSON file (CLI use).
type FileStore struct{ path string }

// NewFileStore returns a store over path; a missing file is a zero State.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

func (f *FileStore) Get(ctx context.Context) (State, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}
	return decodeState(b, f.path)
}

func (f *FileStore) Put(ctx context.Context, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, b, 0o644)
}

func decodeState(raw []byte, src string) (State, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return State{}, nil
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("parse state %s: %w", src, err)
	}
	return st, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/meetup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/meetup/state.go internal/meetup/state_test.go
git commit -m "feat(meetup): month state with SSM and file stores"
```

---

### Task 8: `internal/meetup/idea.go` — Claude Haiku idea generator

**Files:**
- Modify: `go.mod` / `go.sum` (add `github.com/anthropics/anthropic-sdk-go v1.71.0`)
- Create: `internal/meetup/idea.go`
- Test: `internal/meetup/idea_test.go`

**Interfaces:**
- Produces:
  - `type IdeaInput struct { Weekend Weekend; PastIdeas []string; Conditions string }`
  - `type IdeaGenerator interface { Generate(ctx context.Context, in IdeaInput) (string, error) }`
  - `func NewClaudeIdeas(apiKey string, opts ...option.RequestOption) *ClaudeIdeas`
  - `const IdeaModel = "claude-haiku-4-5"`
  - `func Season(m time.Month) string` → `"зима" | "весна" | "літо" | "осінь"`
  - `func cleanIdea(raw string) string` — first non-empty line, trimmed, stripped of surrounding quotes and a leading `🤖 `, capped at `MaxIdeaLen` runes

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/anthropics/anthropic-sdk-go@v1.71.0 && go mod tidy`
Expected: `go.mod` gains the requirement.

- [ ] **Step 2: Write failing tests**

`internal/meetup/idea_test.go`:

```go
package meetup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestSeason(t *testing.T) {
	cases := map[time.Month]string{
		time.December: "зима", time.January: "зима", time.February: "зима",
		time.March: "весна", time.May: "весна",
		time.June: "літо", time.August: "літо",
		time.September: "осінь", time.November: "осінь",
	}
	for m, want := range cases {
		if got := Season(m); got != want {
			t.Errorf("%v: got %q want %q", m, got, want)
		}
	}
}

func TestCleanIdea(t *testing.T) {
	cases := map[string]string{
		"Субота після обіду — більярд":               "Субота після обіду — більярд",
		"\n\n  \"Неділя 13:00 — боулінг\"  \n":        "Неділя 13:00 — боулінг",
		"🤖 Пʼятниця 19:00 — квіз\nдруга лінія":      "Пʼятниця 19:00 — квіз",
		strings.Repeat("я", 120):                    strings.Repeat("я", MaxIdeaLen-1) + "…",
		"   ":                                       "",
	}
	for in, want := range cases {
		if got := cleanIdea(in); got != want {
			t.Errorf("cleanIdea(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeAnthropic returns an httptest server that mimics POST /v1/messages.
func fakeAnthropic(t *testing.T, text string, status int) (*httptest.Server, *map[string]any) {
	t.Helper()
	var lastReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &lastReq)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": IdeaModel,
			"content":     []map[string]any{{"type": "text", "text": text}},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &lastReq
}

func TestClaudeIdeas_Generate(t *testing.T) {
	srv, req := fakeAnthropic(t, "\"Субота 14:00 — лижі у Windsor Park\"\n", http.StatusOK)
	g := NewClaudeIdeas("k", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	w := LastWeekend(2027, time.January, wpg)
	got, err := g.Generate(context.Background(), IdeaInput{Weekend: w, PastIdeas: []string{"боулінг"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Субота 14:00 — лижі у Windsor Park" {
		t.Errorf("got %q", got)
	}
	if (*req)["model"] != IdeaModel {
		t.Errorf("model = %v", (*req)["model"])
	}
	body, _ := json.Marshal(*req)
	for _, want := range []string{"29 січня", "30 січня", "31 січня", "зима", "боулінг", "Winnipeg"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request missing %q:\n%s", want, body)
		}
	}
	if (*req)["thinking"] != nil {
		t.Error("thinking must not be set for haiku")
	}
}

func TestClaudeIdeas_EmptyIsError(t *testing.T) {
	srv, _ := fakeAnthropic(t, "   ", http.StatusOK)
	g := NewClaudeIdeas("k", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if _, err := g.Generate(context.Background(), IdeaInput{Weekend: LastWeekend(2026, time.July, wpg)}); err == nil {
		t.Error("expected error for empty idea")
	}
}

func TestClaudeIdeas_APIError(t *testing.T) {
	srv, _ := fakeAnthropic(t, "", http.StatusInternalServerError)
	g := NewClaudeIdeas("k", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if _, err := g.Generate(context.Background(), IdeaInput{Weekend: LastWeekend(2026, time.July, wpg)}); err == nil {
		t.Error("expected API error")
	}
}

func TestClaudeIdeas_NoKey(t *testing.T) {
	g := NewClaudeIdeas("")
	if _, err := g.Generate(context.Background(), IdeaInput{Weekend: LastWeekend(2026, time.July, wpg)}); err == nil {
		t.Error("expected error when key is empty")
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/meetup/ -run 'TestSeason|TestCleanIdea|TestClaudeIdeas'`
Expected: compile errors.

- [ ] **Step 4: Implement**

`internal/meetup/idea.go`:

```go
package meetup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// IdeaModel is the model used for the AI option. Haiku is plenty for one
// short line and keeps the monthly cost negligible.
const IdeaModel = "claude-haiku-4-5"

// IdeaInput is everything the generator may consider.
type IdeaInput struct {
	Weekend   Weekend
	PastIdeas []string
	// Conditions is an optional free-text hint (e.g. a forecast). Empty today;
	// reserved so a forecast can be added without changing the prompt shape.
	Conditions string
}

// IdeaGenerator produces the "🤖" poll option text (without the prefix).
type IdeaGenerator interface {
	Generate(ctx context.Context, in IdeaInput) (string, error)
}

// ClaudeIdeas generates ideas with the Anthropic API.
type ClaudeIdeas struct {
	apiKey string
	client anthropic.Client
}

// NewClaudeIdeas builds a generator. Extra options (base URL, retries) are
// for tests.
func NewClaudeIdeas(apiKey string, opts ...option.RequestOption) *ClaudeIdeas {
	all := append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(20 * time.Second),
	}, opts...)
	return &ClaudeIdeas{apiKey: apiKey, client: anthropic.NewClient(all...)}
}

const ideaSystemPrompt = `Ти допомагаєш невеликій українськомовній tech-спільноті у Вінніпезі (Winnipeg, Manitoba, Canada) обрати ідею для зустрічі на вихідних.

Правила відповіді:
- Рівно один рядок, українською, без пояснень, без лапок, без емодзі.
- Формат: "<день і приблизний час> — <конкретна активність і місце>". Наприклад: "Субота 14:00 — більярд на Osborne".
- Не більше 70 символів.
- Обери один із трьох запропонованих днів і вкажи приблизний час початку.
- Активність має бути реальною і доступною у Вінніпезі або поруч: спорт, прогулянки, ігри, виставки, кафе з настільними іграми, музеї, tech-події тощо.
- Враховуй пору року і погодні умови, щоб ідея була доречною, але НІКОЛИ не згадуй пору року, місяць чи погоду в тексті.
- Не повторюй ідеї зі списку минулих ідей.`

// Generate asks Claude for one idea line. Empty output is an error so the
// caller can drop the option.
func (c *ClaudeIdeas) Generate(ctx context.Context, in IdeaInput) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	w := in.Weekend
	var user strings.Builder
	fmt.Fprintf(&user, "Дати зустрічі: пʼятниця %s, субота %s, неділя %s (%d рік).\n",
		UkrDate(w.Fri), UkrDate(w.Sat), UkrDate(w.Sun), w.Sat.Year())
	fmt.Fprintf(&user, "Пора року: %s.\n", Season(w.Sat.Month()))
	if in.Conditions != "" {
		fmt.Fprintf(&user, "Умови: %s\n", in.Conditions)
	}
	if len(in.PastIdeas) > 0 {
		user.WriteString("Минулі ідеї (не повторювати):\n")
		for _, p := range in.PastIdeas {
			fmt.Fprintf(&user, "- %s\n", p)
		}
	}
	user.WriteString("Запропонуй одну ідею.")

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     IdeaModel,
		MaxTokens: 200,
		System:    []anthropic.TextBlockParam{{Text: ideaSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user.String())),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	idea := cleanIdea(text.String())
	if idea == "" {
		return "", fmt.Errorf("anthropic: empty idea")
	}
	return idea, nil
}

// Season returns the Ukrainian season word for a month.
func Season(m time.Month) string {
	switch m {
	case time.December, time.January, time.February:
		return "зима"
	case time.March, time.April, time.May:
		return "весна"
	case time.June, time.July, time.August:
		return "літо"
	default:
		return "осінь"
	}
}

// cleanIdea keeps the first non-empty line, strips quotes and a stray
// robot prefix, and caps the length for the Telegram option limit.
func cleanIdea(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, `"'«»“”`)
		line = strings.TrimSpace(strings.TrimPrefix(line, ideaPrefix))
		if line != "" {
			return truncateRunes(line, MaxIdeaLen)
		}
	}
	return ""
}
```

If the compiler reports that `anthropic.NewClient` returns a value rather than a pointer (or vice versa), adjust the `client` field type to whatever `NewClient` returns; do not change the call pattern `c.client.Messages.New`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/meetup/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/meetup/idea.go internal/meetup/idea_test.go
git commit -m "feat(meetup): Claude Haiku idea generator for the AI poll option"
```

---

### Task 9: `internal/meetup/run.go` — the orchestrator

**Files:**
- Create: `internal/meetup/run.go`
- Test: `internal/meetup/run_test.go`

**Interfaces:**
- Produces:
  - `type Bot interface { SendPoll(chatID, question string, options []string, allowMultiple bool) (int64, error); StopPoll(chatID string, messageID int64) ([]int, error); SendMessageID(chatID, text string) (int64, error); PinChatMessage(chatID string, messageID int64) error }` — satisfied by `*telegram.Service`
  - `type Deps struct { Bot Bot; Store Store; Ideas IdeaGenerator; ChatID string; TestMode bool; Loc *time.Location }`
  - `type Result struct { Success bool; Step string; Month string; PollMessageID int64; Winner string; Error string }` (JSON tags snake_case, `omitempty` on the optional ones)
  - `func Run(ctx context.Context, deps Deps, now time.Time) Result`

- [ ] **Step 1: Write failing tests**

`internal/meetup/run_test.go`:

```go
package meetup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeBot struct {
	pollQ     string
	pollOpts  []string
	pollErr   error
	stopped   int64
	counts    []int
	stopErr   error
	sentText  string
	sendErr   error
	pinned    int64
	pinErr    error
	calls     []string
}

func (b *fakeBot) SendPoll(chatID, q string, opts []string, multi bool) (int64, error) {
	b.calls = append(b.calls, "sendPoll")
	b.pollQ, b.pollOpts = q, opts
	if !multi {
		return 0, errors.New("poll must allow multiple answers")
	}
	return 111, b.pollErr
}
func (b *fakeBot) StopPoll(chatID string, id int64) ([]int, error) {
	b.calls = append(b.calls, "stopPoll")
	b.stopped = id
	return b.counts, b.stopErr
}
func (b *fakeBot) SendMessageID(chatID, text string) (int64, error) {
	b.calls = append(b.calls, "sendMessage")
	b.sentText = text
	return 222, b.sendErr
}
func (b *fakeBot) PinChatMessage(chatID string, id int64) error {
	b.calls = append(b.calls, "pin")
	b.pinned = id
	return b.pinErr
}

type memStore struct {
	st     State
	getErr error
	putErr error
	puts   int
}

func (m *memStore) Get(ctx context.Context) (State, error) { return m.st, m.getErr }
func (m *memStore) Put(ctx context.Context, s State) error {
	m.puts++
	m.st = s
	return m.putErr
}

type fakeIdeas struct {
	idea string
	err  error
	in   IdeaInput
}

func (f *fakeIdeas) Generate(ctx context.Context, in IdeaInput) (string, error) {
	f.in = in
	return f.idea, f.err
}

func deps(b *fakeBot, s *memStore, i *fakeIdeas) Deps {
	return Deps{Bot: b, Store: s, Ideas: i, ChatID: "-100", Loc: wpg}
}

// 14:00 UTC on the given Winnipeg date.
func tick(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 14, 0, 0, 0, time.UTC)
}

func TestRun_Noop_BeforeWindow(t *testing.T) {
	b, s := &fakeBot{}, &memStore{}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 5))
	if !res.Success || res.Step != "none" || len(b.calls) != 0 || s.puts != 0 {
		t.Errorf("res=%+v calls=%v puts=%d", res, b.calls, s.puts)
	}
}

func TestRun_SendsPollWithIdea(t *testing.T) {
	b, s, i := &fakeBot{}, &memStore{st: State{Month: "2026-09", Phase: PhaseDone, PastIdeas: []string{"old"}}}, &fakeIdeas{idea: "Субота 14:00 — більярд"}
	res := Run(context.Background(), deps(b, s, i), tick(2026, 10, 20))
	if !res.Success || res.Step != "send_poll" || res.PollMessageID != 111 {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(b.pollQ, "30 жовтня – 1 листопада") {
		t.Errorf("question = %q", b.pollQ)
	}
	if len(b.pollOpts) != 5 || b.pollOpts[3] != "🤖 Субота 14:00 — більярд" {
		t.Errorf("options = %q", b.pollOpts)
	}
	if s.st.Month != "2026-10" || s.st.Phase != PhasePolled || s.st.PollMessageID != 111 || len(s.st.Options) != 5 {
		t.Errorf("state = %+v", s.st)
	}
	if len(s.st.PastIdeas) != 2 || s.st.PastIdeas[1] != "Субота 14:00 — більярд" {
		t.Errorf("past ideas = %v", s.st.PastIdeas)
	}
	if len(i.in.PastIdeas) != 1 || i.in.PastIdeas[0] != "old" {
		t.Errorf("generator should see previous ideas: %+v", i.in)
	}
}

func TestRun_SendsPollWithoutIdeaOnGeneratorError(t *testing.T) {
	b, s := &fakeBot{}, &memStore{}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{err: errors.New("down")}), tick(2026, 10, 20))
	if !res.Success || len(b.pollOpts) != 4 || b.pollOpts[3] != OptionCannot {
		t.Errorf("res=%+v opts=%q", res, b.pollOpts)
	}
}

func TestRun_TestMode_SendsNothingWritesNothing(t *testing.T) {
	b, s := &fakeBot{}, &memStore{}
	d := deps(b, s, &fakeIdeas{idea: "x"})
	d.TestMode = true
	res := Run(context.Background(), d, tick(2026, 10, 20))
	if !res.Success || res.Step != "send_poll" || len(b.calls) != 0 || s.puts != 0 {
		t.Errorf("res=%+v calls=%v puts=%d", res, b.calls, s.puts)
	}
}

func TestRun_PollSendErrorLeavesStateUntouched(t *testing.T) {
	b, s := &fakeBot{pollErr: errors.New("tg down")}, &memStore{}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 20))
	if res.Success || s.puts != 0 {
		t.Errorf("res=%+v puts=%d", res, s.puts)
	}
}

func polledState() State {
	return State{Month: "2026-10", Phase: PhasePolled, PollMessageID: 111,
		Options: []string{"🍻 пт", "☕ сб", "☕ нд", "🤖 ідея", OptionCannot}}
}

func TestRun_SummarizesAndPins(t *testing.T) {
	b, s := &fakeBot{counts: []int{1, 5, 2, 0, 3}}, &memStore{st: polledState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if !res.Success || res.Step != "summarize" || res.Winner != "☕ сб" {
		t.Fatalf("res=%+v", res)
	}
	if b.stopped != 111 || b.pinned != 222 {
		t.Errorf("stopped=%d pinned=%d", b.stopped, b.pinned)
	}
	if !strings.Contains(b.sentText, "Голосів: 5") {
		t.Errorf("summary = %q", b.sentText)
	}
	if s.st.Phase != PhaseDone {
		t.Errorf("state = %+v", s.st)
	}
}

func TestRun_SummarySkipsWhenBelowThreshold(t *testing.T) {
	b, s := &fakeBot{counts: []int{1, 3, 2, 0, 3}}, &memStore{st: polledState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if !res.Success || res.Winner != "" || b.pinned != 0 {
		t.Errorf("res=%+v pinned=%d", res, b.pinned)
	}
	if !strings.Contains(b.sentText, "пропускаємо") {
		t.Errorf("summary = %q", b.sentText)
	}
	if s.st.Phase != PhaseDone {
		t.Errorf("state = %+v", s.st)
	}
}

func TestRun_PinFailureIsNotFatal(t *testing.T) {
	b, s := &fakeBot{counts: []int{0, 4, 0, 0, 0}, pinErr: errors.New("not admin")}, &memStore{st: polledState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if !res.Success || s.st.Phase != PhaseDone {
		t.Errorf("res=%+v state=%+v", res, s.st)
	}
}

func TestRun_StopPollErrorKeepsPolledPhase(t *testing.T) {
	b, s := &fakeBot{stopErr: errors.New("tg down")}, &memStore{st: polledState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if res.Success || s.puts != 0 || s.st.Phase != PhasePolled {
		t.Errorf("res=%+v state=%+v", res, s.st)
	}
}

func TestRun_StoreErrorFails(t *testing.T) {
	res := Run(context.Background(), deps(&fakeBot{}, &memStore{getErr: errors.New("ssm")}, &fakeIdeas{}), tick(2026, 10, 20))
	if res.Success {
		t.Errorf("res=%+v", res)
	}
}

func TestRun_MissingConfigFails(t *testing.T) {
	d := deps(&fakeBot{}, &memStore{}, &fakeIdeas{})
	d.ChatID = ""
	if res := Run(context.Background(), d, tick(2026, 10, 20)); res.Success {
		t.Errorf("res=%+v", res)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/meetup/ -run TestRun`
Expected: compile errors.

- [ ] **Step 3: Implement**

`internal/meetup/run.go`:

```go
package meetup

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Bot is the slice of the Telegram client the flow needs.
// *telegram.Service satisfies it.
type Bot interface {
	SendPoll(chatID, question string, options []string, allowMultiple bool) (int64, error)
	StopPoll(chatID string, messageID int64) ([]int, error)
	SendMessageID(chatID, text string) (int64, error)
	PinChatMessage(chatID string, messageID int64) error
}

// Deps are the collaborators for Run.
type Deps struct {
	Bot      Bot
	Store    Store
	Ideas    IdeaGenerator
	ChatID   string
	TestMode bool
	Loc      *time.Location
}

// Result is what one tick did.
type Result struct {
	Success       bool   `json:"success"`
	Step          string `json:"step"`
	Month         string `json:"month,omitempty"`
	PollMessageID int64  `json:"poll_message_id,omitempty"`
	Winner        string `json:"winner,omitempty"`
	Error         string `json:"error,omitempty"`
}

func fail(step, format string, args ...any) Result {
	return Result{Step: step, Error: fmt.Sprintf(format, args...)}
}

// Run executes one daily tick for the instant now.
func Run(ctx context.Context, deps Deps, now time.Time) Result {
	if deps.ChatID == "" {
		return fail("none", "TELEGRAM_MEETUP_CHAT_ID not set")
	}
	if deps.Bot == nil || deps.Store == nil || deps.Loc == nil {
		return fail("none", "meetup: missing dependencies")
	}

	today := DateOnly(now, deps.Loc)
	w := LastWeekend(today.Year(), today.Month(), deps.Loc)
	stored, err := deps.Store.Get(ctx)
	if err != nil {
		return fail("none", "state read: %v", err)
	}
	st := stored.ForMonth(w.Key())
	step := NextStep(today, w, st.Phase)
	log.Printf("meetup: today=%s weekend=%s..%s phase=%q step=%s",
		today.Format("2006-01-02"), w.Fri.Format("01-02"), w.Sun.Format("01-02"), st.Phase, step)

	switch step {
	case StepSendPoll:
		return sendPoll(ctx, deps, w, st)
	case StepSummarize:
		return summarize(ctx, deps, st)
	default:
		return Result{Success: true, Step: step.String(), Month: w.Key()}
	}
}

func sendPoll(ctx context.Context, deps Deps, w Weekend, st State) Result {
	idea := ""
	if deps.Ideas != nil {
		got, err := deps.Ideas.Generate(ctx, IdeaInput{Weekend: w, PastIdeas: st.PastIdeas})
		if err != nil {
			log.Printf("meetup: idea generation failed, sending poll without AI option: %v", err)
		} else {
			idea = got
		}
	}
	question := Question(w)
	options := Options(w, idea)

	if deps.TestMode {
		log.Printf("test mode: would send poll %q with options %q to %s", question, options, deps.ChatID)
		return Result{Success: true, Step: StepSendPoll.String(), Month: w.Key()}
	}

	id, err := deps.Bot.SendPoll(deps.ChatID, question, options, true)
	if err != nil {
		return fail(StepSendPoll.String(), "send poll: %v", err)
	}
	st.Phase = PhasePolled
	st.PollMessageID = id
	st.Options = options
	st.RememberIdea(idea)
	if err := deps.Store.Put(ctx, st); err != nil {
		// The poll is out but we lost track of it; the next tick would send
		// another. Surface loudly.
		return fail(StepSendPoll.String(), "poll %d sent but state write failed: %v", id, err)
	}
	return Result{Success: true, Step: StepSendPoll.String(), Month: w.Key(), PollMessageID: id}
}

func summarize(ctx context.Context, deps Deps, st State) Result {
	if deps.TestMode {
		log.Printf("test mode: would stop poll %d and post summary to %s", st.PollMessageID, deps.ChatID)
		return Result{Success: true, Step: StepSummarize.String(), Month: st.Month, PollMessageID: st.PollMessageID}
	}

	counts, err := deps.Bot.StopPoll(deps.ChatID, st.PollMessageID)
	if err != nil {
		return fail(StepSummarize.String(), "stop poll %d: %v", st.PollMessageID, err)
	}
	winner, ok := PickWinner(counts)
	text := FormatSummary(st.Options, counts, winner, ok)

	msgID, err := deps.Bot.SendMessageID(deps.ChatID, text)
	if err != nil {
		return fail(StepSummarize.String(), "send summary: %v", err)
	}
	res := Result{Success: true, Step: StepSummarize.String(), Month: st.Month, PollMessageID: st.PollMessageID}
	if ok && winner < len(st.Options) {
		res.Winner = st.Options[winner]
		if err := deps.Bot.PinChatMessage(deps.ChatID, msgID); err != nil {
			log.Printf("meetup: pin failed (is the bot an admin?): %v", err)
		}
	}
	st.Phase = PhaseDone
	if err := deps.Store.Put(ctx, st); err != nil {
		return fail(StepSummarize.String(), "summary posted but state write failed: %v", err)
	}
	return res
}
```

- [ ] **Step 4: Run tests and vet**

Run: `go vet ./internal/meetup/ && go test ./internal/meetup/ -v -run TestRun`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/meetup/run.go internal/meetup/run_test.go
git commit -m "feat(meetup): daily tick orchestrator (poll, summarize, noop)"
```

---

### Task 10: `cmd/meetup` Lambda and CLI action

**Files:**
- Create: `cmd/meetup/main.go`
- Modify: `cmd/cli/main.go`
- Modify: `.gitignore` (add `/meetup` next to `/cli`, `/server`, `/lambda`)

**Interfaces:**
- Consumes `meetup.Run`, `meetup.Deps`, `meetup.NewSSMStore`, `meetup.NewFileStore`, `meetup.NewClaudeIdeas`, `ssmparam.NewAWSAdapter`, `telegram.NewService`, `config.Load`.
- Lambda event: `{"date":"YYYY-MM-DD"}` optional; empty object is the normal EventBridge payload.

- [ ] **Step 1: Write the Lambda**

`cmd/meetup/main.go`:

```go
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

func handle(ctx context.Context, evt Event) (meetup.Result, error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v", r)
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

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return meetup.Result{Error: fmt.Sprintf("aws config: %v", err)}, nil
	}
	store := meetup.NewSSMStore(ssmparam.NewAWSAdapter(ssm.NewFromConfig(awsCfg)), cfg.MeetupSSMParam)

	return meetup.Run(ctx, meetup.Deps{
		Bot:      telegram.NewService(cfg.MeetupBotToken),
		Store:    store,
		Ideas:    meetup.NewClaudeIdeas(cfg.AnthropicAPIKey),
		ChatID:   cfg.MeetupChatID,
		TestMode: cfg.TestMode,
		Loc:      loc,
	}, now), nil
}

// resolveNow returns time.Now, or 14:00 UTC-equivalent (09:00 local) on the
// overridden date so the tick lands on the same civil day it would in prod.
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
```

- [ ] **Step 2: Add the CLI action**

In `cmd/cli/main.go`:

- Add `_ "time/tzdata"` to imports plus `"event_calendar/internal/meetup"` and `"event_calendar/internal/ssmparam"`.
- Add a flag: `date := flag.String("date", "", "meetup only: simulate this day (YYYY-MM-DD)")`.
- Change the `-out` help to `"scrape_annual/meetup: write state JSON to this file instead of SSM"`.
- Add `case "meetup": result = toJobsResult(runMeetup(ctx, cfg, *date, *out))` — no: keep `result` typed loosely. Simplest: change `main` to print whatever the action returns:

```go
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
```

Add:

```go
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
		ChatID:   cfg.MeetupChatID,
		TestMode: cfg.TestMode,
		Loc:      loc,
	}, now)
}
```

Update the file's usage comment:

```
//	go run ./cmd/cli -action=events
//	go run ./cmd/cli -action=scrape_annual                    # writes to SSM via default AWS creds
//	go run ./cmd/cli -action=scrape_annual -out=/tmp/x.json   # writes JSON to file, skips SSM
//	go run ./cmd/cli -action=meetup                           # today's tick, SSM state
//	go run ./cmd/cli -action=meetup -date=2026-10-20 -out=/tmp/meetup.json
```

Also change `runAnnualScrape`'s SSM wiring to `ssmparam.NewAWSAdapter(...)` so the `annual.NewAWSSSMAdapter` shim from Task 1 has no callers left; then delete the shim from `internal/annual/store.go` and update `cmd/lambda/main.go` the same way.

- [ ] **Step 3: Build, vet, test, smoke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS.

Run the smoke test in test mode against a file store:

```bash
TEST_MODE=true TELEGRAM_MEETUP_BOT_TOKEN=x TELEGRAM_MEETUP_CHAT_ID=-1 \
  go run ./cmd/cli -action=meetup -date=2026-10-20 -out=/tmp/meetup-state.json
```

Expected log: `step=send_poll`, "test mode: would send poll …", result JSON with `"success": true, "step": "send_poll"`, and no `/tmp/meetup-state.json` written. (The idea generator logs a failure because the API key is empty; that is expected.)

Then `go run ./cmd/cli -action=meetup -date=2026-10-05 -out=/tmp/meetup-state.json` → `"step": "none"`.

Add `/meetup` to `.gitignore` under the stray-binaries block.

- [ ] **Step 4: Commit**

```bash
git add cmd/meetup cmd/cli cmd/lambda internal/annual .gitignore
git commit -m "feat(meetup): Lambda entry point and CLI action with date override"
```

---

### Task 11: Deploy scripts, CI workflow, docs

**Files:**
- Modify: `deploy/deploy.sh`
- Modify: `deploy/bootstrap.sh`
- Modify: `.github/workflows/deploy.yml`
- Modify: `README.md`, `CHANGELOG.md`, `config.example.env`

- [ ] **Step 1: deploy.sh — two functions, delete the old poll rule**

Edit `deploy/deploy.sh`:

Header env list: remove the two `TELEGRAM_POLL_*` lines; add

```
#   TELEGRAM_MEETUP_BOT_TOKEN  (meetup bot)
#   TELEGRAM_MEETUP_CHAT_ID    (meetup bot)
#   ANTHROPIC_API_KEY          (meetup bot)
#   MEETUP_SSM_PARAM=/winnipeg-tech-events/meetup-state (default; set by this script)
```

Variables: replace `RULE_POLL="winnipeg-poll-monthly"` with

```bash
MEETUP_FUNCTION_NAME="winnipeg-meetup-bot"
RULE_MEETUP="winnipeg-meetup-daily"
RULE_POLL_LEGACY="winnipeg-poll-monthly"
MEETUP_SSM_PARAM="/winnipeg-tech-events/meetup-state"
```

Build block: build both binaries.

```bash
echo "== Building Go Lambdas =="
build_zip() {
    local pkg="$1" zip="$2"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -tags lambda.norpc -o bootstrap "$pkg"
    zip -j "$zip" bootstrap >/dev/null
    rm bootstrap
    echo "  -> $zip"
}
build_zip ./cmd/lambda deploy/lambda.zip
build_zip ./cmd/meetup deploy/meetup.zip
```

Turn the create/update + env blocks into a function used twice. Replace the "Deploying Lambda function" and "Setting Lambda environment variables" sections with:

```bash
deploy_function() {
    local name="$1" zip="$2" env_vars="$3"
    echo "== Deploying Lambda function ${name} =="
    if aws lambda get-function --function-name "$name" --region "$REGION" >/dev/null 2>&1; then
        aws lambda update-function-code \
            --function-name "$name" --zip-file "fileb://${zip}" --region "$REGION" >/dev/null
        aws lambda wait function-updated --function-name "$name" --region "$REGION"
        aws lambda update-function-configuration \
            --function-name "$name" --runtime "$RUNTIME" --handler "$HANDLER" \
            --timeout "$TIMEOUT" --memory-size "$MEMORY" --region "$REGION" >/dev/null
        echo "  -> updated"
    else
        aws lambda create-function \
            --function-name "$name" --runtime "$RUNTIME" --role "$ROLE_ARN" \
            --handler "$HANDLER" --zip-file "fileb://${zip}" \
            --timeout "$TIMEOUT" --memory-size "$MEMORY" --region "$REGION" >/dev/null
        echo "  -> created"
    fi
    aws lambda wait function-updated --function-name "$name" --region "$REGION"
    aws lambda update-function-configuration \
        --function-name "$name" --region "$REGION" \
        --environment "Variables={${env_vars}}" >/dev/null
    aws logs put-retention-policy \
        --log-group-name "/aws/lambda/${name}" \
        --retention-in-days "$LOG_RETENTION_DAYS" --region "$REGION" 2>/dev/null || true
}

deploy_function "$FUNCTION_NAME" deploy/lambda.zip "
        TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN:-},
        TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID:-},
        CITY=${CITY:-Winnipeg},
        CATEGORIES=${CATEGORIES:-tech},
        PERIOD_DAYS=${PERIOD_DAYS:-30},
        TEST_MODE=${TEST_MODE:-false},
        ANNUAL_SSM_PARAM=${SSM_PARAM}"

deploy_function "$MEETUP_FUNCTION_NAME" deploy/meetup.zip "
        TELEGRAM_MEETUP_BOT_TOKEN=${TELEGRAM_MEETUP_BOT_TOKEN:-},
        TELEGRAM_MEETUP_CHAT_ID=${TELEGRAM_MEETUP_CHAT_ID:-},
        ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-},
        TEST_MODE=${TEST_MODE:-false},
        MEETUP_SSM_PARAM=${MEETUP_SSM_PARAM}"
```

Remove the standalone "Setting CloudWatch log retention" block (now inside the function).

`create_or_update_rule` gains a fifth argument, the target function name; replace `$FUNCTION_NAME` inside it with `$fn`:

```bash
create_or_update_rule() {
    local rule_name="$1" schedule="$2" input="$3" description="$4" fn="$5"
    ...--function-name "$fn"...
    ...Arn=arn:aws:lambda:${REGION}:${ACCOUNT_ID}:function:${fn}...
}
```

Update the two remaining calls to pass `"$FUNCTION_NAME"`, delete the poll rule call, and add:

```bash
# Daily 14:00 UTC (08:00/09:00 Winnipeg). The meetup bot decides in code
# whether today is a poll day (last Saturday - 11), a summary day (- 3), or nothing.
create_or_update_rule "$RULE_MEETUP" \
    "cron(0 14 * * ? *)" \
    '{}' \
    "Daily tick for the monthly meetup poll bot" \
    "$MEETUP_FUNCTION_NAME"

echo "== Removing legacy rule ${RULE_POLL_LEGACY} (if present) =="
if aws events describe-rule --name "$RULE_POLL_LEGACY" --region "$REGION" >/dev/null 2>&1; then
    aws events remove-targets --rule "$RULE_POLL_LEGACY" --ids 1 --region "$REGION" >/dev/null || true
    aws events delete-rule --name "$RULE_POLL_LEGACY" --region "$REGION" >/dev/null
    aws lambda remove-permission --function-name "$FUNCTION_NAME" \
        --statement-id "allow-${RULE_POLL_LEGACY}" --region "$REGION" 2>/dev/null || true
    echo "  -> deleted"
else
    echo "  -> not present"
fi
```

Extend the final echo block with a meetup invoke example:

```bash
echo "  aws lambda invoke --function-name ${MEETUP_FUNCTION_NAME} \\"
echo "      --payload '{\"date\":\"2026-10-20\"}' --cli-binary-format raw-in-base64-out out.json && cat out.json"
```

Add `deploy/meetup.zip` to `.gitignore`.

- [ ] **Step 2: bootstrap.sh — cover the second parameter**

In `deploy/bootstrap.sh`: add `MEETUP_SSM_PARAM="/winnipeg-tech-events/meetup-state"` and `MEETUP_SSM_PARAM_ARN=...parameter${MEETUP_SSM_PARAM}`; change the policy `Resource` to a JSON array of both ARNs and rename the policy to `ssm-parameters` (keep the old `ssm-annual-events` in place — do not delete, harmless). Seed the meetup parameter with `{}`:

```bash
echo "== Ensuring SSM parameter ${MEETUP_SSM_PARAM} =="
if ! aws ssm get-parameter --name "$MEETUP_SSM_PARAM" --region "$REGION" >/dev/null 2>&1; then
    aws ssm put-parameter --name "$MEETUP_SSM_PARAM" --type String --value '{}' --region "$REGION" >/dev/null
    echo "  -> created (empty state)"
else
    echo "  -> exists; leaving value untouched"
fi
```

Update the header comment to say it bootstraps both Lambdas' SSM access.

- [ ] **Step 3: GitHub Actions**

In `.github/workflows/deploy.yml` Deploy step env: remove the two `TELEGRAM_POLL_*` lines; add

```yaml
          TELEGRAM_MEETUP_BOT_TOKEN: ${{ secrets.TELEGRAM_MEETUP_BOT_TOKEN }}
          TELEGRAM_MEETUP_CHAT_ID: ${{ secrets.TELEGRAM_MEETUP_CHAT_ID }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

Add a second smoke test step that cannot post anything (a date with no poll or summary due):

```yaml
      - name: Smoke test (meetup bot, no-op day)
        run: |
          aws lambda invoke \
            --function-name winnipeg-meetup-bot \
            --payload '{"date":"2026-01-02"}' \
            --cli-binary-format raw-in-base64-out \
            --region "$AWS_REGION" \
            /tmp/meetup.json
          cat /tmp/meetup.json
          grep -q '"success":true' /tmp/meetup.json
```

Rename the workflow to `Deploy Lambdas`.

- [ ] **Step 4: Docs**

`config.example.env`: replace the poll block with

```
# --- Meetup bot (separate Telegram bot, separate Lambda) ---
TELEGRAM_MEETUP_BOT_TOKEN=
TELEGRAM_MEETUP_CHAT_ID=
ANTHROPIC_API_KEY=
```

and fix the comment on line ~29 that lists the poll vars.

`README.md`: rewrite the poll mentions:
- Title line 3: "…posts weekly digests to Telegram, and runs a monthly last-weekend meetup poll with an AI-suggested option."
- Features → Telegram Integration: replace the two poll bullets with "Monthly meetup poll from a **dedicated meetup bot**: opens 11 days before the last weekend of the month, closes 3 days before, announces and pins the winner (needs ≥4 votes)" and "One poll option is generated by Claude Haiku (Winnipeg + the exact dates)".
- Quick Start → Run locally: replace the poll example with the `-action=meetup -date=… -out=…` example from the spec's "Local testing" section.
- Deploy section: schedules list becomes `winnipeg-events-weekly`, `winnipeg-annual-scrape-monthly`, `winnipeg-meetup-daily` (daily 14:00 UTC). `deploy/.env` example uses the new var names.
- Configuration table: replace the two poll rows with rows for `TELEGRAM_MEETUP_BOT_TOKEN`, `TELEGRAM_MEETUP_CHAT_ID`, `ANTHROPIC_API_KEY`, `MEETUP_SSM_PARAM`.
- Telegram Bot Setup → step 3: retitle "Meetup bot" and say: create a second bot with BotFather, add it to the same group, **promote it to admin with "Pin messages"**, set `TELEGRAM_MEETUP_BOT_TOKEN` / `TELEGRAM_MEETUP_CHAT_ID`. Note that `stopPoll` only works on polls sent by this bot.
- "Where secrets live" table and GitHub Secrets table: swap poll rows for the three new secrets.
- Architecture diagram: add the second Lambda:

```
EventBridge (weekly Mon 14:00 UTC)  ─┐
EventBridge (monthly 1st 13:00 UTC) ─┴─▶ Lambda winnipeg-tech-events ──▶ scrape → Telegram digest / SSM annual events
EventBridge (daily 14:00 UTC) ─────────▶ Lambda winnipeg-meetup-bot ───▶ SSM state, Claude Haiku, Telegram poll / stopPoll / pin
```

- Code layout: add `cmd/meetup/`, `internal/meetup/`, `internal/ssmparam/`; remove the `jobs/` mention of "monthly-poll".
- Testing: mention `scripts/check` (added in Task 12).

`CHANGELOG.md` under `[Unreleased]`:

```
### Added
- **Meetup bot** (`cmd/meetup`, Lambda `winnipeg-meetup-bot`): a dedicated Telegram bot that polls the group about the last weekend of each month (Fri evening / Sat noon / Sun noon / an AI-generated idea from Claude Haiku / can't make it), reads the counts back with `stopPoll` three days before the weekend, and posts + pins the winner when it has at least 4 votes. State lives in SSM `/winnipeg-tech-events/meetup-state`; a daily EventBridge tick drives it.

### Removed
- The day-of-week poll on the 20th (`poll` action, `winnipeg-poll-monthly` rule, `TELEGRAM_POLL_*` env vars). Absorbed by the meetup bot.
```

- [ ] **Step 5: Verify scripts parse**

Run: `bash -n deploy/deploy.sh && bash -n deploy/bootstrap.sh && go build ./... && go test ./...`
Expected: no output from `bash -n`; tests PASS.

- [ ] **Step 6: Commit**

```bash
git add deploy .github README.md CHANGELOG.md config.example.env .gitignore
git commit -m "deploy: second Lambda for the meetup bot, daily rule, drop legacy poll rule"
```

---

### Task 12: Scripts contract and repo CLAUDE.md

**Files:**
- Create: `scripts/check`, `scripts/test`, `scripts/build`, `scripts/ship` (all `chmod +x`)
- Create: `CLAUDE.md`

- [ ] **Step 1: Scripts**

`scripts/check`:

```bash
#!/usr/bin/env bash
# Fast gate: format check, vet, unit tests. Exits non-zero on any failure.
set -euo pipefail
cd "$(dirname "$0")/.."
unformatted=$(gofmt -l ./cmd ./internal ./pkg)
if [[ -n "$unformatted" ]]; then
    echo "gofmt needed:" >&2
    echo "$unformatted" >&2
    exit 1
fi
go vet ./...
go test ./... -count=1
```

`scripts/test`:

```bash
#!/usr/bin/env bash
# Full test suite with race detector.
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./... -race -count=1
```

`scripts/build`:

```bash
#!/usr/bin/env bash
# Production build: both Lambda zips, same flags as deploy/deploy.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
for pkg in lambda meetup; do
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -tags lambda.norpc -o bootstrap "./cmd/${pkg}"
    zip -j "deploy/${pkg}.zip" bootstrap >/dev/null
    rm bootstrap
    echo "deploy/${pkg}.zip"
done
```

`scripts/ship`:

```bash
#!/usr/bin/env bash
# Deploy both Lambdas to AWS. Normally CI does this on push to main.
set -euo pipefail
cd "$(dirname "$0")/.."
exec ./deploy/deploy.sh "$@"
```

Run: `chmod +x scripts/*` then `scripts/check`.
Expected: exit 0. If `gofmt -l` lists files, run `gofmt -w` on them and re-run.

- [ ] **Step 2: CLAUDE.md**

`CLAUDE.md` at repo root:

```markdown
# horologium — Winnipeg tech events + meetup bots

Go 1.24 monorepo (single `go.mod`, module `event_calendar`) with two AWS
Lambdas driven by EventBridge. No inbound HTTP anywhere.

## Services

| Binary | Lambda | Bot | Does |
|---|---|---|---|
| `cmd/lambda` | `winnipeg-tech-events` | events bot | weekly digest (`events`), monthly annual-events scrape (`scrape_annual`) |
| `cmd/meetup` | `winnipeg-meetup-bot` | meetup bot | daily tick: last-weekend poll (S-11), stopPoll + summary + pin (S-3) |

`cmd/cli` runs any of them locally; `cmd/server` is the local web UI.

## Scripts contract

- `scripts/check` — gofmt, vet, unit tests (fast gate)
- `scripts/test`  — tests with `-race`
- `scripts/build` — builds `deploy/lambda.zip` and `deploy/meetup.zip`
- `scripts/ship`  — `deploy/deploy.sh` (CI does this on push to `main`; don't run locally unless asked)

## Conventions

- Job logic lives in `internal/` behind small interfaces (`jobs`, `meetup`); `cmd/` only wires real clients. Tests use fakes, never the network.
- Persistent state is one SSM parameter per concern (`internal/ssmparam`). No databases.
- Telegram copy is Ukrainian; MarkdownV2 via `telegram.EscapeMarkdownV2`.
- Anthropic calls go through `github.com/anthropics/anthropic-sdk-go`; model `claude-haiku-4-5` for the idea option.
- Dates: `America/Winnipeg`; binaries import `time/tzdata`.
- Specs in `docs/superpowers/specs/`, plans in `docs/superpowers/plans/`. Update `CHANGELOG.md` under `[Unreleased]`.

## Local smoke

TEST_MODE=true go run ./cmd/cli -action=meetup -date=2026-10-20 -out=/tmp/meetup.json
```

- [ ] **Step 3: Run the gate and commit**

Run: `scripts/check && scripts/test`
Expected: exit 0.

```bash
git add scripts CLAUDE.md
git commit -m "chore: scripts contract and repo CLAUDE.md"
```

---

## Self-review

- **Spec coverage:** dedicated bot + env (T3, T11); daily tick + S-11/S-3 (T4, T9, T11); one Ukrainian multi-select poll with exact copy (T5); AI option via `claude-haiku-4-5`, silent about season/weather, dropped on failure, past ideas, `Conditions` hook (T8, T9); stopPoll counts (T2, T9); winner ≥4, tie → earliest, pin non-fatal (T6, T9); SSM state with options stored and past ideas kept (T7); old poll removed incl. rule (T3, T11); `ssmparam` extraction (T1); CLI `-date`/`-out` (T10); scripts + CLAUDE.md (T12); docs/changelog (T11).
- **Type consistency:** `Bot.SendMessageID` matches `telegram.Service.SendMessageID` (T2/T9); `Store.Get/Put` with `State` (T7/T9); `IdeaGenerator.Generate(ctx, IdeaInput)` (T8/T9); `ssmparam.NewAWSAdapter` used in T10 after the T1 shim is removed there.
- **Placeholders:** none; every step has code or an exact command.
