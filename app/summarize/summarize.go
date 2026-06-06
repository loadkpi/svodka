// Package summarize is the use-case orchestrator: within an active Telegram
// session it resolves and fetches the source chats, summarizes them with the
// configured LLM, and posts the digest. It is network-bound and therefore not
// unit-tested (ADR-7); the pure helpers it relies on live in helpers.go.
package summarize

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"

	"svodka/business/digest"
	"svodka/business/llm"
	"svodka/business/telegram"
	"svodka/config"
	"svodka/foundation/logger"
)

// Deps are the orchestrator's collaborators, injected by main.
type Deps struct {
	Log      *logger.Logger
	Cfg      *config.Config
	Provider llm.Provider
}

// Run executes one digest cycle. It is tolerant of per-chat resolve/fetch
// failures (warn and skip, ADR-8) but fails the run if every source chat failed,
// if the LLM errors, or if sending fails. An empty digest (no messages in the
// window) is a success with nothing sent.
func Run(ctx context.Context, api *tg.Client, d Deps) error {
	start := time.Now()

	loc, err := time.LoadLocation(d.Cfg.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", d.Cfg.Timezone, err)
	}
	since := windowStart(start, d.Cfg.WindowHours)
	resolver := telegram.NewResolver(api)

	chats, st := collect(ctx, api, resolver, d.Cfg.SourceChats, since, d.Log)
	d.Log.Info(ctx, "history collected",
		"chats_total", st.total, "chats_ok", st.ok, "chats_failed", st.failed,
		"messages", st.messages, "chars", st.chars,
		"fetch_ms", time.Since(start).Milliseconds())
	if st.ok == 0 {
		return fmt.Errorf("all %d source chats failed to resolve or fetch", st.total)
	}

	llmStart := time.Now()
	text, usage, err := digest.Build(ctx, d.Provider, chats, digest.Options{
		OutputLang: d.Cfg.OutputLang,
		Location:   loc,
		Model:      d.Cfg.Model,
		MaxTokens:  d.Cfg.MaxOutputTokens,
	})
	if err != nil {
		return fmt.Errorf("build digest: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		d.Log.Info(ctx, "no digest produced; nothing to send", "messages", st.messages)
		return nil
	}
	if usage.Truncated {
		// Output hit max_output_tokens; the digest is likely cut off mid-thought.
		d.Log.Warn(ctx, "digest truncated at max_output_tokens; raise max_output_tokens or narrow the window",
			"max_output_tokens", d.Cfg.MaxOutputTokens, "tokens_out", usage.OutputTokens)
	}
	d.Log.Info(ctx, "digest built",
		"digest_chars", runeLen(text),
		"tokens_in", usage.InputTokens, "tokens_out", usage.OutputTokens,
		"llm_ms", time.Since(llmStart).Milliseconds())

	sendStart := time.Now()
	if err := telegram.Send(ctx, api, resolver, d.Cfg.TargetChat, text); err != nil {
		return fmt.Errorf("send digest: %w", err)
	}
	d.Log.Info(ctx, "digest sent",
		"target", d.Cfg.TargetChat,
		"send_ms", time.Since(sendStart).Milliseconds(),
		"total_ms", time.Since(start).Milliseconds())
	return nil
}

// collect resolves and fetches each source chat within the window. Failures are
// logged by index (not by reference, to avoid advertising which chats are
// tracked) and skipped so one broken chat does not sink the whole run.
func collect(ctx context.Context, api *tg.Client, r *telegram.Resolver, refs []string, since time.Time, log *logger.Logger) ([]telegram.ChatMessages, stats) {
	st := stats{total: len(refs)}
	var out []telegram.ChatMessages

	for i, ref := range refs {
		p, err := r.Resolve(ctx, ref)
		if err != nil {
			st.failed++
			log.Warn(ctx, "resolve failed; skipping chat", "chat_index", i, "err", err.Error())
			continue
		}
		cm, err := telegram.FetchWindow(ctx, api, p, since)
		if err != nil {
			st.failed++
			log.Warn(ctx, "fetch failed; skipping chat", "chat_index", i, "err", err.Error())
			continue
		}
		st.ok++
		st.messages += len(cm.Messages)
		st.chars += chatChars(cm)
		out = append(out, cm)
	}
	return out, st
}
