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
	// 31 Oct 2026 is a Saturday; Sat-11 = Tue 20 Oct.
	if !w.PollDay().Equal(d(2026, 10, 20)) || w.PollDay().Weekday() != time.Tuesday {
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
		{"stopped is not a window decision", d(2026, 10, 29), PhaseStopped, StepNone},
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
