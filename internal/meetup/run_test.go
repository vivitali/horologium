package meetup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeBot struct {
	pollQ    string
	pollOpts []string
	pollErr  error
	stopped  int64
	counts   []int
	stopErr  error
	sentText string
	sendErr  error
	pinned   int64
	pinErr   error
	calls    []string
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
	st       State
	getErr   error
	putErr   error
	failPuts int // fail this many Put calls, then succeed
	puts     int
}

func (m *memStore) Get(ctx context.Context) (State, error) { return m.st, m.getErr }
func (m *memStore) Put(ctx context.Context, s State) error {
	m.puts++
	if m.failPuts > 0 {
		m.failPuts--
		return errors.New("transient")
	}
	if m.putErr != nil {
		return m.putErr
	}
	m.st = s
	return nil
}

type fakeIdeas struct {
	idea    string
	details string
	err     error
	in      IdeaInput
}

func (f *fakeIdeas) Generate(ctx context.Context, in IdeaInput) (Idea, error) {
	f.in = in
	return Idea{Text: f.idea, Details: f.details}, f.err
}

func deps(b *fakeBot, s *memStore, i *fakeIdeas) Deps {
	return Deps{Bot: b, Store: s, Ideas: i, BotToken: "tok", ChatID: "-100", Loc: wpg, Sleep: func(time.Duration) {}}
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
	b, s, i := &fakeBot{}, &memStore{st: State{Month: "2026-09", Phase: PhaseDone, PastIdeas: []string{"old"}}}, &fakeIdeas{idea: "Sat ~2pm — pool", details: "Pool hall on Osborne, $10/hour."}
	res := Run(context.Background(), deps(b, s, i), tick(2026, 10, 20))
	if !res.Success || res.Step != "send_poll" || res.PollMessageID != 111 {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(b.pollQ, "Oct 30 – Nov 1") {
		t.Errorf("question = %q", b.pollQ)
	}
	if len(b.pollOpts) != 7 || b.pollOpts[5] != "🤖 AI pick: Sat ~2pm — pool" {
		t.Errorf("options = %q", b.pollOpts)
	}
	if s.st.Month != "2026-10" || s.st.Phase != PhasePolled || s.st.PollMessageID != 111 || len(s.st.Options) != 7 {
		t.Errorf("state = %+v", s.st)
	}
	if s.st.IdeaDetails != "Pool hall on Osborne, $10/hour." {
		t.Errorf("idea details not stored: %+v", s.st)
	}
	if len(s.st.PastIdeas) != 2 || s.st.PastIdeas[1] != "Sat ~2pm — pool" {
		t.Errorf("past ideas = %v", s.st.PastIdeas)
	}
	if len(i.in.PastIdeas) != 1 || i.in.PastIdeas[0] != "old" {
		t.Errorf("generator should see previous ideas: %+v", i.in)
	}
}

func TestRun_SendsPollWithoutIdeaOnGeneratorError(t *testing.T) {
	b, s := &fakeBot{}, &memStore{}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{err: errors.New("down")}), tick(2026, 10, 20))
	if !res.Success || len(b.pollOpts) != 6 || b.pollOpts[5] != OptionCannot {
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
		Options: []string{"🍻 fri", "☕ sat", "☕ sun", "🤖 AI pick: idea", OptionCannot}}
}

func TestRun_SummarizesAndPins(t *testing.T) {
	b, s := &fakeBot{counts: []int{1, 5, 2, 0, 3}}, &memStore{st: polledState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if !res.Success || res.Step != "summarize" || res.Winner != "☕ sat" {
		t.Fatalf("res=%+v", res)
	}
	if b.stopped != 111 || b.pinned != 222 {
		t.Errorf("stopped=%d pinned=%d", b.stopped, b.pinned)
	}
	if !strings.Contains(b.sentText, "5 голосів") {
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
	if !strings.Contains(b.sentText, "не збираємось") {
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
		t.Errorf("missing chat id: res=%+v", res)
	}
	d = deps(&fakeBot{}, &memStore{}, &fakeIdeas{})
	d.BotToken = ""
	if res := Run(context.Background(), d, tick(2026, 10, 5)); res.Success {
		t.Errorf("missing bot token must fail even on a no-op day: res=%+v", res)
	}
}

func TestRun_PollSent_StateWriteRetriesThenSucceeds(t *testing.T) {
	b, s := &fakeBot{}, &memStore{failPuts: 2}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 20))
	if !res.Success || s.puts != 3 || s.st.Phase != PhasePolled {
		t.Errorf("res=%+v puts=%d state=%+v", res, s.puts, s.st)
	}
}

