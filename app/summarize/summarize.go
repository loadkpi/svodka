// Package summarize is the use-case orchestrator: within an active Telegram
// session it resolves and fetches the source chats, summarizes them with the
// configured LLM, and posts the digest. It is network-bound and therefore not
// unit-tested (ADR-7); the pure helpers it relies on live in helpers.go.
package summarize

import (
	"context"
	"errors"
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

// Per-step deadlines. These are backstops, not tuning knobs: a hung Telegram or
// Anthropic call should surface a clear per-step error long before the workflow's
// overall timeout kills the job with no diagnostic. The parent context (signal /
// workflow timeout) still bounds the run as a whole.
const (
	chatTimeout = 2 * time.Minute // resolve + windowed history fetch for one chat
	llmTimeout  = 5 * time.Minute // whole digest build (single, or N maps + reduce)
	sendTimeout = 1 * time.Minute // posting the digest (possibly several parts)
)

// Deps are the orchestrator's collaborators, injected by main.
type Deps struct {
	Log      *logger.Logger
	Cfg      *config.Config
	Provider llm.Provider
}

// Run executes one digest cycle over every configured route (ADR-19): each route
// fans a set of source chats into one digest posted to one target chat. Routes
// run sequentially sharing a single resolver, window, and global settings. The
// run is tolerant of a failing route — every route is attempted and errors are
// aggregated — but returns non-nil (exit 1) if any route failed. Within a route
// it is tolerant of per-chat resolve/fetch failures (warn and skip, ADR-8) but a
// route fails if every source chat failed, if the LLM errors, or if sending
// fails. An empty digest (no messages in the window) is a success with nothing
// sent.
func Run(ctx context.Context, api *tg.Client, d Deps) error {
	start := time.Now()

	loc, err := time.LoadLocation(d.Cfg.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", d.Cfg.Timezone, err)
	}
	since := windowStart(start, d.Cfg.WindowHours)
	resolver := telegram.NewResolver(api)

	routes := d.Cfg.EffectiveRoutes()
	var errs []error
	for i, route := range routes {
		// Index, not target ref in the error path's chat warnings: warnings must
		// not advertise which chats are tracked (NFR-2). The target itself is
		// already logged in the clear by the send step, as before.
		if err := runRoute(ctx, api, resolver, d, i, route, since, loc); err != nil {
			errs = append(errs, fmt.Errorf("route[%d]: %w", i, err))
		}
	}
	d.Log.Info(ctx, "all routes done",
		"routes", len(routes), "routes_failed", len(errs),
		"total_ms", time.Since(start).Milliseconds())
	return errors.Join(errs...)
}

// runRoute executes one route: collect its source chats within the window, build
// a digest with the global LLM settings, and post it to the route's target.
func runRoute(ctx context.Context, api *tg.Client, resolver *telegram.Resolver, d Deps, idx int, route config.Route, since time.Time, loc *time.Location) error {
	start := time.Now()

	chats, st := collect(ctx, api, resolver, route.SourceChats, since, d.Log)
	d.Log.Info(ctx, "history collected",
		"route", idx,
		"chats_total", st.total, "chats_ok", st.ok, "chats_failed", st.failed,
		"messages", st.messages, "chars", st.chars,
		"fetch_ms", time.Since(start).Milliseconds())
	if st.ok == 0 {
		return fmt.Errorf("all %d source chats failed to resolve or fetch", st.total)
	}

	llmStart := time.Now()
	llmCtx, cancelLLM := context.WithTimeout(ctx, llmTimeout)
	defer cancelLLM()
	text, usage, err := digest.Build(llmCtx, d.Provider, chats, digest.Options{
		OutputLang:        d.Cfg.OutputLang,
		Location:          loc,
		Model:             d.Cfg.Model,
		MaxTokens:         d.Cfg.MaxOutputTokens,
		ExtraInstructions: d.Cfg.ExtraInstructions,
		Backlinks:         d.Cfg.Backlinks == nil || *d.Cfg.Backlinks,
	})
	if err != nil {
		return fmt.Errorf("build digest: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		d.Log.Info(ctx, "no digest produced; nothing to send", "route", idx, "messages", st.messages)
		return nil
	}
	if usage.Truncated {
		// Output hit max_output_tokens; the digest is likely cut off mid-thought.
		d.Log.Warn(ctx, "digest truncated at max_output_tokens; raise max_output_tokens or narrow the window",
			"route", idx, "max_output_tokens", d.Cfg.MaxOutputTokens, "tokens_out", usage.OutputTokens)
	}
	d.Log.Info(ctx, "digest built",
		"route", idx,
		"digest_chars", runeLen(text),
		"tokens_in", usage.InputTokens, "tokens_out", usage.OutputTokens,
		"llm_ms", time.Since(llmStart).Milliseconds())

	sendStart := time.Now()
	sendCtx, cancelSend := context.WithTimeout(ctx, sendTimeout)
	defer cancelSend()
	if err := telegram.Send(sendCtx, api, resolver, route.TargetChat, text); err != nil {
		return fmt.Errorf("send digest: %w", err)
	}
	d.Log.Info(ctx, "digest sent",
		"route", idx,
		"target", route.TargetChat,
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
		cm, err := fetchChat(ctx, api, r, ref, since)
		if err != nil {
			st.failed++
			// Index, not ref: the warning must not advertise which chats are tracked.
			log.Warn(ctx, "resolve/fetch failed; skipping chat", "chat_index", i, "err", err.Error())
			continue
		}
		st.ok++
		st.messages += len(cm.Messages)
		st.chars += chatChars(cm)
		out = append(out, cm)
	}
	return out, st
}

// fetchChat resolves one chat reference and reads its window under a per-chat
// deadline, so a single slow chat cannot stall the whole collect loop.
func fetchChat(ctx context.Context, api *tg.Client, r *telegram.Resolver, ref string, since time.Time) (telegram.ChatMessages, error) {
	ctx, cancel := context.WithTimeout(ctx, chatTimeout)
	defer cancel()

	p, err := r.Resolve(ctx, ref)
	if err != nil {
		return telegram.ChatMessages{}, fmt.Errorf("resolve: %w", err)
	}
	cm, err := telegram.FetchWindow(ctx, api, p, since)
	if err != nil {
		return telegram.ChatMessages{}, fmt.Errorf("fetch: %w", err)
	}
	return cm, nil
}
