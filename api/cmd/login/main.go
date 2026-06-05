// login is a one-off interactive command that signs the user into Telegram and
// produces a session for the svodka job. It is meant to be run in a Codespace
// (browser terminal) where the phone, login code and optional 2FA password can
// be entered. On success the session is exported and stored as a GitHub secret
// (added in M2.3); M2.2 establishes the auth flow and confirms authorization.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"svodka/business/telegram"
	"svodka/config"
	"svodka/foundation/logger"
)

func main() {
	log := logger.New("login")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, log); err != nil {
		if errors.Is(err, config.ErrHelp) {
			return
		}
		log.Error(ctx, "login failed", "err", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *logger.Logger) error {
	// Only the env secrets are needed here (api_id/api_hash). config.Load would
	// fail: there is no session yet, which is exactly what we are about to mint.
	secrets, err := config.LoadSecrets()
	if err != nil {
		return err
	}

	// Empty Telegram.Session => telegram.New starts with empty storage, so the
	// auth flow below performs a fresh sign-in.
	cfg := &config.Config{Secrets: secrets}
	client, err := telegram.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("telegram client: %w", err)
	}

	return client.Run(ctx, func(ctx context.Context, _ *tg.Client) error {
		flow := auth.NewFlow(telegram.TermAuth(os.Stdin, os.Stderr), auth.SendCodeOptions{})
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("auth flow: %w", err)
		}

		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}
		if !status.Authorized {
			return errors.New("auth flow finished but session is not authorized")
		}
		// Log only the numeric id; usernames/names are PII-adjacent.
		log.Info(ctx, "telegram authorized", "user_id", status.User.GetID())
		return nil
	})
}
