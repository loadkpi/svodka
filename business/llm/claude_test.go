package llm

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestExtractText(t *testing.T) {
	// The SDK's AsText() reads from the captured raw JSON, so build messages by
	// unmarshaling wire-shaped payloads rather than struct literals.
	msg := func(t *testing.T, contentJSON string) *anthropic.Message {
		t.Helper()
		var m anthropic.Message
		if err := json.Unmarshal([]byte(`{"content":`+contentJSON+`}`), &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return &m
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"empty content", `[]`, ""},
		{"single text", `[{"type":"text","text":"hello"}]`, "hello"},
		{
			"multiple text blocks concatenated",
			`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`,
			"ab",
		},
		{
			"thinking blocks ignored",
			`[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"answer"}]`,
			"answer",
		},
		{
			"only thinking yields empty",
			`[{"type":"thinking","thinking":"hmm"}]`,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractText(msg(t, tt.content)); got != tt.want {
				t.Errorf("extractText = %q, want %q", got, tt.want)
			}
		})
	}
}
