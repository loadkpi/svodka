package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Claude is a Provider backed by the Anthropic Messages API via the official
// SDK (retries on 429/5xx are handled inside the SDK).
type Claude struct {
	client anthropic.Client
}

// NewClaude builds a Claude provider with the given API key.
func NewClaude(apiKey string) *Claude {
	return &Claude{client: anthropic.NewClient(option.WithAPIKey(apiKey))}
}

// Summarize sends one request and returns the concatenated text output plus
// token usage and whether the output was cut off at MaxTokens.
func (c *Claude) Summarize(ctx context.Context, in Input) (Result, error) {
	params := anthropic.MessageNewParams{
		Model:     in.Model,
		MaxTokens: int64(in.MaxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(in.User)),
		},
	}
	if in.System != "" {
		// cache_control pays off across the multiple map-reduce calls in one
		// run that share this system prompt (no effect on a single call).
		params.System = []anthropic.TextBlockParam{{
			Text:         in.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return Result{}, fmt.Errorf("claude messages: %w", err)
	}

	text := extractText(resp)
	if text == "" {
		return Result{}, errors.New("claude returned no text content")
	}
	return Result{
		Text:         text,
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
		Truncated:    resp.StopReason == anthropic.StopReasonMaxTokens,
	}, nil
}

// extractText concatenates the text blocks of a response, ignoring any
// non-text blocks (e.g. thinking). Pure helper, unit-tested.
func extractText(resp *anthropic.Message) string {
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
