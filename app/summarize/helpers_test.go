package summarize

import (
	"testing"
	"time"

	"svodka/business/telegram"
)

func TestWindowStart(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		hours int
		want  time.Time
	}{
		{24, time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)},
		{2, time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)},
		{0, now},
	}
	for _, tt := range tests {
		if got := windowStart(now, tt.hours); !got.Equal(tt.want) {
			t.Errorf("windowStart(now, %d) = %v, want %v", tt.hours, got, tt.want)
		}
	}
}

func TestChatChars(t *testing.T) {
	cm := telegram.ChatMessages{
		Title: "ignored",
		Messages: []telegram.Message{
			{Author: "long author name", Text: "hi"}, // author excluded
			{Text: "привет"},                         // 6 runes, 12 bytes
		},
	}
	if got, want := chatChars(cm), 2+6; got != want {
		t.Fatalf("chatChars = %d, want %d", got, want)
	}
}

func TestChatCharsEmpty(t *testing.T) {
	if got := chatChars(telegram.ChatMessages{Title: "x"}); got != 0 {
		t.Fatalf("chatChars(empty) = %d, want 0", got)
	}
}
