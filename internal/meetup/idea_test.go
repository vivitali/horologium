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
		time.December: "зима", time.January: "зима", time.February: "зима",
		time.March: "весна", time.May: "весна",
		time.June: "літо", time.August: "літо",
		time.September: "осінь", time.November: "осінь",
	}
	for m, want := range cases {
		if got := Season(m); got != want {
			t.Errorf("%v: got %q want %q", m, got, want)
		}
	}
}

func TestCleanIdea(t *testing.T) {
	cases := map[string]string{
		"Субота після обіду — більярд":         "Субота після обіду — більярд",
		"\n\n  \"Неділя 13:00 — боулінг\"  \n": "Неділя 13:00 — боулінг",
		"🤖 Пʼятниця 19:00 — квіз\nдруга лінія": "Пʼятниця 19:00 — квіз",
		strings.Repeat("я", 120):               strings.Repeat("я", MaxIdeaLen-1) + "…",
		"   ":                                  "",
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
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &lastReq)
		w.Header().Set("Content-Type", "application/json")
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
	srv, req := fakeAnthropic(t, "\"Субота 14:00 — лижі у Windsor Park\"\n", http.StatusOK)
	g := NewClaudeIdeas("k", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	w := LastWeekend(2027, time.January, wpg)
	got, err := g.Generate(context.Background(), IdeaInput{Weekend: w, PastIdeas: []string{"боулінг"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Субота 14:00 — лижі у Windsor Park" {
		t.Errorf("got %q", got)
	}
	if (*req)["model"] != IdeaModel {
		t.Errorf("model = %v", (*req)["model"])
	}
	body, _ := json.Marshal(*req)
	for _, want := range []string{"29 січня", "30 січня", "31 січня", "зима", "боулінг", "Winnipeg"} {
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
