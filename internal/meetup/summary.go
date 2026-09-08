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
func FormatSummary(options []string, counts []int, winner int, ok bool) string {
	esc := telegram.EscapeMarkdownV2
	var sb strings.Builder
	if ok {
		label := fmt.Sprintf("варіант #%d", winner+1)
		if winner < len(options) {
			label = options[winner]
		}
		sb.WriteString("📌 *Зустріч цього місяця:* ")
		sb.WriteString(esc(label))
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("Голосів: %d", countAt(counts, winner))))
	} else {
		maxVotes := 0
		for i := 0; i < len(counts)-1; i++ {
			if counts[i] > maxVotes {
				maxVotes = counts[i]
			}
		}
		sb.WriteString(esc(fmt.Sprintf("😴 Цього місяця пропускаємо — замало голосів (максимум %d, потрібно %d).", maxVotes, MinVotes)))
	}
	sb.WriteString("\n\n")
	sb.WriteString(esc("Усі варіанти:"))
	for i, o := range options {
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%s — %d", o, countAt(counts, i))))
	}
	return sb.String()
}

func countAt(counts []int, i int) int {
	if i < len(counts) {
		return counts[i]
	}
	return 0
}
