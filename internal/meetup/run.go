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
