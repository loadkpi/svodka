package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
)

// maxMessageRunes is Telegram's per-message text limit. The true limit is 4096
// UTF-16 code units; counting runes is a safe approximation for the text-heavy
// digest (a rune never costs less than a UTF-16 unit except for astral emoji,
// which are vanishingly rare in prose), and Telegram has slack at the boundary.
const maxMessageRunes = 4096

// Send posts text to target. "me" goes to Saved Messages; anything else is
// resolved through r (sharing the run's resolver cache). Messages longer than
// Telegram's limit are split into several parts on line boundaries.
func Send(ctx context.Context, api *tg.Client, r *Resolver, target, text string) error {
	sender := message.NewSender(api)

	to := sender.Self()
	if !isSelf(target) {
		p, err := r.Resolve(ctx, target)
		if err != nil {
			return fmt.Errorf("resolve target %q: %w", target, err)
		}
		to = sender.To(p.Input)
	}

	for _, part := range splitMessage(text, maxMessageRunes) {
		if _, err := to.Text(ctx, part); err != nil {
			return fmt.Errorf("send message: %w", err)
		}
	}
	return nil
}

// isSelf reports whether target means the account's own Saved Messages.
func isSelf(target string) bool {
	return strings.EqualFold(strings.TrimSpace(target), "me")
}

// splitMessage breaks text into chunks of at most limit runes, preferring to cut
// on the last newline within each chunk so digest sections stay intact. A single
// line longer than the limit is hard-cut. Pure, unit-tested (ADR-7).
func splitMessage(text string, limit int) []string {
	runes := []rune(strings.TrimRight(text, "\n"))
	if len(runes) == 0 {
		return nil
	}

	var parts []string
	for len(runes) > limit {
		cut := limit
		if idx := lastNewline(runes[:limit]); idx > 0 {
			cut = idx
		}
		parts = append(parts, strings.TrimRight(string(runes[:cut]), "\n"))

		runes = runes[cut:]
		for len(runes) > 0 && runes[0] == '\n' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		parts = append(parts, string(runes))
	}
	return parts
}

// lastNewline returns the index of the last '\n' in s, or -1 if there is none.
func lastNewline(s []rune) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}
