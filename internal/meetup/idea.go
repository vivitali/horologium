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

// IdeaGenerator produces the "🤖" poll option text (without the prefix).
type IdeaGenerator interface {
	Generate(ctx context.Context, in IdeaInput) (string, error)
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

const ideaSystemPrompt = `Ти допомагаєш невеликій українськомовній tech-спільноті у Вінніпезі (Winnipeg, Manitoba, Canada) обрати ідею для зустрічі на вихідних.

Правила відповіді:
- Рівно один рядок, українською, без пояснень, без лапок, без емодзі.
- Формат: "<день і приблизний час> — <конкретна активність і місце>". Наприклад: "Субота 14:00 — більярд на Osborne".
- Не більше 70 символів.
- Обери один із трьох запропонованих днів і вкажи приблизний час початку.
- Активність має бути реальною і доступною у Вінніпезі або поруч: спорт, прогулянки, ігри, виставки, кафе з настільними іграми, музеї, tech-події тощо.
- Враховуй пору року і погодні умови, щоб ідея була доречною, але НІКОЛИ не згадуй пору року, місяць чи погоду в тексті.
- Не повторюй ідеї зі списку минулих ідей.`

// Generate asks Claude for one idea line. Empty output is an error so the
// caller can drop the option.
func (c *ClaudeIdeas) Generate(ctx context.Context, in IdeaInput) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	w := in.Weekend
	var user strings.Builder
	fmt.Fprintf(&user, "Дати зустрічі: пʼятниця %s, субота %s, неділя %s (%d рік).\n",
		UkrDate(w.Fri), UkrDate(w.Sat), UkrDate(w.Sun), w.Sat.Year())
	fmt.Fprintf(&user, "Пора року: %s.\n", Season(w.Sat.Month()))
	if in.Conditions != "" {
		fmt.Fprintf(&user, "Умови: %s\n", in.Conditions)
	}
	if len(in.PastIdeas) > 0 {
		user.WriteString("Минулі ідеї (не повторювати):\n")
		for _, p := range in.PastIdeas {
			fmt.Fprintf(&user, "- %s\n", p)
		}
	}
	user.WriteString("Запропонуй одну ідею.")

	model := c.latestModel(ctx)
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 200,
		System:    []anthropic.TextBlockParam{{Text: ideaSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user.String())),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	idea := cleanIdea(text.String())
	if idea == "" {
		return "", fmt.Errorf("anthropic: empty idea")
	}
	return idea, nil
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

// Season returns the Ukrainian season word for a month.
func Season(m time.Month) string {
	switch m {
	case time.December, time.January, time.February:
		return "зима"
	case time.March, time.April, time.May:
		return "весна"
	case time.June, time.July, time.August:
		return "літо"
	default:
		return "осінь"
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
