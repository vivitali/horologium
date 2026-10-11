package meetup

import (
	"fmt"
	"strings"

	"event_calendar/pkg/telegram"
)

// MinVotes is the smallest winning count that confirms a meetup.
const MinVotes = 4

// PickWinner returns the index of the option with the most votes, ignoring
// the last option (cannot attend). Ties go to the earliest option. ok is
// false when there is no eligible option or the best count is below MinVotes.
func PickWinner(counts []int) (int, bool) {
	if len(counts) < 2 {
		return 0, false
	}
	best := 0
	for i := 1; i < len(counts)-1; i++ {
		if counts[i] > counts[best] {
			best = i
		}
	}
	return best, counts[best] >= MinVotes
}

// FormatSummary renders the MarkdownV2 message announcing the decision.
// ideaDetails is appended only when the winning option is the 🤖 idea.
func FormatSummary(options []string, counts []int, winner int, ok bool, ideaDetails string) string {
	esc := telegram.EscapeMarkdownV2
	var sb strings.Builder
	sb.WriteString(esc(Tag))
	sb.WriteString("\n")
	switch {
	case ok && winner < len(options) && strings.HasPrefix(options[winner], ideaPrefix):
		// The AI option is a conversation starter, not a decision: hand it
		// to the chat with the details and let people settle it.
		n := countAt(counts, winner)
		sb.WriteString("💡 *Пропозиція на обговорення:* ")
		sb.WriteString(esc(options[winner]))
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%d %s. Обговоріть у чаті та домовтесь про деталі.", n, votesWord(n))))
		if ideaDetails != "" {
			sb.WriteString("\nℹ️ ")
			sb.WriteString(esc(ideaDetails))
		}
	case ok:
		label := fmt.Sprintf("варіант #%d", winner+1)
		if winner < len(options) {
			label = options[winner]
		}
		n := countAt(counts, winner)
		sb.WriteString("📌 *Домовились:* ")
		sb.WriteString(esc(label))
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%d %s — до зустрічі!", n, votesWord(n))))
	default:
		maxVotes := 0
		for i := 0; i < len(counts)-1; i++ {
			if counts[i] > maxVotes {
				maxVotes = counts[i]
			}
		}
		sb.WriteString(esc(fmt.Sprintf("Цього разу не збираємось: найбільше %d %s, а треба хоча б %d. Спробуємо наступного місяця 🙂", maxVotes, votesWord(maxVotes), MinVotes)))
	}
	sb.WriteString("\n\n")
	sb.WriteString(esc("Як голосували:"))
	for i, o := range options {
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%s — %d", o, countAt(counts, i))))
	}
	return sb.String()
}

// votesWord declines "голос" for n: 1 голос, 2–4 голоси, 5+ голосів.
func votesWord(n int) string {
	n10, n100 := n%10, n%100
	switch {
	case n10 == 1 && n100 != 11:
		return "голос"
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		return "голоси"
	default:
		return "голосів"
	}
}

func countAt(counts []int, i int) int {
	if i < len(counts) {
		return counts[i]
	}
	return 0
}
