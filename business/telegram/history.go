package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/tg"
)

// Message is a normalized chat message ready for summarization. Time is UTC; the
// digest applies the configured timezone. Author is empty for channel posts
// (the sender is the channel itself, already named by ChatMessages.Title).
type Message struct {
	Author string
	Time   time.Time
	Text   string
}

// ChatMessages is the messages of one source chat within the time window, in
// chronological order (oldest first).
type ChatMessages struct {
	Title    string
	Messages []Message
}

// FetchWindow reads p's history back to since and returns the normalized,
// chronologically ordered messages. Telegram returns history newest-first, so
// the walk stops at the first message older than since.
func FetchWindow(ctx context.Context, api *tg.Client, p Peer, since time.Time) (ChatMessages, error) {
	cm := ChatMessages{Title: p.Title}
	cutoff := since.Unix()

	iter := query.Messages(api).GetHistory(p.Input).BatchSize(100).Iter()
	for iter.Next(ctx) {
		e := iter.Value()
		if int64(e.Msg.GetDate()) < cutoff {
			break
		}
		if m, ok := normalize(e.Msg, e.Entities); ok {
			cm.Messages = append(cm.Messages, m)
		}
	}
	if err := iter.Err(); err != nil {
		return ChatMessages{}, fmt.Errorf("fetch history for %q: %w", p.Title, err)
	}

	reverse(cm.Messages)
	return cm, nil
}

// normalize converts a history element into a Message. It returns ok=false for
// service messages and for entries that carry neither text nor a media marker —
// nothing worth summarizing. This is the pure core covered by unit tests (ADR-7).
func normalize(msg tg.NotEmptyMessage, ent peer.Entities) (Message, bool) {
	m, ok := msg.(*tg.Message)
	if !ok {
		return Message{}, false // service message (e.g. "user joined")
	}

	text := strings.TrimSpace(m.Message)
	if tag := mediaTag(m.Media); tag != "" {
		if text == "" {
			text = tag
		} else {
			text = tag + " " + text
		}
	}
	if text == "" {
		return Message{}, false
	}

	return Message{
		Author: author(m, ent),
		Time:   time.Unix(int64(m.Date), 0).UTC(),
		Text:   text,
	}, true
}

// author resolves the sender's display name from the message entities. A missing
// FromID means a channel post (the channel is the author) — left empty.
func author(m *tg.Message, ent peer.Entities) string {
	from, ok := m.GetFromID()
	if !ok {
		return ""
	}
	switch v := from.(type) {
	case *tg.PeerUser:
		if u, ok := ent.User(v.UserID); ok {
			return userTitle(u)
		}
	case *tg.PeerChannel:
		if c, ok := ent.Channel(v.ChannelID); ok {
			return c.Title
		}
	case *tg.PeerChat:
		if c, ok := ent.Chat(v.ChatID); ok {
			return c.Title
		}
	}
	return ""
}

// mediaTag returns a compact placeholder for a message's attachment, or "" when
// there is none. Web page previews yield no tag: the link already lives in text.
func mediaTag(media tg.MessageMediaClass) string {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		return "[photo]"
	case *tg.MessageMediaDocument:
		return documentTag(m)
	case *tg.MessageMediaPoll:
		return "[poll]"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return "[location]"
	case *tg.MessageMediaContact:
		return "[contact]"
	case *tg.MessageMediaGame:
		return "[game]"
	case *tg.MessageMediaInvoice:
		return "[invoice]"
	case *tg.MessageMediaDice:
		return "[dice]"
	case *tg.MessageMediaStory:
		return "[story]"
	case *tg.MessageMediaGiveaway, *tg.MessageMediaGiveawayResults:
		return "[giveaway]"
	case *tg.MessageMediaWebPage, *tg.MessageMediaEmpty, nil:
		return ""
	default:
		return "[media]"
	}
}

// documentTag distinguishes the common document sub-kinds. The voice/round flags
// live on the media wrapper; the rest is read from the document attributes.
func documentTag(m *tg.MessageMediaDocument) string {
	if m.Voice {
		return "[voice]"
	}
	if m.Round {
		return "[video]"
	}
	doc, ok := m.Document.(*tg.Document)
	if !ok {
		return "[file]"
	}
	var video, audio, sticker, animated bool
	for _, a := range doc.Attributes {
		switch a.(type) {
		case *tg.DocumentAttributeSticker:
			sticker = true
		case *tg.DocumentAttributeAnimated:
			animated = true
		case *tg.DocumentAttributeVideo:
			video = true
		case *tg.DocumentAttributeAudio:
			audio = true
		}
	}
	switch {
	case sticker:
		return "[sticker]"
	case animated:
		return "[gif]"
	case video:
		return "[video]"
	case audio:
		return "[audio]"
	default:
		return "[file]"
	}
}

// reverse flips s in place. History arrives newest-first; the digest wants
// chronological order.
func reverse(s []Message) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
