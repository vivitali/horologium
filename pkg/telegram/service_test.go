package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestService returns a Service pointed at a fake Telegram API that
// records the last request path/body and replies with the given JSON.
func newTestService(t *testing.T, reply string) (*Service, *string, *string) {
	t.Helper()
	var lastPath, lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastPath, lastBody = r.URL.Path, string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	s := NewService("tok")
	s.baseURL = srv.URL + "/bottok"
	return s, &lastPath, &lastBody
}

func TestSendPoll_ReturnsMessageID(t *testing.T) {
	s, path, body := newTestService(t, `{"ok":true,"result":{"message_id":42}}`)
	id, err := s.SendPoll("-1", "Q?", []string{"a", "b"}, true)
	if err != nil || id != 42 {
		t.Fatalf("got id=%d err=%v", id, err)
	}
	if *path != "/bottok/sendPoll" {
		t.Errorf("path = %s", *path)
	}
	var req map[string]any
	_ = json.Unmarshal([]byte(*body), &req)
	if req["is_anonymous"] != false || req["allows_multiple_answers"] != true {
		t.Errorf("unexpected body: %s", *body)
	}
}

func TestSendPoll_RejectsBadOptionCount(t *testing.T) {
	s, _, _ := newTestService(t, `{"ok":true}`)
	if _, err := s.SendPoll("-1", "Q?", []string{"only"}, false); err == nil {
		t.Error("expected error for 1 option")
	}
}

func TestStopPoll_ReturnsCountsInOrder(t *testing.T) {
	s, path, _ := newTestService(t, `{"ok":true,"result":{"id":"p","question":"Q","options":[{"text":"a","voter_count":3},{"text":"b","voter_count":0},{"text":"c","voter_count":5}]}}`)
	counts, err := s.StopPoll("-1", 42)
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/bottok/stopPoll" {
		t.Errorf("path = %s", *path)
	}
	want := []int{3, 0, 5}
	if len(counts) != len(want) {
		t.Fatalf("counts = %v, want %v", counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("counts = %v, want %v", counts, want)
		}
	}
}

func TestSendMessageID_ReturnsID(t *testing.T) {
	s, _, _ := newTestService(t, `{"ok":true,"result":{"message_id":7}}`)
	id, err := s.SendMessageID("-1", "hi")
	if err != nil || id != 7 {
		t.Fatalf("got id=%d err=%v", id, err)
	}
}

func TestPinChatMessage_SendsIDs(t *testing.T) {
	s, path, body := newTestService(t, `{"ok":true,"result":true}`)
	if err := s.PinChatMessage("-1", 7); err != nil {
		t.Fatal(err)
	}
	if *path != "/bottok/pinChatMessage" {
		t.Errorf("path = %s", *path)
	}
	var req map[string]any
	_ = json.Unmarshal([]byte(*body), &req)
	if req["message_id"] != float64(7) || req["disable_notification"] != true {
		t.Errorf("unexpected body: %s", *body)
	}
}

func TestAPIError_IsReturned(t *testing.T) {
	s, _, _ := newTestService(t, `{"ok":false,"description":"Bad Request: nope"}`)
	if _, err := s.SendMessageID("-1", "hi"); err == nil {
		t.Error("expected API error")
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	got := EscapeMarkdownV2("a_b*c.d-e(f)")
	want := `a\_b\*c\.d\-e\(f\)`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
