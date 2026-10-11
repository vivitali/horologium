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
