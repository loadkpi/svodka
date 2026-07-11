package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// openRouterBaseURL is OpenRouter's OpenAI-compatible endpoint (ADR-20).
const openRouterBaseURL = "https://openrouter.ai/api/v1"

// OpenRouter is a Provider backed by OpenRouter's OpenAI-compatible chat
// completions API via the official openai-go SDK (retries on 429/5xx are
// handled inside the SDK, same as claude.go).
type OpenRouter struct {
	client openai.Client
}

// NewOpenRouter builds an OpenRouter provider with the given API key.
func NewOpenRouter(apiKey string) *OpenRouter {
	return &OpenRouter{client: openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(openRouterBaseURL),
	)}
}

// Summarize sends one chat completion request and returns the response text
// plus token usage and whether the output was cut off at MaxTokens. No
// cache_control is sent: prompt caching through OpenRouter is out of scope in
// v1 (ADR-20).
func (o *OpenRouter) Summarize(ctx context.Context, in Input) (Result, error) {
	var messages []openai.ChatCompletionMessageParamUnion
	if in.System != "" {
		messages = append(messages, openai.SystemMessage(in.System))
	}
	messages = append(messages, openai.UserMessage(in.User))

	params := openai.ChatCompletionNewParams{
		Model:    in.Model,
		Messages: messages,
		// OpenRouter (and most non-OpenAI backends behind it) expects the
		// classic `max_tokens` field, not OpenAI's newer `max_completion_tokens`.
		MaxTokens: openai.Int(int64(in.MaxTokens)),
	}

	resp, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return Result{}, fmt.Errorf("openrouter chat completion: %w", err)
	}

	result, err := toResult(resp)
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// toResult maps a chat completion response to Result. Pure helper, unit-tested.
func toResult(resp *openai.ChatCompletion) (Result, error) {
	if len(resp.Choices) == 0 {
		return Result{}, errors.New("openrouter returned no choices")
	}
	choice := resp.Choices[0]
	text := choice.Message.Content
	if text == "" {
		// finish_reason distinguishes "reasoning ate the whole token budget"
		// (length) from a genuinely empty answer — seen live with reasoning
		// models when max_tokens is sized for text-only output.
		return Result{}, fmt.Errorf("openrouter returned no text content (finish_reason=%q)", choice.FinishReason)
	}
	return Result{
		Text:         text,
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: int(resp.Usage.CompletionTokens),
		Truncated:    choice.FinishReason == "length",
	}, nil
}
