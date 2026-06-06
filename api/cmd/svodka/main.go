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
		"source_chats", len(cfg.SourceChats),
		"target_chat", cfg.TargetChat,
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
			return errors.New("Telegram session is not authorized; run `go run ./api/cmd/login` in a Codespace to refresh SVODKA_TELEGRAM_SESSION")
		}
		// Log only the numeric Telegram id; usernames/names are PII-adjacent
		// and Actions logs may be visible to others on a forked template.
		log.Info(ctx, "telegram authorized", "user_id", status.User.GetID())

		return summarize.Run(ctx, api, summarize.Deps{
			Log:      log,
			Cfg:      cfg,
			Provider: llm.NewClaude(cfg.Anthropic.Key),
		})
	})
}
