package llm

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestToResult(t *testing.T) {
	completion := func(t *testing.T, body string) *openai.ChatCompletion {
		t.Helper()
		var c openai.ChatCompletion
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return &c
	}

	t.Run("maps text, usage and finish reason", func(t *testing.T) {
		resp := completion(t, `{
			"choices": [{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],
			"usage": {"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`)
		got, err := toResult(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := Result{Text: "hello", InputTokens: 10, OutputTokens: 5, Truncated: false}
		if got != want {
			t.Errorf("toResult() = %+v, want %+v", got, want)
		}
	})

	t.Run("finish_reason length sets Truncated", func(t *testing.T) {
		resp := completion(t, `{
			"choices": [{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"cut off"}}],
			"usage": {"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`)
		got, err := toResult(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Truncated {
			t.Errorf("Truncated = false, want true")
		}
	})

	t.Run("empty content is an error", func(t *testing.T) {
		resp := completion(t, `{
			"choices": [{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":""}}],
			"usage": {"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}
		}`)
		if _, err := toResult(resp); err == nil {
			t.Error("want error for empty content, got nil")
		}
	})

	t.Run("no choices is an error", func(t *testing.T) {
		resp := completion(t, `{"choices": [], "usage": {"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`)
		if _, err := toResult(resp); err == nil {
			t.Error("want error for no choices, got nil")
		}
	})
}
