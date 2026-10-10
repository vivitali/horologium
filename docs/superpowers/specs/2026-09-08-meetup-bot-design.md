# Meetup Bot: Monthly Last-Weekend Poll with AI Idea

**Status:** Approved (design)
**Date:** 2026-09-08
**Author:** vivitali (with Claude)

## Problem

The repo currently posts a single fire-and-forget Telegram poll on the 20th
of each month ("which day of the week?"). Nobody reads the result back, the
poll does not name a date, and it comes from the same code path and deploy
unit as the weekly events digest. The maintainer wants a **separate bot**, a
**separate service** in the same repo (monorepo), that runs one concrete
questionnaire per month about the community meetup on the **last weekend of
the month**, reads the votes back, and announces the decision.

A two-way bot was built once before (inline-keyboard voting with an in-memory
webhook server) and removed in the 2026-05-17 cleanup with the note "can be
re-added later with proper storage". This design deliberately avoids an
inbound path entirely.

## Goals

- One native Telegram poll per month, sent by a **dedicated meetup bot**,
  naming the exact Fri/Sat/Sun dates of the last weekend.
- Read the vote counts back **without a webhook**, decide a winner, post and
  pin a summary.
- One of the poll options is an **AI-generated idea** (Claude Haiku), tied to
  Winnipeg and the specific dates, silently informed by the season.
- The new service is a **second Lambda binary** in the same Go module, with
  its own env, EventBridge rule, and state parameter.
- Retire the old day-of-week poll: action, rule, config, job.
- Add the `scripts/` contract and a repo `CLAUDE.md` per `~/Developer`
  conventions.

## Non-goals

- Receiving `poll_answer` updates, voter names, or @mentions (would need a
  Function URL + DynamoDB; the `Store` boundary leaves room for it later).
- Live weather forecasts (11 days out is noise). The idea generator accepts an
  optional `Conditions` hint, empty for now.
- A `go.work` multi-module split. One `go.mod`, multiple `cmd/` binaries.
- Any change to the weekly digest or annual-events flows beyond removing the
  old poll.

## Decisions (from the design interview)

| Question | Decision |
|---|---|
| Inbound path | None. Counts via `stopPoll`; state in SSM. |
| Existing day-of-week poll | Absorbed and removed. |
| Number of polls | One, multi-select, non-anonymous, Ukrainian. |
| Anchor | S = last Saturday of the month. Fri = S-1, Sun = S+1. |
| Schedule | Daily EventBridge tick at 14:00 UTC; code decides. Poll on S-11 (Monday). Summary on S-3 (Wednesday). |
| Winner | Most votes excluding "без мене". Tie → earliest option. Fewer than **4** votes → skip month. |
| Summary | Posted as a message; pinned when there is a winner. |
| AI idea | `claude-haiku-4-5`. Inputs: Winnipeg, the three dates, month/season, past ideas. Output: `<slot> — <activity>`, Ukrainian, short. Must not mention season or weather. On error the poll ships without it. |
| Chat | Same group as the digest; bot is admin with pin rights. Pin failure is logged, never fatal. |
| Layout | `cmd/meetup` + `internal/meetup`; reuse `pkg/telegram`, `internal/config`. |

## Architecture

```
EventBridge (daily 14:00 UTC) ──▶ Lambda winnipeg-meetup-bot (Go)
                                      │
                                      ├─ SSM /winnipeg-tech-events/meetup-state  (read/write)
                                      ├─ Anthropic API (claude-haiku-4-5)         (poll day only)
                                      └─ Telegram Bot API: sendPoll / stopPoll / sendMessage / pinChatMessage
```

The existing `winnipeg-tech-events` Lambda keeps `events` and
`scrape_annual`; the `poll` action and `winnipeg-poll-monthly` rule are
deleted by `deploy.sh`.

### Daily tick state machine

All dates are civil dates in `America/Winnipeg`. The tick at 14:00 UTC is
08:00 or 09:00 local, so "today" is unambiguous.

```
today  = now in America/Winnipeg, truncated to date
W      = last weekend of today's month  (Sat = last Saturday; Fri = Sat-1; Sun = Sat+1)
key    = W.Sat formatted "2006-01"
state  = SSM read; if state.month != key → reset phase/message/options, keep past_ideas

phase == ""       && pollDay <= today < summaryDay   → SEND POLL   (pollDay = Sat-11)
phase == "polled" && summaryDay <= today <= Sat      → SUMMARIZE   (summaryDay = Sat-3)
otherwise                                            → NOOP
```

`<=` comparisons make a missed day self-heal on the next tick. If the whole
poll window is missed, the month is silently skipped.

### State (SSM `String` parameter, JSON)

```json
{
  "month": "2026-10",
  "phase": "polled",
  "poll_message_id": 1234,
  "options": ["🍻 …", "☕ …", "☕ …", "🤖 …", "❌ Цього разу не зможу"],
  "past_ideas": ["Субота після обіду — лижі в Windsor Park", "…"]
}
```

`options` is stored as sent so the summary maps `stopPoll` counts by index
even if the option builder changes later. `past_ideas` keeps the last 12.

