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
	sb.WriteString(esc(Tag))
	sb.WriteString("\n")
	if ok {
		label := fmt.Sprintf("option #%d", winner+1)
		if winner < len(options) {
			label = options[winner]
		}
		sb.WriteString("📌 *It's settled:* ")
		sb.WriteString(esc(label))
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%d %s. See you there!", countAt(counts, winner), votesWord(countAt(counts, winner)))))
	} else {
		maxVotes := 0
		for i := 0; i < len(counts)-1; i++ {
			if counts[i] > maxVotes {
				maxVotes = counts[i]
			}
		}
		sb.WriteString(esc(fmt.Sprintf("No meetup this time: the top option got %d %s, and it takes at least %d. Let's try again next month 🙂", maxVotes, votesWord(maxVotes), MinVotes)))
	}
	sb.WriteString("\n\n")
	sb.WriteString(esc("How people voted:"))
	for i, o := range options {
		sb.WriteString("\n")
		sb.WriteString(esc(fmt.Sprintf("%s — %d", o, countAt(counts, i))))
	}
	return sb.String()
}

// votesWord pluralises "vote".
func votesWord(n int) string {
	if n == 1 {
		return "vote"
	}
	return "votes"
}

func countAt(counts []int, i int) int {
	if i < len(counts) {
		return counts[i]
	}
	return 0
}
