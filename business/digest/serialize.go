package digest

import (
	"strings"
	"time"

	"svodka/business/telegram"
)

// titleMarker prefixes a chat title in the serialized text so the model can tell
// chat boundaries apart from message lines. It is an input convention only; the
// model is told to emit a plain title line (no markdown) in its output.
const titleMarker = "## "

// timeLayout renders a message timestamp as a wall-clock time in the configured
// timezone, e.g. "14:32". Absolute (not relative) so the text is stable
// regardless of when the digest is generated — important for prompt caching.
const timeLayout = "15:04"

// serializeChat renders one chat as a compact block: a title marker line
// followed by one "[HH:MM] Author: text" line per message (author omitted for
// channel posts, which have no author).
func serializeChat(chat telegram.ChatMessages, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(titleMarker)
	b.WriteString(chat.Title)
	b.WriteByte('\n')
	for _, m := range chat.Messages {
		b.WriteByte('[')
		b.WriteString(m.Time.In(loc).Format(timeLayout))
		b.WriteString("] ")
		if m.Author != "" {
			b.WriteString(m.Author)
			b.WriteString(": ")
		}
		b.WriteString(m.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// serializeAll joins per-chat blocks with a blank line between them, producing
// the user text for a single-call digest.
func serializeAll(chats []telegram.ChatMessages, loc *time.Location) string {
	parts := make([]string, 0, len(chats))
	for _, c := range chats {
		parts = append(parts, serializeChat(c, loc))
	}
	return strings.Join(parts, "\n")
}
