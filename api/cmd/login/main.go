// login is a one-off interactive command that signs the user into Telegram and
// produces a session for the svodka job. It is meant to be run in a Codespace
// (browser terminal) where the phone, login code and optional 2FA password can
// be entered. On success the session is exported and stored as a GitHub secret
// (added in M2.3); M2.2 establishes the auth flow and confirms authorization.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"svodka/business/telegram"
	"svodka/config"
	"svodka/foundation/logger"
)

// sessionSecret is the GitHub Actions secret that holds the base64 session.
const sessionSecret = "SVODKA_TELEGRAM_SESSION"

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

		b64, err := telegram.Export(ctx, client.Storage())
		if err != nil {
			return fmt.Errorf("export session: %w", err)
		}
		return storeSession(ctx, log, b64)
	})
}

// secretSetter stores value under the named GitHub secret. ghSecretSet is the
// production implementation; tests inject fakes. A nil setter means gh is not
// available, which forces the manual fallback.
type secretSetter func(name, value string) error

// storeSession picks the gh-backed setter (or none, if gh is missing) and hands
// off to persistSession. The numeric logging stays here so persistSession can
// be a pure, fully testable function.
func storeSession(ctx context.Context, log *logger.Logger, b64 string) error {
	var setter secretSetter
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, "gh CLI not found; falling back to manual setup.")
	} else {
		setter = func(name, value string) error { return ghSecretSet(ctx, name, value) }
	}

	if persistSession(os.Stdout, os.Stderr, setter, b64) {
		log.Info(ctx, "session stored as GitHub secret", "secret", sessionSecret)
	}
	return nil
}

// persistSession stores the session via setter and reports whether it landed in
// the secret. Security invariant: when setter succeeds the base64 string is
// written nowhere; only on the manual fallback is it printed to stdout (with a
// warning on stderr). This function is the unit-tested core.
func persistSession(stdout, stderr io.Writer, setter secretSetter, b64 string) (stored bool) {
	if setter == nil {
		printManual(stdout, stderr, b64)
		return false
	}
	if err := setter(sessionSecret, b64); err != nil {
		fmt.Fprintf(stderr, "gh secret set failed: %v\n", err)
		printManual(stdout, stderr, b64)
		return false
	}
	fmt.Fprintf(stderr, "Done. Session stored as the %s secret.\n", sessionSecret)
	return true
}

// ghSecretSet shells out to `gh secret set NAME`, feeding value on stdin. Any gh
// stderr is folded into the returned error so callers can surface it.
func ghSecretSet(ctx context.Context, name, value string) error {
	cmd := exec.CommandContext(ctx, "gh", "secret", "set", name)
	cmd.Stdin = strings.NewReader(value)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// printManual prints the base64 session with a sensitivity warning. The warning
// goes to stderr; the value alone goes to stdout so it is easy to copy.
func printManual(stdout, stderr io.Writer, b64 string) {
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "!!! The base64 string below is FULL ACCESS to your Telegram account.")
	fmt.Fprintln(stderr, "!!! Do NOT share it with anyone.")
	fmt.Fprintf(stderr, "!!! Add it as the %s secret (Settings -> Secrets and variables ->\n", sessionSecret)
	fmt.Fprintln(stderr, "!!! Actions), then clear your terminal.")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stdout, b64)
}
