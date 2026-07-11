package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/tg"

	"svodka/business/telegram"
	"svodka/config"
	"svodka/foundation/logger"
)

// chatTimeout bounds resolve+fetch for a single source chat. Mirrors
// app/summarize/summarize.go's chatTimeout, redefined locally since that
// constant is unexported in that package.
const chatTimeout = 2 * time.Minute

// runCapture connects to Telegram using the same session/config as the
// svodka job, fetches every configured source chat within the window, and
// freezes the result into a snapshot file under eval/ so a later `eval run`
// needs no Telegram secrets and every candidate model sees byte-identical
// input (ADR-21).
func runCapture(ctx context.Context, log *logger.Logger) error {
	cfg, err := config.LoadForCapture()
	if err != nil {
		return err
	}

	client, err := telegram.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("telegram client: %w", err)
	}

	return client.Run(ctx, func(ctx context.Context, api *tg.Client) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}
		if !status.Authorized {
			return errors.New("no authorized Telegram session; run `go run ./api/cmd/login` in a Codespace to refresh SVODKA_TELEGRAM_SESSION")
		}

		resolver := telegram.NewResolver(api)
		since := time.Now().Add(-time.Duration(cfg.WindowHours) * time.Hour)
		refs := sourceChatUnion(cfg.EffectiveRoutes())

		chats, ok, _ := fetchAll(ctx, api, resolver, refs, since, log)
		if ok == 0 {
			return fmt.Errorf("all %d source chats failed to resolve or fetch", len(refs))
		}

		snap := Snapshot{
			Schema:     currentSchema,
			CapturedAt: time.Now().UTC(),
			Settings: SnapshotSettings{
				WindowHours:       cfg.WindowHours,
				OutputLang:        cfg.OutputLang,
				Timezone:          cfg.Timezone,
				Backlinks:         cfg.Backlinks == nil || *cfg.Backlinks,
				ExtraInstructions: cfg.ExtraInstructions,
				MaxOutputTokens:   cfg.MaxOutputTokens,
			},
			Chats: chats,
		}

		data, err := encodeSnapshot(snap)
		if err != nil {
			return fmt.Errorf("encode snapshot: %w", err)
		}
		if err := os.MkdirAll("eval", 0o755); err != nil {
			return fmt.Errorf("create eval dir: %w", err)
		}
		path := filepath.Join("eval", "capture-"+time.Now().Format("20060102-150405")+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write snapshot: %w", err)
		}

		messages, chars := chatStats(chats)
		log.Info(ctx, "snapshot captured", "path", path, "chats", len(chats), "messages", messages, "chars", chars)
		return nil
	})
}

// sourceChatUnion flattens every route's source chats into one deduplicated,
// first-seen-order list: capture is route-agnostic, it only needs "which
// messages would be summarized" across the whole run, not per-route targets.
func sourceChatUnion(routes []config.Route) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range routes {
		for _, ref := range r.SourceChats {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// fetchAll resolves and fetches each ref within the window, tolerant of
// per-chat failure (logged by index only, never by ref — NFR-2).
func fetchAll(ctx context.Context, api *tg.Client, r *telegram.Resolver, refs []string, since time.Time, log *logger.Logger) (chats []telegram.ChatMessages, ok, failed int) {
	for i, ref := range refs {
		cm, err := fetchChat(ctx, api, r, ref, since)
		if err != nil {
			failed++
			log.Warn(ctx, "resolve/fetch failed; skipping chat", "chat_index", i, "err", err.Error())
			continue
		}
		ok++
		chats = append(chats, cm)
	}
	return chats, ok, failed
}

// fetchChat resolves one chat reference and reads its window under a
// per-chat deadline, mirroring app/summarize's fetchChat so one slow chat
// cannot stall the whole capture.
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

// chatStats sums messages and character counts across chats, for logging
// counts only — never content (NFR-2).
func chatStats(chats []telegram.ChatMessages) (messages, chars int) {
	for _, c := range chats {
		messages += len(c.Messages)
		for _, m := range c.Messages {
			chars += len([]rune(m.Text))
		}
	}
	return messages, chars
}
