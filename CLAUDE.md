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
- `scripts/ship`  — `deploy/deploy.sh` (CI runs it only via manual "Run workflow"; merging never deploys; don't run locally unless asked)
- Dev stage: `STAGE=dev ./deploy/deploy.sh` deploys `-dev` copies of both Lambdas/rules/SSM params with tokens from `deploy/.env` (dev bot + test group).

## Conventions

- Job logic lives in `internal/` behind small interfaces (`jobs`, `meetup`); `cmd/` only wires real clients. Tests use fakes and `httptest`, never the network.
- Persistent state is one SSM parameter per concern (`internal/ssmparam`). No databases.
- Telegram copy is Ukrainian; MarkdownV2 via `telegram.EscapeMarkdownV2`.
- Anthropic calls go through `github.com/anthropics/anthropic-sdk-go`; the idea option uses the newest `claude-haiku-*` from the Models API, falling back to `claude-haiku-4-5`.
- Dates: `America/Winnipeg`; binaries import `time/tzdata`.
- Specs in `docs/superpowers/specs/`, plans in `docs/superpowers/plans/`. Update `CHANGELOG.md` under `[Unreleased]`.

## Local smoke

```bash
TEST_MODE=true go run ./cmd/cli -action=meetup -date=2026-10-20 -out=/tmp/meetup.json
```
