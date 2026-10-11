package meetup

import (
	"context"
	"fmt"
	"log"
	"time"

	"event_calendar/internal/models"
	"event_calendar/pkg/telegram"
)

// EventSource lists upcoming local events; *scraping.ScrapingService
// satisfies it. Optional: without it the AI pick has no event list.
type EventSource interface {
	ScrapeEvents(city, category string, period time.Duration) ([]models.Event, error)
}

// maxIdeaEvents caps the list handed to the model.
const maxIdeaEvents = 15

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
	Events   EventSource // optional
	City     string      // for Events; defaults to Winnipeg
	BotToken string      // validated up front so a misconfigured deploy fails on every tick, not on poll day
	ChatID   string
	TestMode bool
	Loc      *time.Location
	// Sleep is used between state-write retries; tests override it.
	Sleep func(time.Duration)
}

// putRetries is how many times a state write is attempted before giving up.
const putRetries = 3

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
	if deps.BotToken == "" {
		return fail("none", "TELEGRAM_MEETUP_BOT_TOKEN not set")
	}
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
	if stored.Phase == PhaseStopped {
		if today.Before(resumeDeadline(stored.Month, deps.Loc)) {
			// A previous tick closed the poll but never posted the summary.
			// Finish that month first, even if the calendar has rolled over;
			// ForMonth would otherwise discard the saved counts. summarize
			// is side-effect free in TestMode, so dry runs show this step.
			log.Printf("meetup: resuming unfinished summary for %s", stored.Month)
			return summarize(ctx, deps, stored)
		}
		// Posting has failed every day up to the next month's poll day.
		// Give up on that month so the bot does not stay stuck forever.
		if deps.TestMode {
			log.Printf("test mode: would give up on the unfinished %s summary", stored.Month)
		} else {
			stored.Phase = PhaseDone
			if err := putWithRetry(ctx, deps, stored); err != nil {
				return fail("none", "abandon %s: state write failed: %v", stored.Month, err)
			}
			alert(deps, fmt.Sprintf("⚠️ meetup bot: giving up on the %s summary (posting kept failing). Counts were %v.", stored.Month, stored.Counts))
		}
	}
	st := stored.ForMonth(w.Key())
	step := NextStep(today, w, st.Phase)
	log.Printf("meetup: today=%s weekend=%s..%s phase=%q step=%s",
		today.Format("2006-01-02"), w.Fri.Format("01-02"), w.Sun.Format("01-02"), st.Phase, step)

	switch step {
	case StepSendPoll:
		return sendPoll(ctx, deps, today, w, st)
	case StepSummarize:
		return summarize(ctx, deps, st)
	default:
		return Result{Success: true, Step: step.String(), Month: w.Key()}
	}
}

func sendPoll(ctx context.Context, deps Deps, today time.Time, w Weekend, st State) Result {
	var idea Idea
	if deps.Ideas != nil {
		from, to := today, today.AddDate(0, 0, 21)
		got, err := deps.Ideas.Generate(ctx, IdeaInput{Weekend: w, From: from, To: to, PastIdeas: st.PastIdeas, Events: upcomingEvents(deps, from, to)})
		if err != nil {
			log.Printf("meetup: idea generation failed, sending poll without AI option: %v", err)
		} else {
			idea = got
		}
	}
	question := Question(w)
	options := Options(w, idea.Text)

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
	st.IdeaDetails = idea.Details
	st.RememberIdea(idea.Text)
	if err := putWithRetry(ctx, deps, st); err != nil {
		// The poll is out but we lost track of it; the next tick would send
		// another. Tell the group so a human can intervene.
		alert(deps, fmt.Sprintf("⚠️ meetup bot: poll %d sent but state write failed (%v). Next tick may repeat the poll; please check SSM.", id, err))
		return fail(StepSendPoll.String(), "poll %d sent but state write failed: %v", id, err)
	}
	return Result{Success: true, Step: StepSendPoll.String(), Month: w.Key(), PollMessageID: id}
}

