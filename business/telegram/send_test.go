package telegram

import (
	"strings"
	"testing"
)

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  []string
	}{
		{"empty", "", 10, nil},
		{"blank trimmed to empty", "\n\n", 10, nil},
		{"under limit", "hello", 10, []string{"hello"}},
		{"exactly limit", "hello", 5, []string{"hello"}},
		{
			"split on newline",
			"line one\nline two\nline three",
			12,
			[]string{"line one", "line two", "line three"},
		},
		{
			"hard cut when no newline",
			"abcdefghij",
			4,
			[]string{"abcd", "efgh", "ij"},
		},
		{
			"prefers last newline within limit",
			"aa\nbb\ncccccc",
			6,
			[]string{"aa\nbb", "cccccc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitMessage(tt.text, tt.limit)
			if !equalParts(got, tt.want) {
				t.Fatalf("splitMessage(%q, %d)\n got: %#v\nwant: %#v", tt.text, tt.limit, got, tt.want)
			}
			for i, p := range got {
				if n := len([]rune(p)); n > tt.limit {
					t.Fatalf("part %d exceeds limit %d: %q (%d runes)", i, tt.limit, p, n)
				}
			}
		})
	}
}

func TestSplitMessageMultibyte(t *testing.T) {
	// Cyrillic: parts must be split by runes, not bytes.
	text := strings.Repeat("я", 10)
	parts := splitMessage(text, 4)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %#v", len(parts), parts)
	}
	if got := strings.Join(parts, ""); got != text {
		t.Fatalf("reassembled mismatch: %q", got)
	}
}

func TestIsSelf(t *testing.T) {
	for _, s := range []string{"me", "ME", " me ", "Me"} {
		if !isSelf(s) {
			t.Errorf("isSelf(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"@me", "meme", "@group", ""} {
		if isSelf(s) {
			t.Errorf("isSelf(%q) = true, want false", s)
		}
	}
}

func equalParts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