func TestRun_PollSent_StateWriteFailsAlertsGroup(t *testing.T) {
	b, s := &fakeBot{}, &memStore{putErr: errors.New("ssm down")}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 20))
	if res.Success || s.puts != putRetries {
		t.Errorf("res=%+v puts=%d", res, s.puts)
	}
	if !strings.Contains(b.sentText, "state write failed") {
		t.Errorf("expected an alert message, got %q", b.sentText)
	}
}

func TestRun_Summary_SendFailureThenResumeWithoutSecondStop(t *testing.T) {
	// First tick: stopPoll ok, sendMessage fails -> phase stopped, counts saved.
	b, s := &fakeBot{counts: []int{0, 5, 0, 0, 1}, sendErr: errors.New("tg down")}, &memStore{st: polledState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if res.Success || s.st.Phase != PhaseStopped || len(s.st.Counts) != 5 {
		t.Fatalf("first tick: res=%+v state=%+v", res, s.st)
	}
	// Next tick (even after Saturday): no stopPoll, summary posts from saved counts.
	b2 := &fakeBot{stopErr: errors.New("poll has already been closed")}
	res = Run(context.Background(), deps(b2, s, &fakeIdeas{}), tick(2026, 11, 1))
	if !res.Success || res.Winner != "☕ sat" || s.st.Phase != PhaseDone {
		t.Fatalf("second tick: res=%+v state=%+v", res, s.st)
	}
	for _, c := range b2.calls {
		if c == "stopPoll" {
			t.Error("stopPoll must not be called again")
		}
	}
	if !strings.Contains(b2.sentText, "5 голосів") {
		t.Errorf("summary = %q", b2.sentText)
	}
}

func stoppedState() State {
	st := polledState()
	st.Phase = PhaseStopped
	st.Counts = []int{0, 5, 0, 0, 1}
	return st
}

func TestRun_StoppedMonthGivesUpAtNextPollDay(t *testing.T) {
	// October is stuck in "stopped"; 20 Nov is past November's poll day (17 Nov).
	b, s := &fakeBot{}, &memStore{st: stoppedState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 11, 20))
	if !res.Success || res.Step != "send_poll" || res.Month != "2026-11" {
		t.Fatalf("expected to move on and send November's poll: res=%+v", res)
	}
	if len(b.calls) < 2 || b.calls[0] != "sendMessage" || b.calls[1] != "sendPoll" {
		t.Errorf("expected alert then poll, got %v", b.calls)
	}
	if !strings.Contains(strings.ReplaceAll(b.sentText, "\\", ""), "giving up on the 2026-10 summary") {
		t.Errorf("alert = %q", b.sentText)
	}
	if s.st.Month != "2026-11" || s.st.Phase != PhasePolled {
		t.Errorf("state = %+v", s.st)
	}
	for _, c := range b.calls {
		if c == "stopPoll" {
			t.Error("stopPoll must not be called for the abandoned month")
		}
	}
}

func TestRun_StoppedMonthResumesBeforeNextPollDay(t *testing.T) {
	b, s := &fakeBot{}, &memStore{st: stoppedState()}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 11, 16))
	if !res.Success || res.Step != "summarize" || res.Month != "2026-10" || res.Winner != "☕ sat" {
		t.Fatalf("res=%+v", res)
	}
	if s.st.Phase != PhaseDone || s.st.Month != "2026-10" {
		t.Errorf("state = %+v", s.st)
	}
}

