// Package llm wraps the summarization LLM behind a small interface so the rest
// of the app does not depend on a specific vendor or transport.
package llm

import "context"

// Provider turns a system+user prompt into summarized text.
type Provider interface {
	Summarize(ctx context.Context, in Input) (string, error)
}

// Input is a single summarization request. System is stable across calls within
// a run (it is cached); User carries the per-call content. Model and MaxTokens
// come from config.
type Input struct {
	System    string
	User      string
	Model     string
	MaxTokens int
}
