package main

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeModel(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"vendor slash model", "openai/gpt-5", "openai_gpt-5"},
		{"no slash unchanged", "local-model", "local-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeModel(tt.in); got != tt.want {
				t.Errorf("sanitizeModel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitModels(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"simple csv", "a,b,c", []string{"a", "b", "c"}},
		{"trims spaces", " a , b ", []string{"a", "b"}},
		{"drops blanks", "a,,b,", []string{"a", "b"}},
		{"single", "a", []string{"a"}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitModels(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitModels(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitModels(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestFormatSummary checks the markdown table shape: a header row, one row
// per model, successful models showing real numbers and failed models
// showing "error" in every metric cell.
func TestFormatSummary(t *testing.T) {
	results := []modelResult{
		{Model: "openai/gpt-5", TokensIn: 100, TokensOut: 50, DurationMs: 1234, DigestChars: 500, Truncated: false},
		{Model: "broken/model", Err: errors.New("boom")},
	}
	summary := formatSummary(results)

	if !strings.Contains(summary, "| model | tokens_in | tokens_out | duration_ms | digest_chars | truncated |") {
		t.Errorf("missing header row:\n%s", summary)
	}
	if !strings.Contains(summary, "| openai/gpt-5 | 100 | 50 | 1234 | 500 | false |") {
		t.Errorf("missing successful model row:\n%s", summary)
	}
	if !strings.Contains(summary, "| broken/model | error | error | error | error | error |") {
		t.Errorf("missing failed model row:\n%s", summary)
	}
}
