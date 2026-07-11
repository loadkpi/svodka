// svodka is the daily digest job: load config, connect to Telegram, summarize
// recent messages with Claude, and post the digest to the target chat.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gotd/td/tg"

	"svodka/app/summarize"
	"svodka/business/llm"
	"svodka/business/telegram"
	"svodka/config"
	"svodka/foundation/logger"
)

func main() {
	log := logger.New("svodka")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, log); err != nil {
		if errors.Is(err, config.ErrHelp) {
			return
		}
		log.Error(ctx, "run failed", "err", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *logger.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info(ctx, "config loaded",
		"routes", len(cfg.EffectiveRoutes()),
		"window_hours", cfg.WindowHours,
		"model", cfg.Model,
	)

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
		// Log only the numeric Telegram id; usernames/names are PII-adjacent
		// and Actions logs may be visible to others on a forked template.
		log.Info(ctx, "telegram authorized", "user_id", status.User.GetID())

		provider, err := newProvider(cfg)
		if err != nil {
			return err
		}
		return summarize.Run(ctx, api, summarize.Deps{
			Log:      log,
			Cfg:      cfg,
			Provider: provider,
		})
	})
}

// newProvider picks the llm.Provider implementation for the configured
// vendor (ADR-20). The default case is unreachable after config.validate()
// rejects unknown llm_provider values, but errors instead of panicking.
func newProvider(cfg *config.Config) (llm.Provider, error) {
	switch cfg.LLMProvider {
	case "anthropic":
		return llm.NewClaude(cfg.Anthropic.Key), nil
	case "openrouter":
		return llm.NewOpenRouter(cfg.OpenRouter.Key), nil
	default:
		return nil, fmt.Errorf("unknown llm_provider %q", cfg.LLMProvider)
	}
}
