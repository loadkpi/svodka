package digest

import (
	"fmt"
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
// channel posts, which have no author). When backlinks is set, each line of a
// channel/supergroup ends with a bare t.me/c source link (M13, ADR-15).
func serializeChat(chat telegram.ChatMessages, loc *time.Location, backlinks bool) string {
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
		if backlinks {
			if link := linkFor(chat.ChannelID, chat.Username, m.ID); link != "" {
				b.WriteByte(' ')
				b.WriteString(link)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// serializeAll joins per-chat blocks with a blank line between them, producing
// the user text for a single-call digest.
func serializeAll(chats []telegram.ChatMessages, loc *time.Location, backlinks bool) string {
	parts := make([]string, 0, len(chats))
	for _, c := range chats {
		parts = append(parts, serializeChat(c, loc, backlinks))
	}
	return strings.Join(parts, "\n")
}

// linkFor builds the message backlink, or "" when the chat is not a
// channel/supergroup (channelID 0) or the id is missing. Telegram auto-links the
// bare URL. When the chat has a public username, the link is the public
// https://t.me/<username>/<msg> form, open to anyone (M28); otherwise it falls
// back to the private https://t.me/c/<channelID>/<msg> form, which requires
// chat membership to open (M13, ADR-15). A username alone is not enough: user
// chats and basic groups (channelID == 0) have no addressable per-message link
// regardless of whether they carry a username.
func linkFor(channelID int64, username string, msgID int) string {
	if channelID <= 0 || msgID <= 0 {
		return ""
	}
	if username != "" {
		return fmt.Sprintf("https://t.me/%s/%d", username, msgID)
	}
	return fmt.Sprintf("https://t.me/c/%d/%d", channelID, msgID)
}
