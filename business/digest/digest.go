// Package digest turns collected Telegram chat history into a single daily
// summary via an llm.Provider. Small days go through one call; large days use a
// per-chat map-reduce so we do not push the whole context into a single request.
//
// The summarization provider is injected (llm.Provider) so the orchestrator can
// pass a real Claude client and tests can pass a fake. All pure helpers
// (serialization, strategy selection) are unit-tested; the network call lives
// behind the interface (see ADR-7, ADR-10).
package digest

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"svodka/business/llm"
	"svodka/business/telegram"
)

// defaultThresholdChars is the payload size (in runes of the serialized user
// text) at or above which Build switches from a single call to map-reduce.
// Roughly four characters per token, so ~16k chars is on the order of 4k tokens
// of input — small enough for one quality pass below it.
const defaultThresholdChars = 16000

// Options carries the non-secret settings Build needs. Location is resolved by
// the caller (config Timezone -> *time.Location) so Build stays free of timezone
// parsing and its error handling. ThresholdChars <= 0 falls back to the default.
type Options struct {
	OutputLang     string
	Location       *time.Location
	Model          string
	MaxTokens      int
	ThresholdChars int
	// ExtraInstructions is the deployer's free-text customization, appended to
	// the final-output prompts (single/reduce). Empty = default behavior.
	ExtraInstructions string
	// Backlinks appends a bare t.me/c source link to each channel/supergroup
	// message line so the digest can point back to the original (M13, ADR-15).
	Backlinks bool
}

// Usage aggregates token accounting across every LLM call Build made (one for a
// small day, N maps + 1 reduce for a large one). Truncated is set if any call
// stopped at MaxTokens, meaning the digest is likely cut off. LinksRemoved
// counts backlinks stripped by the sanitizeLinks guard (M27) — a non-zero
// value means the model emitted a link that did not match an actual
// (channel, message) pair from the input.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Truncated    bool
	LinksRemoved int
}

func (u *Usage) add(r llm.Result) {
	u.InputTokens += r.InputTokens
	u.OutputTokens += r.OutputTokens
	if r.Truncated {
		u.Truncated = true
	}
}

// Build produces the final digest text plus token usage. It returns "" (no
// error) when there is nothing to summarize, so the caller can skip sending an
// empty message.
func Build(ctx context.Context, p llm.Provider, chats []telegram.ChatMessages, opts Options) (string, Usage, error) {
	chats = nonEmpty(chats)
	if len(chats) == 0 {
		return "", Usage{}, nil
	}

	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}
	threshold := opts.ThresholdChars
	if threshold <= 0 {
		threshold = defaultThresholdChars
	}

	full := serializeAll(chats, loc, opts.Backlinks)
	valid := validLinks(chats)
	if !useMapReduce(full, threshold) {
		return single(ctx, p, full, opts, valid)
	}
	return mapReduce(ctx, p, chats, opts, loc, valid)
}

// validLinks collects every backlink URL that linkFor would actually build for
// the input (public or private form, matching each chat's username), so
// sanitizeLinks can tell a genuine backlink from a misquoted one (M27, M28).
// Chats with ChannelID == 0 (users/basic groups) have no addressable backlink
// and are skipped, matching linkFor.
func validLinks(chats []telegram.ChatMessages) map[string]bool {
	valid := make(map[string]bool)
	for _, c := range chats {
		for _, m := range c.Messages {
			// A t.me post link quoted inside a message is legitimate content
			// the model may keep — allow it verbatim so the guard only ever
			// strips links that appeared in neither form of the input.
			for _, quoted := range backlinkRe.FindAllString(m.Text, -1) {
				valid[quoted] = true
			}
			if c.ChannelID == 0 {
				continue
			}
			if l := linkFor(c.ChannelID, c.Username, m.ID); l != "" {
				valid[l] = true
			}
		}
	}
	return valid
}

// single summarizes the whole day in one call.
func single(ctx context.Context, p llm.Provider, userText string, opts Options, valid map[string]bool) (string, Usage, error) {
	r, err := p.Summarize(ctx, llm.Input{
		System:    singleSystem(opts.OutputLang, opts.ExtraInstructions),
		User:      userText,
		Model:     opts.Model,
		MaxTokens: opts.MaxTokens,
	})
	if err != nil {
		return "", Usage{}, fmt.Errorf("digest single-pass: %w", err)
	}
	var u Usage
	u.add(r)
	text, removed := sanitizeLinks(r.Text, valid)
	u.LinksRemoved = removed
	return strings.TrimSpace(text), u, nil
}

// mapReduce summarizes each chat on its own (map) and then stitches the per-chat
// summaries into the final digest (reduce). The map system prompt is built once
// and reused byte-for-byte across every map call so prompt caching can kick in.
func mapReduce(ctx context.Context, p llm.Provider, chats []telegram.ChatMessages, opts Options, loc *time.Location, valid map[string]bool) (string, Usage, error) {
	sys := mapSystem(opts.OutputLang)

	var u Usage
	var b strings.Builder
	for i, c := range chats {
		r, err := p.Summarize(ctx, llm.Input{
			System:    sys,
			User:      serializeChat(c, loc, opts.Backlinks),
			Model:     opts.Model,
			MaxTokens: opts.MaxTokens,
		})
		if err != nil {
			// Index, not title: error text must not carry chat content (NFR-2).
			return "", Usage{}, fmt.Errorf("digest map chat %d: %w", i, err)
		}
		u.add(r)
		out := strings.TrimSpace(r.Text)
		if out == "" {
			continue
		}
		b.WriteString(titleMarker)
		b.WriteString(c.Title)
		b.WriteByte('\n')
		b.WriteString(out)
		b.WriteString("\n\n")
	}

	reduced := strings.TrimSpace(b.String())
	if reduced == "" {
		return "", u, nil
	}

	r, err := p.Summarize(ctx, llm.Input{
		System:    reduceSystem(opts.OutputLang, opts.ExtraInstructions),
		User:      reduced,
		Model:     opts.Model,
		MaxTokens: opts.MaxTokens,
	})
	if err != nil {
		return "", Usage{}, fmt.Errorf("digest reduce: %w", err)
	}
	u.add(r)
	text, removed := sanitizeLinks(r.Text, valid)
	u.LinksRemoved = removed
	return strings.TrimSpace(text), u, nil
}

// nonEmpty drops chats that have no messages, without mutating the input.
func nonEmpty(chats []telegram.ChatMessages) []telegram.ChatMessages {
	var out []telegram.ChatMessages
	for _, c := range chats {
		if len(c.Messages) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// useMapReduce reports whether the serialized payload is large enough to warrant
// per-chat map-reduce instead of a single call. Size is counted in runes so the
// threshold is fair across languages (Cyrillic is multi-byte in UTF-8).
func useMapReduce(userText string, thresholdChars int) bool {
	return utf8.RuneCountInString(userText) >= thresholdChars
}