### Poll

Question (≤300 chars):

```
#meetup Зустріч в останні вихідні місяця (24–26 жовтня). Оберіть зручний час (орієнтовно), можна кілька варіантів. Зустріч відбудеться, якщо набереться хоча б 4 голоси
```

If Sunday falls in the next month: `(30 жовтня – 1 листопада)`.

Options (each ≤100 chars), `allows_multiple_answers=true`, `is_anonymous=false`:

1. `🍻 Пт 24 жовтня, ~18:30`
2. `☕ Сб 25 жовтня, ~12:30`
3. `🍻 Сб 25 жовтня, ~18:30`
4. `☕ Нд 26 жовтня, ~12:30`
5. `🍻 Нд 26 жовтня, ~18:30`
6. `🤖 <slot> — <activity>` (omitted entirely if generation failed)
7. `❌ Цього разу не зможу` (always last)

### Summary

`stopPoll` returns `voter_count` per option in order. Winner = highest count
among all options except the last; ties → lowest index. With a winner and
count ≥ 4:

```
#meetup
📌 Домовились: ☕ Сб 25 жовтня, ~12:30
5 голосів — до зустрічі!

Як голосували:
🍻 Пт 24 жовтня, ~18:30 — 2
☕ Сб 25 жовтня, ~12:30 — 5
…
```

Otherwise:

```
Цього разу не збираємось: найбільше 2 голоси, а треба хоча б 4. Спробуємо наступного місяця 🙂
```

Sent as MarkdownV2 (escaped). Pinned only when there is a winner.

### AI idea generator

- Model `claude-haiku-4-5`, `max_tokens` 200, 20 s request timeout, no thinking.
- System prompt (fixed, cacheable-order first): role, Winnipeg context, hard
  rules: Ukrainian, exactly one line, format `<slot> — <activity>`, ≤ 70
  characters, must be a concrete activity somewhere in or near Winnipeg,
  pick one of the three days with an approximate start time, **never mention
  the season, month, or weather in the text**, avoid anything in the
  past-ideas list.
- User message: the three dates, month name, season word, optional
  conditions hint, past ideas.
- Parsing: first non-empty line, trimmed, hard-capped to 96 characters so
  `🤖 ` + text fits Telegram's 100-char option limit. Empty → treated as
  failure.
- Any error → logged, poll ships with 4 options.

### Configuration

| Env var | Lambda | Purpose |
|---|---|---|
| `TELEGRAM_MEETUP_BOT_TOKEN` | meetup | Dedicated meetup bot |
| `TELEGRAM_MEETUP_CHAT_ID` | meetup | Group chat (same as digest) |
| `ANTHROPIC_API_KEY` | meetup | Idea generation |
| `MEETUP_SSM_PARAM` | meetup | default `/winnipeg-tech-events/meetup-state` |
| `TEST_MODE` | both | meetup: log the decision, send nothing, write no state |

Removed: `TELEGRAM_POLL_BOT_TOKEN`, `TELEGRAM_POLL_CHAT_ID`.

Secrets flow unchanged: `deploy/.env` locally, GitHub Secrets in CI, Lambda
env set by `deploy.sh`.

### Code layout

```
cmd/
  meetup/main.go          # Lambda entry; optional {"date":"YYYY-MM-DD"} override
  cli/main.go             # +  -action=meetup [-date=YYYY-MM-DD] [-out=state.json]
internal/
  ssmparam/               # extracted from internal/annual: API, ErrNotFound, AWS adapter
  meetup/
    schedule.go           # Weekend, LastWeekend, NextStep
    poll.go               # Question, Options, Ukrainian date formatting
    summary.go            # PickWinner, FormatSummary
    idea.go               # IdeaGenerator, ClaudeIdeas (anthropic-sdk-go)
    state.go              # State, Store, SSMStore, FileStore
    run.go                # Run(ctx, Deps, now) Result — the orchestrator
pkg/telegram/service.go   # SendPoll returns message id; + StopPoll, SendMessageID,
                          #   PinChatMessage, EscapeMarkdownV2; - SendMonthlyMeetupPoll
deploy/
  deploy.sh               # builds + deploys both functions; deletes old poll rule
  bootstrap.sh            # SSM policy covers both parameters; seeds meetup-state
scripts/check|test|build  # contract scripts
CLAUDE.md                 # repo-level instructions
```

### Local testing

```
TEST_MODE=true go run ./cmd/cli -action=meetup -date=2026-10-12 -out=/tmp/meetup.json
```

`-date` overrides "today"; `-out` swaps SSM for a JSON file. Without
`TEST_MODE` and with real tokens this sends to the configured chat.

## Risks

- **Bot must own the poll**: `stopPoll` only works on polls the same bot
  sent. Never point the meetup token at a poll sent by the events bot.
- **Pinning needs admin**: handled as a logged non-fatal error.
- **Timezone data in Lambda**: `provided.al2023` may lack tzdata; binaries
  import `time/tzdata`.
- **Two Lambdas, one role**: both share `winnipeg-tech-events-lambda-role`;
  `bootstrap.sh` widens the SSM policy to both parameters.
