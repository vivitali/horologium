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
		time.December: "winter", time.January: "winter", time.February: "winter",
		time.March: "spring", time.May: "spring",
		time.June: "summer", time.August: "summer",
		time.September: "fall", time.November: "fall",
	}
	for m, want := range cases {
		if got := Season(m); got != want {
			t.Errorf("%v: got %q want %q", m, got, want)
		}
	}
}

func TestCleanIdea(t *testing.T) {
	cases := map[string]string{
		"Sat afternoon — pool":            "Sat afternoon — pool",
		"\n\n  \"Sun 1pm — bowling\"  \n": "Sun 1pm — bowling",
		"🤖 Fri 7pm — trivia\nsecond line": "Fri 7pm — trivia",
		strings.Repeat("я", 120):          strings.Repeat("я", MaxIdeaLen-1) + "…",
		"   ":                             "",
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
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			_, _ = io.WriteString(w, `{"data":[
				{"type":"model","id":"claude-opus-9","display_name":"Opus 9","created_at":"2030-01-01T00:00:00Z"},
				{"type":"model","id":"claude-haiku-4-5","display_name":"Haiku 4.5","created_at":"2025-10-01T00:00:00Z"},
				{"type":"model","id":"claude-haiku-7","display_name":"Haiku 7","created_at":"2029-01-01T00:00:00Z"}],
				"has_more":false,"first_id":"claude-opus-9","last_id":"claude-haiku-7"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &lastReq)
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
	srv, req := fakeAnthropic(t, "\"Sat ~2pm — skiing at Windsor Park\"\n\nDetails: Rent skis at the park pavilion, about $15. Free parking.\n", http.StatusOK)
	g := NewClaudeIdeas("k", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	w := LastWeekend(2027, time.January, wpg)
	got, err := g.Generate(context.Background(), IdeaInput{Weekend: w, PastIdeas: []string{"bowling"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Sat ~2pm — skiing at Windsor Park" {
		t.Errorf("text = %q", got.Text)
	}
	if got.Details != "Rent skis at the park pavilion, about $15. Free parking." {
		t.Errorf("details = %q", got.Details)
	}
	if (*req)["model"] != "claude-haiku-7" {
		t.Errorf("model = %v, want the newest haiku from the models list", (*req)["model"])
	}
	body, _ := json.Marshal(*req)
	for _, want := range []string{"Jan 29", "Jan 30", "Jan 31", "winter", "bowling", "Winnipeg"} {
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

func TestLatestModel_FallsBackWhenListFails(t *testing.T) {
	srv, _ := fakeAnthropic(t, "", http.StatusInternalServerError)
	g := NewClaudeIdeas("k", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if got := g.latestModel(context.Background()); got != IdeaModel {
		t.Errorf("got %q want %q", got, IdeaModel)
	}
}

func TestParseIdea_NoDetails(t *testing.T) {
	got := parseIdea("Sun 1pm — bowling\n")
	if got.Text != "Sun 1pm — bowling" || got.Details != "" {
		t.Errorf("got %+v", got)
	}
}
