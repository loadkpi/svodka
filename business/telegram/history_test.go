package telegram

import (
	"testing"
	"time"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

func TestMediaTag(t *testing.T) {
	doc := func(attrs ...tg.DocumentAttributeClass) *tg.MessageMediaDocument {
		return &tg.MessageMediaDocument{Document: &tg.Document{Attributes: attrs}}
	}
	tests := []struct {
		name  string
		media tg.MessageMediaClass
		want  string
	}{
		{"nil", nil, ""},
		{"empty", &tg.MessageMediaEmpty{}, ""},
		{"webpage suppressed", &tg.MessageMediaWebPage{}, ""},
		{"photo", &tg.MessageMediaPhoto{}, "[photo]"},
		{"poll", &tg.MessageMediaPoll{}, "[poll]"},
		{"geo", &tg.MessageMediaGeo{}, "[location]"},
		{"venue", &tg.MessageMediaVenue{}, "[location]"},
		{"contact", &tg.MessageMediaContact{}, "[contact]"},
		{"dice", &tg.MessageMediaDice{}, "[dice]"},
		{"unsupported kind", &tg.MessageMediaUnsupported{}, "[media]"},
		{"voice flag", &tg.MessageMediaDocument{Voice: true}, "[voice]"},
		{"round flag", &tg.MessageMediaDocument{Round: true}, "[video]"},
		{"sticker attr", doc(&tg.DocumentAttributeSticker{}), "[sticker]"},
		{"animated attr", doc(&tg.DocumentAttributeAnimated{}), "[gif]"},
		{"video attr", doc(&tg.DocumentAttributeVideo{}), "[video]"},
		{"audio attr", doc(&tg.DocumentAttributeAudio{}), "[audio]"},
		{"plain document", doc(&tg.DocumentAttributeFilename{}), "[file]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mediaTag(tt.media); got != tt.want {
				t.Errorf("mediaTag = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	const ts = 1_700_000_000 // arbitrary unix time
	ent := peer.NewEntities(
		map[int64]*tg.User{7: {ID: 7, FirstName: "Alice"}},
		nil, nil,
	)

	userMsg := func(text string, media tg.MessageMediaClass) *tg.Message {
		m := &tg.Message{ID: 99, Date: ts, Message: text}
		m.SetFromID(&tg.PeerUser{UserID: 7})
		if media != nil {
			m.SetMedia(media)
		}
		return m
	}

	t.Run("service message skipped", func(t *testing.T) {
		if _, ok := normalize(&tg.MessageService{}, ent); ok {
			t.Fatal("service message should be skipped")
		}
	})

	t.Run("empty message skipped", func(t *testing.T) {
		if _, ok := normalize(userMsg("   ", nil), ent); ok {
			t.Fatal("text-less, media-less message should be skipped")
		}
	})

	t.Run("text only", func(t *testing.T) {
		got, ok := normalize(userMsg("  hello  ", nil), ent)
		if !ok {
			t.Fatal("want ok")
		}
		if got.Text != "hello" {
			t.Errorf("text = %q, want %q", got.Text, "hello")
		}
		if got.Author != "Alice" {
			t.Errorf("author = %q, want Alice", got.Author)
		}
		if got.ID != 99 {
			t.Errorf("id = %d, want 99", got.ID)
		}
		if !got.Time.Equal(time.Unix(ts, 0).UTC()) {
			t.Errorf("time = %v, want %v", got.Time, time.Unix(ts, 0).UTC())
		}
		if got.Time.Location() != time.UTC {
			t.Errorf("time location = %v, want UTC", got.Time.Location())
		}
	})

	t.Run("media only gets placeholder", func(t *testing.T) {
		got, ok := normalize(userMsg("", &tg.MessageMediaPhoto{}), ent)
		if !ok || got.Text != "[photo]" {
			t.Errorf("text = %q (ok=%v), want [photo]", got.Text, ok)
		}
	})

	t.Run("media plus caption", func(t *testing.T) {
		got, ok := normalize(userMsg("look", &tg.MessageMediaPhoto{}), ent)
		if !ok || got.Text != "[photo] look" {
			t.Errorf("text = %q, want [photo] look", got.Text)
		}
	})

	t.Run("channel post has empty author", func(t *testing.T) {
		m := &tg.Message{Date: ts, Message: "post"} // no FromID set
		got, ok := normalize(m, ent)
		if !ok {
			t.Fatal("want ok")
		}
		if got.Author != "" {
			t.Errorf("author = %q, want empty", got.Author)
		}
	})
}

func TestReverse(t *testing.T) {
	s := []Message{{Text: "a"}, {Text: "b"}, {Text: "c"}}
	reverse(s)
	if s[0].Text != "c" || s[1].Text != "b" || s[2].Text != "a" {
		t.Errorf("reverse = %v", s)
	}
}
