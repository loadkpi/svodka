package telegram

import (
	"errors"
	"strings"
	"testing"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/dialogs"
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

// TestErrorsDoNotLeakRef pins the NFR-2 invariant across pure error
// constructors reachable without a network call: an error crossing the
// business->app boundary must not repeat the input ref/id, since the caller
// already has chat_index for context (M23).
func TestErrorsDoNotLeakRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		err  error
	}{
		{"invite link plus", "+AbCdEf", ErrInviteLink},
		{"invite link joinchat", "https://t.me/joinchat/AbCdEf", ErrInviteLink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := classifyInput(tt.ref)
			if err == nil {
				t.Fatalf("classifyInput(%q): expected error", tt.ref)
			}
			if strings.Contains(err.Error(), tt.ref) {
				t.Fatalf("classifyInput(%q) error leaks ref: %q", tt.ref, err.Error())
			}
		})
	}

	// errChatIDNotFound is a static sentinel (no id interpolated); pin that it
	// stays that way and does not mention the id it was constructed for.
	const id = "1234567890"
	if strings.Contains(errChatIDNotFound.Error(), id) {
		t.Fatalf("errChatIDNotFound leaks id: %q", errChatIDNotFound.Error())
	}
}

// TestDialogUsername covers the numeric-id resolve path (ensureDialogs):
// username extraction from the dialog entities (M28). The manager-resolve
// path (peers.Peer.Username()) is a thin gotd call and not unit-tested per
// ADR-7.
func TestDialogUsername(t *testing.T) {
	// GetUsername reports ok only when the wire "has username" flag bit is
	// set (bit 6/3 for Channel/User), so tests build via SetUsername rather
	// than a struct literal, matching how a real decoded response looks.
	channelWithUsername := &tg.Channel{ID: 100}
	channelWithUsername.SetUsername("netology")
	userWithUsername := &tg.User{ID: 7}
	userWithUsername.SetUsername("durov")

	tests := []struct {
		name string
		peer tg.InputPeerClass
		ent  peer.Entities
		want string
	}{
		{
			name: "channel with username",
			peer: &tg.InputPeerChannel{ChannelID: 100},
			ent:  peer.NewEntities(nil, nil, map[int64]*tg.Channel{100: channelWithUsername}),
			want: "netology",
		},
		{
			name: "channel without username",
			peer: &tg.InputPeerChannel{ChannelID: 100},
			ent:  peer.NewEntities(nil, nil, map[int64]*tg.Channel{100: {ID: 100}}),
			want: "",
		},
		{
			name: "user with username",
			peer: &tg.InputPeerUser{UserID: 7},
			ent:  peer.NewEntities(map[int64]*tg.User{7: userWithUsername}, nil, nil),
			want: "durov",
		},
		{
			name: "user without username",
			peer: &tg.InputPeerUser{UserID: 7},
			ent:  peer.NewEntities(map[int64]*tg.User{7: {ID: 7, FirstName: "Alice"}}, nil, nil),
			want: "",
		},
		{
			name: "basic group never has a username",
			peer: &tg.InputPeerChat{ChatID: 5},
			ent:  peer.NewEntities(nil, map[int64]*tg.Chat{5: {ID: 5, Title: "Group"}}, nil),
			want: "",
		},
		{
			name: "entity missing from the batch",
			peer: &tg.InputPeerChannel{ChannelID: 999},
			ent:  peer.NewEntities(nil, nil, nil),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := dialogs.Elem{Peer: tt.peer, Entities: tt.ent}
			if got := dialogUsername(e); got != tt.want {
				t.Errorf("dialogUsername = %q, want %q", got, tt.want)
			}
		})
	}
}

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
