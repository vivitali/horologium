// Package telegram provides a minimal Telegram Bot API client used to send
// event digests, polls, pins, and alerts.
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Service is a thin HTTP client over the Telegram Bot API.
type Service struct {
	botToken string
	client   *http.Client
	baseURL  string
}

// NewService constructs a Service for the given bot token.
func NewService(botToken string) *Service {
	return &Service{
		botToken: botToken,
		client:   &http.Client{Timeout: 30 * time.Second},
		baseURL:  "https://api.telegram.org/bot" + botToken,
	}
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

type messageResult struct {
	MessageID int64 `json:"message_id"`
}

type pollResult struct {
	Options []struct {
		Text       string `json:"text"`
		VoterCount int    `json:"voter_count"`
	} `json:"options"`
}

// SendMessage sends a MarkdownV2-formatted message to chatID.
func (s *Service) SendMessage(chatID, message string) error {
	_, err := s.SendMessageID(chatID, message)
	return err
}

// SendMessageID sends a MarkdownV2-formatted message and returns its message_id.
func (s *Service) SendMessageID(chatID, message string) (int64, error) {
	if s.botToken == "" {
		return 0, fmt.Errorf("bot token not configured")
	}
	if chatID == "" {
		return 0, fmt.Errorf("chat ID not provided")
	}
	if message == "" {
		return 0, fmt.Errorf("message is empty")
	}
	if n := utf8.RuneCountInString(message); n > 4096 {
		return 0, fmt.Errorf("message too long (%d characters, max 4096)", n)
	}
	var res messageResult
	if err := s.post("/sendMessage", sendMessageRequest{
		ChatID:                chatID,
		Text:                  message,
		ParseMode:             "MarkdownV2",
		DisableWebPagePreview: true,
	}, &res); err != nil {
		return 0, err
	}
	return res.MessageID, nil
}

// SendAlert wraps the message in a standard alert template.
func (s *Service) SendAlert(chatID, alertMessage string) error {
	body := fmt.Sprintf("🚨 *Winnipeg Tech Events Alert*\n\n%s\n\n_Time: %s_",
		alertMessage, time.Now().Format("2006-01-02 15:04:05 MST"))
	return s.SendMessage(chatID, body)
}

// TestConnection calls /getMe to verify the bot token works.
func (s *Service) TestConnection() error {
	if s.botToken == "" {
		return fmt.Errorf("bot token not configured")
	}
	resp, err := s.client.Get(s.baseURL + "/getMe")
	if err != nil {
		return fmt.Errorf("failed to test connection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("connection test failed with status: %d", resp.StatusCode)
	}
	return nil
}

type sendPollRequest struct {
	ChatID                string   `json:"chat_id"`
	Question              string   `json:"question"`
	Options               []string `json:"options"`
	IsAnonymous           bool     `json:"is_anonymous"`
	Type                  string   `json:"type"`
	AllowsMultipleAnswers bool     `json:"allows_multiple_answers"`
}

// SendPoll sends a non-anonymous poll and returns its message_id.
// Telegram requires 2-10 options.
func (s *Service) SendPoll(chatID, question string, options []string, allowMultiple bool) (int64, error) {
	if s.botToken == "" {
		return 0, fmt.Errorf("bot token not configured")
	}
	if chatID == "" {
		return 0, fmt.Errorf("chat ID not provided")
	}
	if question == "" {
		return 0, fmt.Errorf("question is empty")
	}
	if len(options) < 2 || len(options) > 10 {
		return 0, fmt.Errorf("poll requires 2-10 options, got %d", len(options))
	}
	var res messageResult
	if err := s.post("/sendPoll", sendPollRequest{
		ChatID:                chatID,
		Question:              question,
		Options:               options,
		IsAnonymous:           false,
		Type:                  "regular",
		AllowsMultipleAnswers: allowMultiple,
	}, &res); err != nil {
		return 0, err
	}
	return res.MessageID, nil
}

type stopPollRequest struct {
	ChatID    string `json:"chat_id"`
	MessageID int64  `json:"message_id"`
}

// StopPoll closes the poll and returns voter counts per option, in the
// order the options were sent. Only works on polls this bot sent.
func (s *Service) StopPoll(chatID string, messageID int64) ([]int, error) {
	var res pollResult
	if err := s.post("/stopPoll", stopPollRequest{ChatID: chatID, MessageID: messageID}, &res); err != nil {
		return nil, err
	}
	counts := make([]int, len(res.Options))
	for i, o := range res.Options {
		counts[i] = o.VoterCount
	}
	return counts, nil
}

type pinRequest struct {
	ChatID              string `json:"chat_id"`
	MessageID           int64  `json:"message_id"`
	DisableNotification bool   `json:"disable_notification"`
}

// PinChatMessage pins a message. The bot must be an admin with pin rights.
func (s *Service) PinChatMessage(chatID string, messageID int64) error {
	return s.post("/pinChatMessage", pinRequest{ChatID: chatID, MessageID: messageID, DisableNotification: true}, nil)
}

// mdV2Escape escapes every character Telegram MarkdownV2 treats as special.
var mdV2Escape = strings.NewReplacer(
	`\`, `\\`, "_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
	"~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=",
	"|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
)

// EscapeMarkdownV2 makes arbitrary text safe inside a MarkdownV2 message.
func EscapeMarkdownV2(s string) string { return mdV2Escape.Replace(s) }

// post sends body as JSON and, when out is non-nil, decodes the "result"
// field into it.
func (s *Service) post(path string, body any, out any) error {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	resp, err := s.client.Post(s.baseURL+path, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var r apiResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if !r.OK {
		return fmt.Errorf("telegram API error: %s", r.Description)
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("parse result: %w", err)
		}
	}
	return nil
}
