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
	IdeaDetails   string   `json:"idea_details,omitempty"` // blurb for the 🤖 option, shown if it wins
	Counts        []int    `json:"counts,omitempty"`       // set once the poll is stopped
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

// Get reads the file; a missing file is a zero State.
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

// Put writes the state as indented JSON.
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
