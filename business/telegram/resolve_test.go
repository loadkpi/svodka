package telegram

import (
	"errors"
	"testing"

	"github.com/gotd/td/tg"
)

func TestClassifyInput(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantKind inputKind
		wantVal  string
		wantErr  error
	}{
		{"plain username", "durov", kindUsername, "durov", nil},
		{"at username", "@durov", kindUsername, "durov", nil},
		{"trim spaces", "  @durov  ", kindUsername, "durov", nil},
		{"https t.me", "https://t.me/durov", kindUsername, "durov", nil},
		{"bare t.me", "t.me/durov", kindUsername, "durov", nil},
		{"http telegram.me", "http://telegram.me/durov", kindUsername, "durov", nil},
		{"t.me with trailing slash", "t.me/durov/", kindUsername, "durov", nil},
		{"uppercase host", "HTTPS://T.ME/Durov", kindUsername, "Durov", nil},
		{"positive numeric", "1234567890", kindNumeric, "1234567890", nil},
		{"negative numeric", "-1001234567890", kindNumeric, "-1001234567890", nil},
		{"at-stripped numeric stays username-free", "@-100", kindNumeric, "-100", nil},
		{"invite plus", "t.me/+AbCdEf", 0, "", ErrInviteLink},
		{"bare invite plus", "+AbCdEf", 0, "", ErrInviteLink},
		{"invite joinchat", "https://t.me/joinchat/AbCdEf", 0, "", ErrInviteLink},
		{"empty", "", 0, "", errEmpty},
		{"spaces only", "   ", 0, "", errEmpty},
		{"at only", "@", 0, "", errEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, val, err := classifyInput(tt.in)
			switch {
			case tt.wantErr == nil && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr == ErrInviteLink && !errors.Is(err, ErrInviteLink):
				t.Fatalf("want ErrInviteLink, got %v", err)
			case tt.wantErr == errEmpty && err == nil:
				t.Fatalf("want empty-reference error, got nil")
			}
			if tt.wantErr != nil {
				return
			}
			if kind != tt.wantKind {
				t.Errorf("kind = %d, want %d", kind, tt.wantKind)
			}
			if val != tt.wantVal {
				t.Errorf("value = %q, want %q", val, tt.wantVal)
			}
		})
	}
}

// errEmpty is a sentinel used only by the test to flag the "empty reference"
// cases; classifyInput returns a fresh error there, so we just assert non-nil.
var errEmpty = errors.New("empty")

func TestTDLibID(t *testing.T) {
	tests := []struct {
		name string
		peer tg.InputPeerClass
		want int64
		ok   bool
	}{
		{"user", &tg.InputPeerUser{UserID: 42}, 42, true},
		{"chat", &tg.InputPeerChat{ChatID: 42}, -42, true},
		{"channel", &tg.InputPeerChannel{ChannelID: 1234567890}, -1001234567890, true},
		{"self skipped", &tg.InputPeerSelf{}, 0, false},
		{"empty skipped", &tg.InputPeerEmpty{}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tdlibID(tt.peer)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("id = %d, want %d", got, tt.want)
			}
		})
	}
}
