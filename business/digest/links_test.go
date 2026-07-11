package digest

import (
	"context"
	"strings"
	"testing"
	"time"

	"svodka/business/llm"
	"svodka/business/telegram"
)

func TestSanitizeLinks(t *testing.T) {
	valid := map[string]bool{
		"https://t.me/c/123/456": true,
		"https://t.me/c/123/789": true,
	}

	tests := []struct {
		name        string
		text        string
		want        string
		wantRemoved int
	}{
		{
			name:        "valid link kept",
			text:        "- did a thing https://t.me/c/123/456\n",
			want:        "- did a thing https://t.me/c/123/456\n",
			wantRemoved: 0,
		},
		{
			name:        "wrong msg id removed",
			text:        "- did a thing https://t.me/c/123/999\n",
			want:        "- did a thing\n",
			wantRemoved: 1,
		},
		{
			name:        "wrong channel id removed",
			text:        "- did a thing https://t.me/c/999/456\n",
			want:        "- did a thing\n",
			wantRemoved: 1,
		},
		{
			name:        "multiple links only invalid one cut",
			text:        "- a https://t.me/c/123/456 and b https://t.me/c/123/999\n",
			want:        "- a https://t.me/c/123/456 and b\n",
			wantRemoved: 1,
		},
		{
			name:        "no links unchanged",
			text:        "- nothing to see here\n",
			want:        "- nothing to see here\n",
			wantRemoved: 0,
		},
		{
			name:        "trailing space and blank line cleaned",
			text:        "- a https://t.me/c/123/999\n- b https://t.me/c/123/456\n",
			want:        "- a\n- b https://t.me/c/123/456\n",
			wantRemoved: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, removed := sanitizeLinks(tt.text, valid)
			if got != tt.want {
				t.Errorf("sanitizeLinks text\n got: %q\nwant: %q", got, tt.want)
			}
			if removed != tt.wantRemoved {
				t.Errorf("sanitizeLinks removed = %d, want %d", removed, tt.wantRemoved)
			}
		})
	}
}

// TestSanitizeLinksPublicFormat covers the public https://t.me/<username>/<msg>
// backlink (M28): kept when valid, stripped when misquoted, and — the key
// regression this milestone guards against — never confused with the private
// c/ form on either side.
func TestSanitizeLinksPublicFormat(t *testing.T) {
	valid := map[string]bool{
		"https://t.me/netology/456": true,
		// A username that itself starts with "c" must still be read as a
		// username, not misparsed as the private form's literal "c/" segment.
		"https://t.me/cnews/10": true,
	}

	tests := []struct {
		name        string
		text        string
		want        string
		wantRemoved int
	}{
		{
			name:        "valid public link kept",
			text:        "- announced https://t.me/netology/456\n",
			want:        "- announced https://t.me/netology/456\n",
			wantRemoved: 0,
		},
		{
			name:        "wrong msg id in public link removed",
			text:        "- announced https://t.me/netology/999\n",
			want:        "- announced\n",
			wantRemoved: 1,
		},
		{
			name:        "username starting with c not confused with c/ form",
			text:        "- news https://t.me/cnews/10\n",
			want:        "- news https://t.me/cnews/10\n",
			wantRemoved: 0,
		},
		{
			name:        "private form for a public-only chat is rejected (format downgrade)",
			text:        "- announced https://t.me/c/123/456\n",
			want:        "- announced\n",
			wantRemoved: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, removed := sanitizeLinks(tt.text, valid)
			if got != tt.want {
				t.Errorf("sanitizeLinks text\n got: %q\nwant: %q", got, tt.want)
			}
			if removed != tt.wantRemoved {
				t.Errorf("sanitizeLinks removed = %d, want %d", removed, tt.wantRemoved)
			}
		})
	}
}

// TestSanitizeLinksPublicFormatDowngradeInverse mirrors the inverse direction:
// a chat that only ever had a private valid link must reject a
// public-looking substitute for the same message.
func TestSanitizeLinksPublicFormatDowngradeInverse(t *testing.T) {
	valid := map[string]bool{"https://t.me/c/123/456": true}
	got, removed := sanitizeLinks("- x https://t.me/somechan/456\n", valid)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if got != "- x\n" {
		t.Fatalf("got %q, want \"- x\\n\"", got)
	}
}

func TestValidLinks(t *testing.T) {
	chats := []telegram.ChatMessages{
		{Title: "Channel", ChannelID: 123, Messages: []telegram.Message{{ID: 1}, {ID: 2}}},
		{Title: "Public", ChannelID: 555, Username: "netology", Messages: []telegram.Message{{ID: 7}}},
		// A DM has no addressable backlink of its own, but a t.me post link
		// quoted inside a message is legitimate content and must be allowed.
		{Title: "DM", ChannelID: 0, Messages: []telegram.Message{
			{ID: 1, Text: "see https://t.me/other_channel/42 and https://t.me/c/999/5"},
		}},
	}
	got := validLinks(chats)
	want := map[string]bool{
		"https://t.me/c/123/1":          true,
		"https://t.me/c/123/2":          true,
		"https://t.me/netology/7":       true,
		"https://t.me/other_channel/42": true,
		"https://t.me/c/999/5":          true,
	}
	if len(got) != len(want) {
		t.Fatalf("validLinks = %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("validLinks missing %+v", k)
		}
	}
}

// TestBuildSanitizesInvalidBacklink is the end-to-end guard (M27): a provider
// reply that mixes a genuine backlink with a misquoted one should come out of
// Build with only the bad one stripped, and Usage.LinksRemoved reflecting it.
func TestBuildSanitizesInvalidBacklink(t *testing.T) {
	base := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	chats := []telegram.ChatMessages{
		{
			Title:     "Channel",
			ChannelID: 123,
			Messages: []telegram.Message{
				{Author: "", Text: "hello", Time: base, ID: 456},
			},
		},
	}
	const reply = "- good link https://t.me/c/123/456\n" +
		"- bad link (misquoted digit) https://t.me/c/123/999\n"
	fp := &fakeProvider{reply: func(llm.Input) (llm.Result, error) { return llm.Result{Text: reply}, nil }}
	opts := Options{OutputLang: "en", Location: time.UTC, Backlinks: true}

	out, usage, err := Build(context.Background(), fp, chats, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(out, "https://t.me/c/123/456") {
		t.Fatalf("valid backlink should survive: %q", out)
	}
	if strings.Contains(out, "https://t.me/c/123/999") {
		t.Fatalf("invalid backlink should be stripped: %q", out)
	}
	if usage.LinksRemoved != 1 {
		t.Fatalf("usage.LinksRemoved = %d, want 1", usage.LinksRemoved)
	}
}
