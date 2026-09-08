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
	PhaseNew     Phase = ""
	PhasePolled  Phase = "polled"
	PhaseStopped Phase = "stopped" // poll closed, counts saved, summary not yet posted
	PhaseDone    Phase = "done"
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
	case PhaseStopped:
		// The poll is already closed; finish posting the summary even if the
		// window has passed, otherwise the month silently dies.
		if !today.Before(w.SummaryDay()) {
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
