// Package llm wraps the summarization LLM behind a small interface so the rest
// of the app does not depend on a specific vendor or transport.
package llm

import "context"

// Provider turns a system+user prompt into a summarization Result.
type Provider interface {
	Summarize(ctx context.Context, in Input) (Result, error)
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

// Result is one summarization response plus accounting the caller logs (token
// counts for cost, Truncated so a clipped output is not mistaken for a complete
// one). It never carries message content beyond Text (NFR-2).
type Result struct {
	Text         string
	InputTokens  int
	OutputTokens int
	Truncated    bool // output stopped at MaxTokens, so Text is likely incomplete
}