func TestRun_TestMode_StoppedState(t *testing.T) {
	// Past the deadline: would give up, then reports the current month's step.
	b, s := &fakeBot{}, &memStore{st: stoppedState()}
	d := deps(b, s, &fakeIdeas{})
	d.TestMode = true
	res := Run(context.Background(), d, tick(2026, 11, 20))
	if !res.Success || res.Step != "send_poll" || res.Month != "2026-11" || len(b.calls) != 0 || s.puts != 0 {
		t.Errorf("past deadline: res=%+v calls=%v puts=%d", res, b.calls, s.puts)
	}
	// Same month, still resumable: reports summarize without side effects.
	b, s = &fakeBot{}, &memStore{st: stoppedState()}
	d = deps(b, s, &fakeIdeas{})
	d.TestMode = true
	res = Run(context.Background(), d, tick(2026, 10, 29))
	if !res.Success || res.Step != "summarize" || res.Month != "2026-10" || len(b.calls) != 0 || s.puts != 0 {
		t.Errorf("resumable: res=%+v calls=%v puts=%d", res, b.calls, s.puts)
	}
}

func TestRun_GiveUpWriteFailureSendsNothing(t *testing.T) {
	b, s := &fakeBot{}, &memStore{st: stoppedState(), putErr: errors.New("ssm down")}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 11, 20))
	if res.Success || len(b.calls) != 0 || s.st.Phase != PhaseStopped {
		t.Errorf("res=%+v calls=%v state=%+v", res, b.calls, s.st)
	}
}

func TestResumeDeadline(t *testing.T) {
	if got := resumeDeadline("2026-12", wpg); !got.Equal(d(2027, 1, 19)) {
		t.Errorf("Dec->Jan rollover: got %v", got) // last Sat Jan 2027 = 30th; -11 = 19th
	}
	if got := resumeDeadline("2026-10", wpg); !got.Equal(d(2026, 11, 17)) {
		t.Errorf("Oct->Nov: got %v", got)
	}
	if got := resumeDeadline("garbage", wpg); !got.IsZero() {
		t.Errorf("unparseable key should give zero time, got %v", got)
	}
}

func TestRun_StoppedWithoutCountsFailsWithoutSecondStop(t *testing.T) {
	st := polledState()
	st.Phase = PhaseStopped
	b, s := &fakeBot{}, &memStore{st: st}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 29))
	if res.Success || len(b.calls) != 0 {
		t.Errorf("res=%+v calls=%v", res, b.calls)
	}
}

func TestRun_StopSucceedsWriteFailsAlerts(t *testing.T) {
	b, s := &fakeBot{counts: []int{0, 4, 0, 0, 0}}, &memStore{st: polledState(), putErr: errors.New("ssm down")}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if res.Success || !strings.Contains(b.sentText, "closed but state write failed") {
		t.Errorf("res=%+v sent=%q", res, b.sentText)
	}
}

func TestPutWithRetry_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &memStore{putErr: errors.New("down")}
	d := deps(&fakeBot{}, s, &fakeIdeas{})
	d.Sleep = func(time.Duration) { cancel() }
	if err := putWithRetry(ctx, d, State{}); err == nil || s.puts != 1 {
		t.Errorf("err=%v puts=%d", err, s.puts)
	}
}

func TestRun_SummaryShowsIdeaDetailsOnlyWhenIdeaWins(t *testing.T) {
	st := polledState()
	st.IdeaDetails = "Pool hall on Osborne, $10/hour."
	b, s := &fakeBot{counts: []int{0, 1, 0, 5, 0}}, &memStore{st: st}
	res := Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if !res.Success || res.Winner != "🤖 AI pick: idea" || !strings.Contains(b.sentText, "Pool hall on Osborne") {
		t.Errorf("idea win: res=%+v text=%q", res, b.sentText)
	}
	st.IdeaDetails = "Pool hall on Osborne, $10/hour."
	b, s = &fakeBot{counts: []int{0, 5, 0, 1, 0}}, &memStore{st: st}
	res = Run(context.Background(), deps(b, s, &fakeIdeas{}), tick(2026, 10, 28))
	if !res.Success || res.Winner != "☕ sat" || strings.Contains(b.sentText, "Pool hall") {
		t.Errorf("other win must not show details: res=%+v text=%q", res, b.sentText)
	}
}
