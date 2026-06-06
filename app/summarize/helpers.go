package summarize

import (
	"time"
	"unicode/utf8"

	"svodka/business/telegram"
)

// stats accumulates per-run metrics for logging (counts only, never content).
type stats struct {
	total    int // source chats configured
	ok       int // resolved and fetched
	failed   int // skipped due to resolve/fetch errors
	messages int // messages collected across ok chats
	chars    int // total runes of message text collected
}

// windowStart returns the lower bound of the fetch window: hours before now.
func windowStart(now time.Time, hours int) time.Time {
	return now.Add(-time.Duration(hours) * time.Hour)
}

// chatChars counts the runes of all message texts in a chat (a cheap size proxy
// for metrics; authors and timestamps are excluded).
func chatChars(cm telegram.ChatMessages) int {
	n := 0
	for _, m := range cm.Messages {
		n += utf8.RuneCountInString(m.Text)
	}
	return n
}

// runeLen counts runes in s.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