// upcomingEvents scrapes local events and keeps those inside [from, to].
// Any failure just means no list; the poll must still go out.
func upcomingEvents(deps Deps, from, to time.Time) []models.Event {
	if deps.Events == nil {
		return nil
	}
	city := deps.City
	if city == "" {
		city = "Winnipeg"
	}
	all, err := deps.Events.ScrapeEvents(city, "tech", to.Sub(from)+24*time.Hour)
	if err != nil {
		log.Printf("meetup: event scrape failed, AI pick gets no event list: %v", err)
		return nil
	}
	var out []models.Event
	for _, e := range all {
		if !e.StartTime.Before(from) && e.StartTime.Before(to.AddDate(0, 0, 1)) {
			out = append(out, e)
			if len(out) == maxIdeaEvents {
				break
			}
		}
	}
	log.Printf("meetup: %d of %d scraped events fall in the idea window", len(out), len(all))
	return out
}

// resumeDeadline is the last instant a stopped-but-unposted summary for
// month key ("2006-01") is still worth posting: the next month's poll day.
func resumeDeadline(key string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01", key, loc)
	if err != nil {
		return time.Time{} // unparseable month: give up immediately
	}
	return LastWeekend(t.Year(), t.Month()+1, loc).PollDay()
}

// alert posts an operator-facing message to the group; failures are logged only.
func alert(deps Deps, text string) {
	if _, err := deps.Bot.SendMessageID(deps.ChatID, telegram.EscapeMarkdownV2(text)); err != nil {
		log.Printf("meetup: alert send failed: %v", err)
	}
}

// putWithRetry writes state, retrying transient failures with a short
// backoff. A cancelled ctx ends the retries early.
func putWithRetry(ctx context.Context, deps Deps, st State) error {
	sleep := deps.Sleep
	if sleep == nil {
		sleep = func(d time.Duration) {
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}
		}
	}
	var err error
	for i := 0; i < putRetries; i++ {
		if err = deps.Store.Put(ctx, st); err == nil {
			return nil
		}
		log.Printf("meetup: state write attempt %d/%d failed: %v", i+1, putRetries, err)
		if i < putRetries-1 {
			sleep(time.Duration(i+1) * 2 * time.Second)
			if ctx.Err() != nil {
				return fmt.Errorf("%w (after: %v)", ctx.Err(), err)
			}
		}
	}
	return err
}

func summarize(ctx context.Context, deps Deps, st State) Result {
	if deps.TestMode {
		log.Printf("test mode: would stop poll %d and post summary to %s", st.PollMessageID, deps.ChatID)
		return Result{Success: true, Step: StepSummarize.String(), Month: st.Month, PollMessageID: st.PollMessageID}
	}

	// Stop the poll exactly once. If a previous tick stopped it but failed
	// before posting, the counts are already in state and stopPoll would
	// be rejected by Telegram ("poll has already been closed").
	counts := st.Counts
	switch {
	case st.Phase == PhaseStopped && counts == nil:
		// Should be impossible (stopped is only written together with
		// counts); never call stopPoll twice, so surface it instead.
		return fail(StepSummarize.String(), "poll %d is marked stopped but has no counts", st.PollMessageID)
	case st.Phase != PhaseStopped:
		var err error
		counts, err = deps.Bot.StopPoll(deps.ChatID, st.PollMessageID)
		if err != nil {
			return fail(StepSummarize.String(), "stop poll %d: %v", st.PollMessageID, err)
		}
		st.Phase = PhaseStopped
		st.Counts = counts
		if err := putWithRetry(ctx, deps, st); err != nil {
			// The poll is closed and the counts exist only in this process.
			alert(deps, fmt.Sprintf("⚠️ meetup bot: poll %d closed but state write failed (%v). Counts: %v. Please check SSM.", st.PollMessageID, err, counts))
			return fail(StepSummarize.String(), "poll %d stopped but state write failed: %v", st.PollMessageID, err)
		}
	}
	winner, ok := PickWinner(counts)
	text := FormatSummary(st.Options, counts, winner, ok, st.IdeaDetails)

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
	if err := putWithRetry(ctx, deps, st); err != nil {
		return fail(StepSummarize.String(), "summary posted but state write failed: %v", err)
	}
	return res
}
