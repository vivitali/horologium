package meetup

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// IdeaModel is the fallback model for the AI option when the live lookup
// fails. Haiku is plenty for one short line and keeps the cost negligible.
const IdeaModel = "claude-haiku-4-5"

// ideaModelPrefix selects the family; latestModel picks the newest member.
const ideaModelPrefix = "claude-haiku"

// IdeaInput is everything the generator may consider.
type IdeaInput struct {
	Weekend   Weekend
	PastIdeas []string
	// Conditions is an optional free-text hint (e.g. a forecast). Empty today;
	// reserved so a forecast can be added without changing the prompt shape.
	Conditions string
}

// Idea is the AI poll option: the one-line option text (without the 🤖
// prefix) and a short details blurb shown in the summary if it wins.
type Idea struct {
	Text    string
	Details string
}

// MaxDetailsLen caps the details blurb so the summary stays short.
const MaxDetailsLen = 350

// IdeaGenerator produces the AI poll option.
type IdeaGenerator interface {
	Generate(ctx context.Context, in IdeaInput) (Idea, error)
}

// ClaudeIdeas generates ideas with the Anthropic API.
type ClaudeIdeas struct {
	apiKey string
	client anthropic.Client
}

// NewClaudeIdeas builds a generator. Extra options (base URL, retries) are
// for tests.
func NewClaudeIdeas(apiKey string, opts ...option.RequestOption) *ClaudeIdeas {
	all := append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(20 * time.Second),
	}, opts...)
	return &ClaudeIdeas{apiKey: apiKey, client: anthropic.NewClient(all...)}
}

const ideaSystemPrompt = `You help a small tech community in Winnipeg, Manitoba, Canada pick an idea for a weekend meetup.

Reply with exactly two parts, in English, no quotes, no emoji:
1. First line: the poll option. Format: "<day and approximate time> — <specific activity and place>". Example: "Sat ~2pm — pool at a bar on Osborne". At most 70 characters.
2. Then a blank line, then one short paragraph of details (at most 300 characters): the venue and area, how to get there (parking or transit), and cost or what to bring.

Rules for the option:
- Pick one of the three offered days and give an approximate start time.
- The activity must be real and available in or near Winnipeg: sports, walks, games, exhibitions, board-game cafes, museums, tech events, and so on.
- Take the season and likely weather into account so the idea fits, but NEVER mention the season, month, or weather in the text.
- Do not repeat anything from the list of past ideas.`

// Generate asks Claude for one idea. An empty option line is an error so
// the caller can drop the option.
func (c *ClaudeIdeas) Generate(ctx context.Context, in IdeaInput) (Idea, error) {
	if c.apiKey == "" {
		return Idea{}, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	w := in.Weekend
	var user strings.Builder
	fmt.Fprintf(&user, "Meetup dates: Friday %s, Saturday %s, Sunday %s (%d).\n",
		DateText(w.Fri), DateText(w.Sat), DateText(w.Sun), w.Sat.Year())
	fmt.Fprintf(&user, "Season: %s.\n", Season(w.Sat.Month()))
	if in.Conditions != "" {
		fmt.Fprintf(&user, "Conditions: %s\n", in.Conditions)
	}
	if len(in.PastIdeas) > 0 {
		user.WriteString("Past ideas (do not repeat):\n")
		for _, p := range in.PastIdeas {
			fmt.Fprintf(&user, "- %s\n", p)
		}
	}
	user.WriteString("Suggest one idea.")

	model := c.latestModel(ctx)
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 400,
		System:    []anthropic.TextBlockParam{{Text: ideaSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user.String())),
		},
	})
	if err != nil {
		return Idea{}, fmt.Errorf("anthropic: %w", err)
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	idea := parseIdea(text.String())
	if idea.Text == "" {
		return Idea{}, fmt.Errorf("anthropic: empty idea")
	}
	return idea, nil
}

// parseIdea splits the reply: first non-empty line is the option, the rest
// (joined, with an optional "Details:" label stripped) is the blurb.
func parseIdea(raw string) Idea {
	var idea Idea
	var rest []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idea.Text == "" {
			idea.Text = cleanIdea(line)
			continue
		}
		rest = append(rest, line)
	}
	details := strings.TrimSpace(strings.Join(rest, " "))
	details = strings.TrimSpace(strings.TrimPrefix(details, "Details:"))
	idea.Details = truncateRunes(details, MaxDetailsLen)
	return idea
}

// latestModel returns the newest model whose id starts with ideaModelPrefix,
// or IdeaModel if the Models API is unavailable. Runs once a month, so no
// caching.
func (c *ClaudeIdeas) latestModel(ctx context.Context) string {
	page, err := c.client.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		log.Printf("meetup: models list failed, using %s: %v", IdeaModel, err)
		return IdeaModel
	}
	best := ""
	var bestAt time.Time
	for _, m := range page.Data {
		if strings.HasPrefix(m.ID, ideaModelPrefix) && (best == "" || m.CreatedAt.After(bestAt)) {
			best, bestAt = m.ID, m.CreatedAt
		}
	}
	if best == "" {
		return IdeaModel
	}
	log.Printf("meetup: using %s", best)
	return best
}

// Season returns the season for a month.
func Season(m time.Month) string {
	switch m {
	case time.December, time.January, time.February:
		return "winter"
	case time.March, time.April, time.May:
		return "spring"
	case time.June, time.July, time.August:
		return "summer"
	default:
		return "fall"
	}
}

// cleanIdea keeps the first non-empty line, strips quotes and a stray
// robot prefix, and caps the length for the Telegram option limit.
func cleanIdea(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, `"'«»“”`)
		line = strings.TrimSpace(strings.TrimPrefix(line, ideaPrefix))
		if line != "" {
			return truncateRunes(line, MaxIdeaLen)
		}
	}
	return ""
}
